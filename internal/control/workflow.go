package control

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
)

// PRState survives resource deletion to fence delayed webhook deliveries.
type PRState struct {
	UpdatedAt  time.Time
	Closed     bool
	Generation int
	Commit     string
}

// Submission contains normalized, policy-checked source and event identity.
type Submission struct {
	RequestID      string              `json:"request_id"`
	Tenant         string              `json:"tenant"`
	Repo           string              `json:"repo"`
	SourceRepo     string              `json:"source_repo"`
	CommitSHA      string              `json:"commit_sha"`
	Trigger        string              `json:"trigger"`
	PRNumber       int                 `json:"pr_number"`
	Fork           bool                `json:"fork"`
	Closed         bool                `json:"closed"`
	EventTime      time.Time           `json:"event_time"`
	InstallationID int64               `json:"installation_id"`
	Spec           domain.PipelineSpec `json:"spec"`
}

// Submit atomically handles request replay, PR event ordering and supersession.
func (s *Store) Submit(input Submission, now time.Time) (domain.PipelineView, bool, error) {
	if input.RequestID == "" || len(input.RequestID) > 200 {
		return domain.PipelineView{}, false, errors.New("request ID is required and must not exceed 200 characters")
	}
	var order []string
	var err error
	if !input.Closed {
		order, err = planner.Validate(input.Spec)
		if err != nil {
			return domain.PipelineView{}, false, err
		}
	}
	canonical, _ := json.Marshal(input)
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, exists := s.deliveries[input.RequestID]; exists {
		if previous := s.requestHashes[input.RequestID]; previous != "" && previous != hash {
			return domain.PipelineView{}, false, ErrConflict
		}
		s.metrics.WebhookDuplicate++
		return s.pipelineViewLocked(id), true, nil
	}
	key := previewKey(input.Repo, input.PRNumber)
	if input.EventTime.IsZero() {
		input.EventTime = now
	}
	previous, exists := s.prStates[key]
	if input.PRNumber > 0 && exists && (!input.EventTime.After(previous.UpdatedAt)) {
		// Equal timestamps are resolved conservatively: close wins; competing heads are ignored.
		if !input.Closed || previous.Closed || input.EventTime.Before(previous.UpdatedAt) {
			s.deliveries[input.RequestID] = ""
			s.requestHashes[input.RequestID] = hash
			return domain.PipelineView{}, true, nil
		}
	}
	if !input.Closed && s.queuedCountLocked()+len(order) > s.config.MaxQueuedJobs {
		return domain.PipelineView{}, false, ErrCapacity
	}
	configJSON, _ := json.Marshal(input.Spec)
	configSHA := sha256.Sum256(configJSON)
	configHash := hex.EncodeToString(configSHA[:])
	if !input.Closed && !previous.Closed {
		for _, p := range s.pipelines {
			if p.Tenant == input.Tenant && p.Repo == input.Repo && p.PRNumber == input.PRNumber && p.CommitSHA == input.CommitSHA && p.ConfigDigest == configHash && p.Generation == previous.Generation {
				s.deliveries[input.RequestID] = p.ID
				s.requestHashes[input.RequestID] = hash
				if input.PRNumber > 0 {
					previous.UpdatedAt = input.EventTime
					s.prStates[key] = previous
				}
				return s.pipelineViewLocked(p.ID), true, nil
			}
		}
	}
	if input.PRNumber > 0 {
		previous.Generation++
		previous.UpdatedAt = input.EventTime
		previous.Closed = input.Closed
		previous.Commit = input.CommitSHA
		s.prStates[key] = previous
		for _, p := range s.pipelines {
			if p.Repo == input.Repo && p.PRNumber == input.PRNumber && !terminal(p.Status) {
				s.cancelPipelineLocked(p, now)
			}
		}
		if p, ok := s.previews[key]; ok {
			p.Desired = domain.StateDeleting
			p.Generation = previous.Generation
			p.UpdatedAt = now
		}
	}
	s.deliveries[input.RequestID] = ""
	s.requestHashes[input.RequestID] = hash
	s.metrics.WebhookAccepted++
	if input.Closed {
		return domain.PipelineView{}, false, nil
	}
	// Deliveries for the same immutable revision and configuration share one logical pipeline.
	for _, p := range s.pipelines {
		if p.Tenant == input.Tenant && p.Repo == input.Repo && p.PRNumber == input.PRNumber && p.CommitSHA == input.CommitSHA && p.ConfigDigest == configHash && p.Generation == previous.Generation {
			s.deliveries[input.RequestID] = p.ID
			return s.pipelineViewLocked(p.ID), true, nil
		}
	}
	p := s.createPipelineLocked(input.Tenant, input.Repo, input.CommitSHA, input.Trigger, input.PRNumber, input.Spec, order, now)
	p.SourceRepo = input.SourceRepo
	p.Fork = input.Fork
	p.Generation = previous.Generation
	p.ConfigDigest = configHash
	p.InstallationID = input.InstallationID
	s.deliveries[input.RequestID] = p.ID
	return s.pipelineViewLocked(p.ID), false, nil
}

// PipelineList returns a deterministic bounded tenant view, newest first.
func (s *Store) PipelineList(tenant string) []domain.Pipeline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Pipeline{}
	for _, p := range s.pipelines {
		if tenant == "" || p.Tenant == tenant {
			out = append(out, clonePipeline(p))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func (s *Store) PreviewList() []domain.Preview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Preview{}
	for _, p := range s.previews {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace < out[j].Namespace })
	return out
}

// ObservePreview accepts an observation only for the current desired generation.
func (s *Store) ObservePreview(p domain.Preview, actual domain.State, url, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.previews[previewKey(p.Repo, p.PRNumber)]
	if !ok {
		return ErrNotFound
	}
	if current.Generation != p.Generation || current.PipelineID != p.PipelineID || current.Desired != p.Desired {
		return ErrConflict
	}
	current.Actual = actual
	current.URL = url
	current.LastError = lastError
	return nil
}

// AttemptView never exports the secret token in its JSON representation.
func (s *Store) AttemptView(id string) (domain.Attempt, domain.Job, domain.Pipeline, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.attempts[id]
	if !ok {
		return domain.Attempt{}, domain.Job{}, domain.Pipeline{}, ErrNotFound
	}
	j := s.jobs[a.JobID]
	copy := *a
	copy.Artefacts = append([]domain.Artefact(nil), a.Artefacts...)
	return copy, cloneJob(j), clonePipeline(s.pipelines[j.PipelineID]), nil
}

// Finish saves immutable result metadata and the terminal transition together.
func (s *Store) Finish(id, token string, success bool, message, digest, logKey string, artefacts []domain.Artefact, now time.Time) (domain.Attempt, error) {
	// The durable repository serializes this operation with every other mutation.
	s.mu.Lock()
	a, ok := s.attempts[id]
	if !ok {
		s.mu.Unlock()
		return domain.Attempt{}, ErrNotFound
	}
	if a.LeaseToken != token {
		s.mu.Unlock()
		return domain.Attempt{}, ErrStaleLease
	}
	if terminal(a.Status) {
		copy := *a
		s.mu.Unlock()
		wanted := domain.StateFailed
		if success {
			wanted = domain.StateSucceeded
		}
		if copy.Status == wanted && copy.ResultMessage == message && copy.ResultDigest == digest && copy.LogKey == logKey {
			return copy, nil
		}
		return domain.Attempt{}, ErrConflict
	}
	if !now.Before(a.LeaseExpires) {
		s.mu.Unlock()
		return domain.Attempt{}, ErrStaleLease
	}
	a.ResultDigest = digest
	a.LogKey = logKey
	a.Artefacts = append([]domain.Artefact(nil), artefacts...)
	s.mu.Unlock()
	return s.CompleteAttempt(id, token, success, message, now)
}

func (s *Store) RecordCheck(id string, checkID int64, state domain.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return ErrNotFound
	}
	p.CheckRunID = checkID
	p.CheckState = state
	return nil
}

func (s *Store) JobView(id string) (domain.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return domain.Job{}, ErrNotFound
	}
	return cloneJob(j), nil
}

func (s *Store) ActiveAttemptIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := []string{}
	for id, a := range s.attempts {
		if active(a.Status) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// EnsurePreviews retries desired creation after a superseded namespace has disappeared.
func (s *Store) EnsurePreviews(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.pipelines {
		if p.Status != domain.StateSucceeded || p.PRNumber == 0 {
			continue
		}
		if _, exists := s.previews[previewKey(p.Repo, p.PRNumber)]; exists {
			continue
		}
		for _, id := range p.JobIDs {
			j := s.jobs[id]
			if j.Spec.Environment != nil && now.Before(p.CreatedAt.Add(time.Duration(j.Spec.Environment.TTLMinutes)*time.Minute)) {
				s.activatePreviewLocked(p, j.Spec.Environment, now)
			}
		}
	}
}

func (s *Store) queuedCountLocked() int {
	n := 0
	for _, j := range s.jobs {
		if !terminal(j.Status) {
			n++
		}
	}
	return n
}
func (s *Store) usedResourcesLocked(tenant, worker string) domain.Resources {
	used := domain.Resources{}
	for _, a := range s.attempts {
		if !active(a.Status) {
			continue
		}
		j := s.jobs[a.JobID]
		if (tenant == "" || tenant == j.Tenant) && (worker == "" || worker == a.WorkerID) {
			used.CPU += j.Spec.Resources.CPU
			used.Memory += j.Spec.Resources.Memory
		}
	}
	return used
}
func jobTimeout(job *domain.Job) time.Duration {
	n := job.Spec.TimeoutSeconds
	if n <= 0 {
		n = 900
	}
	return time.Duration(n) * time.Second
}
func (s *Store) pruneLocked(now time.Time) {
	for id, p := range s.pipelines {
		if !terminal(p.Status) || now.Sub(p.CreatedAt) < s.config.Retention {
			continue
		}
		retained := false
		for _, v := range s.previews {
			if v.PipelineID == id {
				retained = true
			}
		}
		if retained {
			continue
		}
		for _, jid := range p.JobIDs {
			for _, aid := range s.jobs[jid].AttemptIDs {
				delete(s.attempts, aid)
			}
			delete(s.jobs, jid)
		}
		delete(s.pipelines, id)
	}
}

// Snapshot is a versioned internal persistence format, never an API response.
type Snapshot struct {
	Version       int
	Pipelines     map[string]*domain.Pipeline
	Jobs          map[string]*domain.Job
	Attempts      map[string]*domain.Attempt
	Workers       map[string]*domain.Worker
	Previews      map[string]*domain.Preview
	Deliveries    map[string]string
	TenantLimits  map[string]int
	PRStates      map[string]PRState
	RequestHashes map[string]string
	LastTenant    string
	Sequence      uint64
	Metrics       Metrics
}

func (s *Store) Snapshot() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(Snapshot{1, s.pipelines, s.jobs, s.attempts, s.workers, s.previews, s.deliveries, s.tenantLimits, s.prStates, s.requestHashes, s.lastTenant, s.sequence, s.metrics})
	return b.Bytes(), err
}
func Restore(config Config, data []byte) (*Store, error) {
	s := New(config)
	if len(data) == 0 {
		return s, nil
	}
	var snap Snapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snap); err != nil {
		return nil, err
	}
	if snap.Version != 1 {
		return nil, errors.New("unsupported state schema version")
	}
	s.pipelines = snap.Pipelines
	s.jobs = snap.Jobs
	s.attempts = snap.Attempts
	s.workers = snap.Workers
	s.previews = snap.Previews
	s.deliveries = snap.Deliveries
	s.tenantLimits = snap.TenantLimits
	s.prStates = snap.PRStates
	s.requestHashes = snap.RequestHashes
	s.lastTenant = snap.LastTenant
	s.sequence = snap.Sequence
	s.metrics = snap.Metrics
	return s, nil
}
