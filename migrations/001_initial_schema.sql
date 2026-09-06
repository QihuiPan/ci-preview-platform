BEGIN;

CREATE TYPE pipeline_status AS ENUM ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED');
CREATE TYPE job_status AS ENUM ('QUEUED', 'LEASED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'LOST', 'CANCELLED');
CREATE TYPE preview_status AS ENUM ('ACTIVE', 'DELETING');

CREATE TABLE webhook_deliveries (
    delivery_id text PRIMARY KEY,
    event_type text NOT NULL,
    repository text NOT NULL,
    payload_digest bytea NOT NULL,
    pipeline_id uuid,
    received_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE pipelines (
    id uuid PRIMARY KEY,
    tenant text NOT NULL,
    repository text NOT NULL,
    commit_sha text NOT NULL,
    trigger text NOT NULL,
    pr_number integer,
    configuration jsonb NOT NULL,
    status pipeline_status NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pr_number_positive CHECK (pr_number IS NULL OR pr_number > 0)
);

CREATE TABLE jobs (
    id uuid PRIMARY KEY,
    pipeline_id uuid NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    tenant text NOT NULL,
    name text NOT NULL,
    dependencies jsonb NOT NULL DEFAULT '[]'::jsonb,
    specification jsonb NOT NULL,
    priority smallint NOT NULL DEFAULT 0,
    status job_status NOT NULL,
    queue_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_id, name),
    CONSTRAINT bounded_priority CHECK (priority BETWEEN -10 AND 10)
);

CREATE TABLE workers (
    id text PRIMARY KEY,
    pool text NOT NULL,
    capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
    capacity integer NOT NULL,
    trusted boolean NOT NULL DEFAULT false,
    heartbeat timestamptz NOT NULL,
    CONSTRAINT positive_capacity CHECK (capacity > 0)
);

CREATE TABLE attempts (
    id uuid PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    number integer NOT NULL,
    worker_id text NOT NULL REFERENCES workers(id),
    lease_token_hash bytea NOT NULL,
    lease_expires_at timestamptz NOT NULL,
    status job_status NOT NULL,
    result_message text,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    UNIQUE (job_id, number)
);

CREATE UNIQUE INDEX one_active_attempt_per_job
    ON attempts (job_id)
    WHERE status IN ('LEASED', 'RUNNING');

CREATE INDEX runnable_jobs_by_tenant
    ON jobs (tenant, priority DESC, created_at)
    WHERE status = 'QUEUED';

CREATE INDEX expiring_attempt_leases
    ON attempts (lease_expires_at)
    WHERE status IN ('LEASED', 'RUNNING');

CREATE TABLE artefacts (
    id uuid PRIMARY KEY,
    attempt_id uuid NOT NULL REFERENCES attempts(id) ON DELETE CASCADE,
    digest text NOT NULL,
    size_bytes bigint NOT NULL,
    object_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (digest),
    CONSTRAINT nonnegative_artefact_size CHECK (size_bytes >= 0)
);

CREATE TABLE previews (
    repository text NOT NULL,
    pr_number integer NOT NULL,
    namespace text NOT NULL,
    url text,
    generation bigint NOT NULL,
    desired_state preview_status NOT NULL,
    actual_state preview_status NOT NULL,
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, pr_number),
    UNIQUE (namespace)
);

CREATE TABLE audit_events (
    id bigserial PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor text NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    details jsonb NOT NULL DEFAULT '{}'::jsonb
);

COMMIT;
