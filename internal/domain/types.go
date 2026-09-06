package domain

import "time"

// State is a durable lifecycle state for pipelines, jobs, attempts, and previews.
type State string

const (
	StateQueued    State = "QUEUED"
	StateLeased    State = "LEASED"
	StateRunning   State = "RUNNING"
	StateSucceeded State = "SUCCEEDED"
	StateFailed    State = "FAILED"
	StateLost      State = "LOST"
	StateCancelled State = "CANCELLED"
	StateActive    State = "ACTIVE"
	StateDeleting  State = "DELETING"
)

// Resources expresses the scheduler capacity required by a job.
type Resources struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory_mb"`
}

// Environment requests a preview environment after a job succeeds.
type Environment struct {
	TTLMinutes int    `json:"ttl_minutes"`
	Exposure   string `json:"exposure"`
}

// JobSpec is the immutable configuration used to plan one job.
type JobSpec struct {
	Image        string       `json:"image"`
	Command      []string     `json:"command"`
	Needs        []string     `json:"needs,omitempty"`
	Priority     int          `json:"priority,omitempty"`
	Capabilities []string     `json:"capabilities,omitempty"`
	Trusted      bool         `json:"trusted,omitempty"`
	Resources    Resources    `json:"resources"`
	Environment  *Environment `json:"environment,omitempty"`
}

// PipelineSpec is the repository-owned CI configuration.
type PipelineSpec struct {
	Version int                `json:"version"`
	Jobs    map[string]JobSpec `json:"jobs"`
}

// Pipeline is an immutable plan plus its current aggregate state.
type Pipeline struct {
	ID        string    `json:"id"`
	Tenant    string    `json:"tenant"`
	Repo      string    `json:"repo"`
	CommitSHA string    `json:"commit_sha"`
	Trigger   string    `json:"trigger"`
	PRNumber  int       `json:"pr_number,omitempty"`
	Status    State     `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	JobIDs    []string  `json:"job_ids"`
}

// Job is a planned unit of work in a dependency DAG.
type Job struct {
	ID          string    `json:"id"`
	PipelineID  string    `json:"pipeline_id"`
	Tenant      string    `json:"tenant"`
	Name        string    `json:"name"`
	Spec        JobSpec   `json:"spec"`
	Status      State     `json:"status"`
	QueueReason string    `json:"queue_reason,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	AttemptIDs  []string  `json:"attempt_ids"`
}

// Attempt is one lease-protected execution of a job.
type Attempt struct {
	ID            string    `json:"id"`
	JobID         string    `json:"job_id"`
	Number        int       `json:"number"`
	WorkerID      string    `json:"worker_id"`
	LeaseToken    string    `json:"-"`
	LeaseExpires  time.Time `json:"lease_expires_at"`
	Status        State     `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	CompletedAt   time.Time `json:"completed_at,omitempty"`
	ResultMessage string    `json:"result_message,omitempty"`
}

// AttemptLease is the worker-facing assignment and includes the secret lease token.
type AttemptLease struct {
	Attempt    Attempt   `json:"attempt"`
	LeaseToken string    `json:"lease_token"`
	Job        Job       `json:"job"`
	Pipeline   Pipeline  `json:"pipeline"`
	Deadline   time.Time `json:"deadline"`
}

// Worker advertises an isolated execution pool and its capabilities.
type Worker struct {
	ID           string          `json:"id"`
	Pool         string          `json:"pool"`
	Capabilities map[string]bool `json:"capabilities"`
	Capacity     int             `json:"capacity"`
	Trusted      bool            `json:"trusted"`
	Heartbeat    time.Time       `json:"heartbeat"`
}

// Preview represents desired and observed pull-request environment state.
type Preview struct {
	Repo       string    `json:"repo"`
	PRNumber   int       `json:"pr_number"`
	Namespace  string    `json:"namespace"`
	URL        string    `json:"url"`
	Generation int       `json:"generation"`
	Desired    State     `json:"desired_state"`
	Actual     State     `json:"actual_state"`
	ExpiresAt  time.Time `json:"expires_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PipelineView is a consistent read model for the API.
type PipelineView struct {
	Pipeline Pipeline  `json:"pipeline"`
	Jobs     []Job     `json:"jobs"`
	Attempts []Attempt `json:"attempts"`
}
