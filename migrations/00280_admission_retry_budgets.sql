-- +goose Up
CREATE TABLE admission_retry_budget (
    budget_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    service text NOT NULL CHECK (btrim(service) <> ''),
    dependency text NOT NULL CHECK (btrim(dependency) <> ''),
    logical_operation_id text NOT NULL CHECK (btrim(logical_operation_id) <> ''),
    operation_kind text NOT NULL CHECK (btrim(operation_kind) <> ''),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('ACTIVE','EXPIRED','REVOKED')),
    allowed bigint NOT NULL CHECK (allowed >= 0),
    consumed bigint NOT NULL DEFAULT 0 CHECK (consumed >= 0),
    refunded bigint NOT NULL DEFAULT 0 CHECK (refunded >= 0),
    retryable text[] NOT NULL DEFAULT '{}' CHECK (retryable <@ ARRAY['TRANSIENT','UNAVAILABLE','THROTTLED','TIMEOUT']::text[]),
    version text NOT NULL CHECK (btrim(version) <> ''),
    owner text NOT NULL CHECK (btrim(owner) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, budget_id),
    CHECK (period_end > period_start),
    CHECK (expires_at > period_start AND expires_at <= period_end),
    CHECK (consumed::numeric <= allowed::numeric + refunded::numeric),
    UNIQUE (tenant_id, service, dependency, operation_kind, logical_operation_id, period_start)
);
CREATE TABLE admission_retry_receipt (
    receipt_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    budget_id uuid NOT NULL REFERENCES admission_retry_budget(budget_id),
    attempt_id uuid NOT NULL,
    service text NOT NULL CHECK (btrim(service) <> ''),
    dependency text NOT NULL CHECK (btrim(dependency) <> ''),
    logical_operation_id text NOT NULL,
    operation_kind text NOT NULL CHECK (btrim(operation_kind) <> ''),
    budget_version text NOT NULL CHECK (btrim(budget_version) <> ''),
    attempt integer NOT NULL CHECK (attempt > 0),
    failure_class text NOT NULL CHECK (failure_class IN ('TRANSIENT','UNAVAILABLE','THROTTLED','TIMEOUT')),
    receipt_digest text NOT NULL CHECK (receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    consumed bigint NOT NULL CHECK (consumed >= 0),
    remaining bigint NOT NULL CHECK (remaining >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, budget_id, attempt_id),
    FOREIGN KEY (tenant_id, budget_id) REFERENCES admission_retry_budget(tenant_id, budget_id)
);
CREATE INDEX admission_retry_budget_scope ON admission_retry_budget (tenant_id, service, dependency, operation_kind);
ALTER TABLE admission_retry_budget ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON admission_retry_budget USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
ALTER TABLE admission_retry_receipt ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON admission_retry_receipt USING (tenant_id = current_setting('app.tenant_id', true)::uuid) WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE TRIGGER admission_retry_receipt_forbid_mutation BEFORE UPDATE OR DELETE ON admission_retry_receipt FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION '00280 is irreversible: retry receipts and consumed budget counters are durable evidence';
END $$;
-- +goose StatementEnd
