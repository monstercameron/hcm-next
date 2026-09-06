-- Owner: workforce identity. Phase: production frontend.
-- Organization-scoped worker number policy and append-only reservations.

-- +goose Up

CREATE TABLE organization_worker_id_policy (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    organization_scope_id text       NOT NULL,
    version               bigint     NOT NULL DEFAULT 1,
    prefix                text       NOT NULL DEFAULT 'HC',
    suffix                text       NOT NULL DEFAULT '',
    separator             text       NOT NULL DEFAULT '-',
    sequence_digits       smallint   NOT NULL DEFAULT 6,
    start_at              bigint     NOT NULL DEFAULT 1000,
    next_sequence         bigint     NOT NULL DEFAULT 1000,
    increment_by          bigint     NOT NULL DEFAULT 1,
    zero_pad              boolean    NOT NULL DEFAULT true,
    year_format           text       NOT NULL DEFAULT 'NONE',
    include_unit_code     boolean    NOT NULL DEFAULT false,
    check_digit           text       NOT NULL DEFAULT 'NONE',
    excluded_ranges       text       NOT NULL DEFAULT '',
    updated_by            text       NOT NULL,
    updated_at            timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, organization_scope_id),
    CONSTRAINT worker_id_policy_scope_nonempty CHECK (organization_scope_id <> ''),
    CONSTRAINT worker_id_policy_version_positive CHECK (version >= 1),
    CONSTRAINT worker_id_policy_digits CHECK (sequence_digits BETWEEN 1 AND 12),
    CONSTRAINT worker_id_policy_sequence_nonnegative CHECK (start_at >= 0 AND next_sequence >= start_at),
    CONSTRAINT worker_id_policy_increment CHECK (increment_by BETWEEN 1 AND 1000000),
    CONSTRAINT worker_id_policy_separator CHECK (separator IN ('', '-', '/', '.')),
    CONSTRAINT worker_id_policy_year CHECK (year_format IN ('NONE', 'YY', 'YYYY')),
    CONSTRAINT worker_id_policy_check_digit CHECK (check_digit IN ('NONE', 'LUHN_MOD10'))
);

CREATE TABLE worker_id_reservation (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    organization_scope_id text       NOT NULL,
    worker_number         text       NOT NULL,
    sequence_value        bigint     NOT NULL,
    reserved_by           text       NOT NULL,
    reserved_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, worker_number),
    CONSTRAINT worker_id_reservation_scope_nonempty CHECK (organization_scope_id <> ''),
    CONSTRAINT worker_id_reservation_actor_nonempty CHECK (reserved_by <> ''),
    CONSTRAINT worker_id_reservation_sequence_nonnegative CHECK (sequence_value >= 0)
);

-- Existing numbers predate organization-aware allocation. Reserving them
-- under a legacy scope prevents a newly configured organization from issuing
-- the same human-facing identifier.
INSERT INTO worker_id_reservation (tenant_id, organization_scope_id, worker_number, sequence_value, reserved_by)
SELECT tenant_id, 'legacy', worker_number, 0, 'migration:00181'
FROM journey_worker
ON CONFLICT DO NOTHING;

ALTER TABLE journey_worker
    ADD CONSTRAINT journey_worker_number_unique UNIQUE (tenant_id, worker_number);

CREATE OR REPLACE TRIGGER worker_id_reservation_append_only
    BEFORE UPDATE OR DELETE ON worker_id_reservation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE organization_worker_id_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_worker_id_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_worker_id_policy
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE worker_id_reservation ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_id_reservation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker_id_reservation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON organization_worker_id_policy TO hcmnext_app;
GRANT SELECT, INSERT ON worker_id_reservation TO hcmnext_app;

COMMENT ON TABLE organization_worker_id_policy IS
    'Organization-scoped formatting policy and locked monotonic sequence for human-facing worker numbers.';
COMMENT ON TABLE worker_id_reservation IS
    'Append-only tenant-wide uniqueness ledger; reservations are never reused even when a later hire fails.';

-- +goose Down
REVOKE ALL ON worker_id_reservation FROM hcmnext_app;
REVOKE ALL ON organization_worker_id_policy FROM hcmnext_app;
DROP POLICY tenant_isolation ON worker_id_reservation;
ALTER TABLE worker_id_reservation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE worker_id_reservation DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON organization_worker_id_policy;
ALTER TABLE organization_worker_id_policy NO FORCE ROW LEVEL SECURITY;
ALTER TABLE organization_worker_id_policy DISABLE ROW LEVEL SECURITY;
DROP TRIGGER worker_id_reservation_append_only ON worker_id_reservation;
ALTER TABLE journey_worker DROP CONSTRAINT journey_worker_number_unique;
DROP TABLE worker_id_reservation;
DROP TABLE organization_worker_id_policy;
