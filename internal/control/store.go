package control

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
)

var (
	ErrNotFound   = errors.New("resource not found")
	ErrConflict   = errors.New("state conflict")
	ErrNoWork     = errors.New("no schedulable work")
	ErrStaleLease = errors.New("lease is stale or no longer current")
)

// Config controls scheduler, lease, and preview reconciliation behavior.
type Config struct {
	LeaseTTL           time.Duration
	WorkerTTL          time.Duration
	DefaultTenantLimit int
	PreviewBaseDomain  string
	PreviewDeleteDelay time.Duration
}

// Metrics is a monotonic snapshot suitable for the Prometheus endpoint.
type Metrics struct {
	WebhookAccepted  uint64
	WebhookDuplicate uint64
	LeasesIssued     uint64
	LeasesExpired    uint64
	StaleCompletions uint64
	JobsCompleted    uint64
	JobsFailed       uint64
	JobsCancelled    uint64
	PreviewsCreated  uint64
	PreviewsDeleted  uint64
}

// Store is a concurrency-safe reference control plane.
//
// The in-memory adapter keeps the correctness rules executable in local demos.
// The SQL schema in migrations documents the production PostgreSQL boundary.
type Store struct {
	mu sync.RWMutex

	config       Config
	pipelines    map[string]*domain.Pipeline
	jobs         map[string]*domain.Job
	attempts     map[string]*domain.Attempt
	workers      map[string]*domain.Worker
	previews     map[string]*domain.Preview
	deliveries   map[string]string
	tenantLimits map[string]int
	lastTenant   string
	metrics      Metrics
}

// New creates an empty control-plane store with safe defaults.
func New(config Config) *Store {
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = 30 * time.Second
	}
	if config.WorkerTTL <= 0 {
		config.WorkerTTL = 45 * time.Second
	}
	if config.DefaultTenantLimit <= 0 {
		config.DefaultTenantLimit = 2
	}
	if config.PreviewBaseDomain == "" {
		config.PreviewBaseDomain = "preview.local"
	}
	if config.PreviewDeleteDelay <= 0 {
		config.PreviewDeleteDelay = 5 * time.Second
	}
	return &Store{
		config:       config,
		pipelines:    make(map[string]*domain.Pipeline),
		jobs:         make(map[string]*domain.Job),
		attempts:     make(map[string]*domain.Attempt),
		workers:      make(map[string]*domain.Worker),
		previews:     make(map[string]*domain.Preview),
		deliveries:   make(map[string]string),
		tenantLimits: make(map[string]int),
	}
}

// CreatePipeline validates and persists a new immutable pipeline plan.
func (s *Store) CreatePipeline(tenant, repo, commitSHA, trigger string, prNumber int, spec domain.PipelineSpec, now time.Time) (domain.PipelineView, error) {
	order, err := planner.Validate(spec)
	if err != nil {
		return domain.PipelineView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pipeline := s.createPipelineLocked(tenant, repo, commitSHA, trigger, prNumber, spec, order, now)
	return s.pipelineViewLocked(pipeline.ID), nil
}

// CreatePipelineForDelivery atomically deduplicates a webhook delivery and creates its pipeline.
func (s *Store) CreatePipelineForDelivery(deliveryID, tenant, repo, commitSHA, trigger string, prNumber int, spec domain.PipelineSpec, now time.Time) (domain.PipelineView, bool, error) {
	if deliveryID == "" {
		return domain.PipelineView{}, false, errors.New("delivery ID is required")
	}
	order, err := planner.Validate(spec)
	if err != nil {
		return domain.PipelineView{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pipelineID, ok := s.deliveries[deliveryID]; ok {
		s.metrics.WebhookDuplicate++
		if pipelineID == "" {
			return domain.PipelineView{}, true, nil
		}
		return s.pipelineViewLocked(pipelineID), true, nil
	}
	pipeline := s.createPipelineLocked(tenant, repo, commitSHA, trigger, prNumber, spec, order, now)
	s.deliveries[deliveryID] = pipeline.ID
	s.metrics.WebhookAccepted++
	return s.pipelineViewLocked(pipeline.ID), false, nil
}

// MarkPreviewDeletingForDelivery records a close event exactly once and creates a tombstone when needed.
func (s *Store) MarkPreviewDeletingForDelivery(deliveryID, repo string, prNumber int, now time.Time) (bool, error) {
	if deliveryID == "" {
		return false, errors.New("delivery ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.deliveries[deliveryID]; ok {
		s.metrics.WebhookDuplicate++
		return true, nil
	}
	s.deliveries[deliveryID] = ""
	s.metrics.WebhookAccepted++
	key := previewKey(repo, prNumber)
	preview, ok := s.previews[key]
	if !ok {
		preview = &domain.Preview{
			Repo: repo, PRNumber: prNumber, Namespace: previewNamespace(repo, prNumber),
			Generation: 1, Desired: domain.StateDeleting, Actual: domain.StateDeleting,
			UpdatedAt: now,
		}
		s.previews[key] = preview
	} else if preview.Desired != domain.StateDeleting {
		preview.Desired = domain.StateDeleting
		preview.Generation++
		preview.UpdatedAt = now
	}
	for _, pipeline := range s.pipelines {
		if pipeline.Repo == repo && pipeline.PRNumber == prNumber && !terminal(pipeline.Status) {
			s.cancelPipelineLocked(pipeline, now)
		}
	}
	return false, nil
}

func (s *Store) createPipelineLocked(tenant, repo, commitSHA, trigger string, prNumber int, spec domain.PipelineSpec, order []string, now time.Time) *domain.Pipeline {
	pipeline := &domain.Pipeline{
		ID: newID("pl"), Tenant: tenant, Repo: repo, CommitSHA: commitSHA,
		Trigger: trigger, PRNumber: prNumber, Status: domain.StateQueued, CreatedAt: now,
	}
	for _, name := range order {
		job := &domain.Job{
			ID: newID("job"), PipelineID: pipeline.ID, Tenant: tenant, Name: name,
			Spec: cloneJobSpec(spec.Jobs[name]), Status: domain.StateQueued, CreatedAt: now,
		}
		pipeline.JobIDs = append(pipeline.JobIDs, job.ID)
		s.jobs[job.ID] = job
	}
	s.pipelines[pipeline.ID] = pipeline
	return pipeline
}

// GetPipeline returns a consistent snapshot of the pipeline and all attempts.
func (s *Store) GetPipeline(id string) (domain.PipelineView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.pipelines[id]; !ok {
		return domain.PipelineView{}, ErrNotFound
	}
	return s.pipelineViewLocked(id), nil
}

// RegisterWorker creates or refreshes a worker using its workload identity.
func (s *Store) RegisterWorker(worker domain.Worker, now time.Time) (domain.Worker, error) {
	if worker.ID == "" || worker.Pool == "" {
		return domain.Worker{}, errors.New("worker ID and pool are required")
	}
	if worker.Capacity <= 0 {
		return domain.Worker{}, errors.New("worker capacity must be positive")
	}
	worker.Heartbeat = now
	if worker.Capabilities == nil {
		worker.Capabilities = map[string]bool{}
	}
	s.mu.Lock()
	s.workers[worker.ID] = cloneWorker(&worker)
	s.mu.Unlock()
	return worker, nil
}

// HeartbeatWorker refreshes worker eligibility without changing its advertised capacity.
func (s *Store) HeartbeatWorker(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	worker, ok := s.workers[id]
	if !ok {
		return ErrNotFound
	}
	worker.Heartbeat = now
	return nil
}

// SetTenantLimit overrides the default concurrent-attempt quota for one tenant.
func (s *Store) SetTenantLimit(tenant string, limit int) error {
	if tenant == "" || limit <= 0 {
		return errors.New("tenant and a positive limit are required")
	}
	s.mu.Lock()
	s.tenantLimits[tenant] = limit
	s.mu.Unlock()
	return nil
}

// ScheduleOne selects one runnable job using tenant round-robin and FIFO within priority.
func (s *Store) ScheduleOne(now time.Time) (domain.AttemptLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcileExpiredLocked(now)

	byTenant := s.runnableJobsLocked()
	if len(byTenant) == 0 {
		return domain.AttemptLease{}, ErrNoWork
	}
	tenants := make([]string, 0, len(byTenant))
	for tenant := range byTenant {
		tenants = append(tenants, tenant)
	}
	sort.Strings(tenants)
	start := 0
	if s.lastTenant != "" {
		start = sort.SearchStrings(tenants, s.lastTenant)
		if start < len(tenants) && tenants[start] == s.lastTenant {
			start++
		}
		if start >= len(tenants) {
			start = 0
		}
	}
	for offset := 0; offset < len(tenants); offset++ {
		index := (start + offset) % len(tenants)
		tenant := tenants[index]
		if s.activeForTenantLocked(tenant) >= s.tenantLimitLocked(tenant) {
			for _, job := range byTenant[tenant] {
				job.QueueReason = "tenant concurrent-job quota reached"
			}
			continue
		}
		for _, job := range byTenant[tenant] {
			worker := s.selectWorkerLocked(job, now)
			if worker == nil {
				job.QueueReason = "no eligible worker has matching capabilities and capacity"
				continue
			}
			token := newID("lease")
			attempt := &domain.Attempt{
				ID: newID("attempt"), JobID: job.ID, Number: len(job.AttemptIDs) + 1,
				WorkerID: worker.ID, LeaseToken: token, LeaseExpires: now.Add(s.config.LeaseTTL),
				Status: domain.StateLeased, CreatedAt: now,
			}
			job.AttemptIDs = append(job.AttemptIDs, attempt.ID)
			job.Status = domain.StateLeased
			job.QueueReason = ""
			s.attempts[attempt.ID] = attempt
			pipeline := s.pipelines[job.PipelineID]
			pipeline.Status = domain.StateRunning
			s.lastTenant = tenant
			s.metrics.LeasesIssued++
			return domain.AttemptLease{
				Attempt: *attempt, LeaseToken: token, Job: cloneJob(job), Pipeline: clonePipeline(pipeline), Deadline: attempt.LeaseExpires,
			}, nil
		}
	}
	return domain.AttemptLease{}, ErrNoWork
}

// AssignedLeases returns current worker assignments, including lease tokens.
func (s *Store) AssignedLeases(workerID string) ([]domain.AttemptLease, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.workers[workerID]; !ok {
		return nil, ErrNotFound
	}
	leases := make([]domain.AttemptLease, 0)
	for _, attempt := range s.attempts {
		if attempt.WorkerID != workerID || !active(attempt.Status) {
			continue
		}
		job := s.jobs[attempt.JobID]
		leases = append(leases, domain.AttemptLease{
			Attempt: *attempt, LeaseToken: attempt.LeaseToken, Job: cloneJob(job),
			Pipeline: clonePipeline(s.pipelines[job.PipelineID]), Deadline: attempt.LeaseExpires,
		})
	}
	sort.Slice(leases, func(i, j int) bool { return leases[i].Attempt.CreatedAt.Before(leases[j].Attempt.CreatedAt) })
	return leases, nil
}

// HeartbeatAttempt starts or renews the current lease using compare-and-set semantics.
func (s *Store) HeartbeatAttempt(id, token string, now time.Time) (domain.Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[id]
	if !ok {
		return domain.Attempt{}, ErrNotFound
	}
	if !active(attempt.Status) || attempt.LeaseToken != token || !now.Before(attempt.LeaseExpires) {
		return domain.Attempt{}, ErrStaleLease
	}
	if attempt.Status == domain.StateLeased {
		attempt.Status = domain.StateRunning
		attempt.StartedAt = now
		s.jobs[attempt.JobID].Status = domain.StateRunning
	}
	attempt.LeaseExpires = now.Add(s.config.LeaseTTL)
	return *attempt, nil
}

// CompleteAttempt commits a result only for the current attempt and lease token.
func (s *Store) CompleteAttempt(id, token string, success bool, message string, now time.Time) (domain.Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[id]
	if !ok {
		return domain.Attempt{}, ErrNotFound
	}
	if !active(attempt.Status) || attempt.LeaseToken != token || !now.Before(attempt.LeaseExpires) {
		s.metrics.StaleCompletions++
		return domain.Attempt{}, ErrStaleLease
	}
	job := s.jobs[attempt.JobID]
	if job.Status == domain.StateCancelled {
		s.metrics.StaleCompletions++
		return domain.Attempt{}, ErrStaleLease
	}
	attempt.CompletedAt = now
	attempt.ResultMessage = message
	if success {
		attempt.Status = domain.StateSucceeded
		job.Status = domain.StateSucceeded
		s.metrics.JobsCompleted++
		if job.Spec.Environment != nil {
			s.activatePreviewLocked(s.pipelines[job.PipelineID], job.Spec.Environment, now)
		}
	} else {
		attempt.Status = domain.StateFailed
		job.Status = domain.StateFailed
		s.metrics.JobsFailed++
	}
	s.updatePipelineLocked(s.pipelines[job.PipelineID], now)
	return *attempt, nil
}

// CancelJob persists cancellation and invalidates any current attempt.
func (s *Store) CancelJob(id string, now time.Time) (domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return domain.Job{}, ErrNotFound
	}
	if terminal(job.Status) {
		return cloneJob(job), nil
	}
	s.cancelPipelineLocked(s.pipelines[job.PipelineID], now)
	return cloneJob(job), nil
}

// Reconcile repairs expired leases and removes previews after their deletion grace period.
func (s *Store) Reconcile(now time.Time) (expired, deleted int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expired = s.reconcileExpiredLocked(now)
	for key, preview := range s.previews {
		if preview.Desired == domain.StateActive && !preview.ExpiresAt.IsZero() && !now.Before(preview.ExpiresAt) {
			preview.Desired = domain.StateDeleting
			preview.Generation++
			preview.UpdatedAt = now
		}
		if preview.Desired == domain.StateDeleting {
			preview.Actual = domain.StateDeleting
			if now.Sub(preview.UpdatedAt) >= s.config.PreviewDeleteDelay {
				delete(s.previews, key)
				deleted++
				s.metrics.PreviewsDeleted++
			}
		}
	}
	return expired, deleted
}

// GetPreview returns desired and observed state for a repository pull request.
func (s *Store) GetPreview(repo string, prNumber int) (domain.Preview, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	preview, ok := s.previews[previewKey(repo, prNumber)]
	if !ok {
		return domain.Preview{}, ErrNotFound
	}
	return *preview, nil
}

// Metrics returns a consistent counter snapshot.
func (s *Store) Metrics() Metrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}

func (s *Store) runnableJobsLocked() map[string][]*domain.Job {
	result := make(map[string][]*domain.Job)
	for _, job := range s.jobs {
		if job.Status != domain.StateQueued || !s.dependenciesSucceededLocked(job) {
			continue
		}
		result[job.Tenant] = append(result[job.Tenant], job)
	}
	for tenant := range result {
		sort.Slice(result[tenant], func(i, j int) bool {
			left, right := result[tenant][i], result[tenant][j]
			if left.Spec.Priority != right.Spec.Priority {
				return left.Spec.Priority > right.Spec.Priority
			}
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.Before(right.CreatedAt)
			}
			return left.Name < right.Name
		})
	}
	return result
}

func (s *Store) dependenciesSucceededLocked(job *domain.Job) bool {
	if len(job.Spec.Needs) == 0 {
		return true
	}
	pipeline := s.pipelines[job.PipelineID]
	states := make(map[string]domain.State, len(pipeline.JobIDs))
	for _, id := range pipeline.JobIDs {
		candidate := s.jobs[id]
		states[candidate.Name] = candidate.Status
	}
	for _, dependency := range job.Spec.Needs {
		if states[dependency] != domain.StateSucceeded {
			return false
		}
	}
	return true
}

func (s *Store) selectWorkerLocked(job *domain.Job, now time.Time) *domain.Worker {
	ids := make([]string, 0, len(s.workers))
	for id := range s.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		worker := s.workers[id]
		if now.Sub(worker.Heartbeat) > s.config.WorkerTTL || s.activeForWorkerLocked(worker.ID) >= worker.Capacity {
			continue
		}
		if job.Spec.Trusted && !worker.Trusted {
			continue
		}
		matched := true
		for _, capability := range job.Spec.Capabilities {
			if !worker.Capabilities[capability] {
				matched = false
				break
			}
		}
		if matched {
			return worker
		}
	}
	return nil
}

func (s *Store) activeForWorkerLocked(workerID string) int {
	count := 0
	for _, attempt := range s.attempts {
		if attempt.WorkerID == workerID && active(attempt.Status) {
			count++
		}
	}
	return count
}

func (s *Store) activeForTenantLocked(tenant string) int {
	count := 0
	for _, job := range s.jobs {
		if job.Tenant == tenant && active(job.Status) {
			count++
		}
	}
	return count
}

func (s *Store) tenantLimitLocked(tenant string) int {
	if limit := s.tenantLimits[tenant]; limit > 0 {
		return limit
	}
	return s.config.DefaultTenantLimit
}

func (s *Store) reconcileExpiredLocked(now time.Time) int {
	count := 0
	for _, attempt := range s.attempts {
		if !active(attempt.Status) || now.Before(attempt.LeaseExpires) {
			continue
		}
		attempt.Status = domain.StateLost
		attempt.CompletedAt = now
		job := s.jobs[attempt.JobID]
		if job.Status != domain.StateCancelled {
			job.Status = domain.StateQueued
			job.QueueReason = "previous attempt lease expired; waiting for retry"
		}
		count++
		s.metrics.LeasesExpired++
	}
	return count
}

func (s *Store) updatePipelineLocked(pipeline *domain.Pipeline, now time.Time) {
	allSucceeded := true
	for _, jobID := range pipeline.JobIDs {
		job := s.jobs[jobID]
		if job.Status == domain.StateFailed {
			pipeline.Status = domain.StateFailed
			for _, pendingID := range pipeline.JobIDs {
				pending := s.jobs[pendingID]
				if !terminal(pending.Status) {
					pending.Status = domain.StateCancelled
					s.metrics.JobsCancelled++
				}
			}
			return
		}
		if job.Status != domain.StateSucceeded {
			allSucceeded = false
		}
	}
	if allSucceeded {
		pipeline.Status = domain.StateSucceeded
	} else {
		pipeline.Status = domain.StateRunning
	}
}

func (s *Store) cancelPipelineLocked(pipeline *domain.Pipeline, now time.Time) {
	pipeline.Status = domain.StateCancelled
	for _, jobID := range pipeline.JobIDs {
		job := s.jobs[jobID]
		if terminal(job.Status) {
			continue
		}
		job.Status = domain.StateCancelled
		job.QueueReason = "pipeline cancelled"
		s.metrics.JobsCancelled++
		for _, attemptID := range job.AttemptIDs {
			attempt := s.attempts[attemptID]
			if active(attempt.Status) {
				attempt.Status = domain.StateCancelled
				attempt.CompletedAt = now
			}
		}
	}
	if pipeline.PRNumber > 0 {
		key := previewKey(pipeline.Repo, pipeline.PRNumber)
		if preview, ok := s.previews[key]; ok && preview.Desired != domain.StateDeleting {
			preview.Desired = domain.StateDeleting
			preview.Generation++
			preview.UpdatedAt = now
		}
	}
}

func (s *Store) activatePreviewLocked(pipeline *domain.Pipeline, environment *domain.Environment, now time.Time) {
	if pipeline.PRNumber <= 0 {
		return
	}
	key := previewKey(pipeline.Repo, pipeline.PRNumber)
	if existing, ok := s.previews[key]; ok && existing.Desired == domain.StateDeleting {
		return
	}
	ttl := time.Duration(environment.TTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	namespace := previewNamespace(pipeline.Repo, pipeline.PRNumber)
	preview := &domain.Preview{
		Repo: pipeline.Repo, PRNumber: pipeline.PRNumber, Namespace: namespace,
		URL:        fmt.Sprintf("https://%s.%s", namespace, s.config.PreviewBaseDomain),
		Generation: 1, Desired: domain.StateActive, Actual: domain.StateActive,
		ExpiresAt: now.Add(ttl), UpdatedAt: now,
	}
	if old, ok := s.previews[key]; ok {
		preview.Generation = old.Generation + 1
	}
	s.previews[key] = preview
	s.metrics.PreviewsCreated++
}

func (s *Store) pipelineViewLocked(id string) domain.PipelineView {
	pipeline := s.pipelines[id]
	view := domain.PipelineView{Pipeline: clonePipeline(pipeline)}
	for _, jobID := range pipeline.JobIDs {
		job := s.jobs[jobID]
		view.Jobs = append(view.Jobs, cloneJob(job))
		for _, attemptID := range job.AttemptIDs {
			view.Attempts = append(view.Attempts, *s.attempts[attemptID])
		}
	}
	return view
}

func clonePipeline(pipeline *domain.Pipeline) domain.Pipeline {
	copy := *pipeline
	copy.JobIDs = append([]string(nil), pipeline.JobIDs...)
	return copy
}

func cloneJob(job *domain.Job) domain.Job {
	copy := *job
	copy.Spec = cloneJobSpec(job.Spec)
	copy.AttemptIDs = append([]string(nil), job.AttemptIDs...)
	return copy
}

func cloneJobSpec(spec domain.JobSpec) domain.JobSpec {
	copy := spec
	copy.Command = append([]string(nil), spec.Command...)
	copy.Needs = append([]string(nil), spec.Needs...)
	copy.Capabilities = append([]string(nil), spec.Capabilities...)
	if spec.Environment != nil {
		environment := *spec.Environment
		copy.Environment = &environment
	}
	return copy
}

func cloneWorker(worker *domain.Worker) *domain.Worker {
	copy := *worker
	copy.Capabilities = make(map[string]bool, len(worker.Capabilities))
	for capability, enabled := range worker.Capabilities {
		copy.Capabilities[capability] = enabled
	}
	return &copy
}

func previewKey(repo string, prNumber int) string {
	return fmt.Sprintf("%s#%d", strings.ToLower(repo), prNumber)
}

func previewNamespace(repo string, prNumber int) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(repo) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		} else if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "-") {
			builder.WriteByte('-')
		}
	}
	name := strings.Trim(builder.String(), "-")
	if len(name) > 40 {
		name = name[:40]
	}
	return fmt.Sprintf("preview-%s-%d", name, prNumber)
}

func newID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate random ID: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func active(state domain.State) bool {
	return state == domain.StateLeased || state == domain.StateRunning
}

func terminal(state domain.State) bool {
	return state == domain.StateSucceeded || state == domain.StateFailed || state == domain.StateCancelled || state == domain.StateLost
}
