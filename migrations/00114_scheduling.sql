-- PERSIST-SCHEDULING-001: durable appointment, availability and clock-device
-- semantics. These are one table set because the three domains do not import
-- one another and each contributes at most two tables. If a domain gains a
-- dependent aggregate, it gets a separate migration and storage disposition.
--
-- Storage disposition:
-- appointment_requirement | 00114 | internal/data/schedulingstore | AGGREGATE | tenant_id | false | PERMANENT | none
-- resource_type           | 00114 | internal/data/schedulingstore | AGGREGATE | tenant_id | false | PERMANENT | none
-- worker_availability     | 00114 | internal/data/schedulingstore | LEDGER    | tenant_id | true  | PERMANENT | none
-- time_device_registration| 00114 | internal/data/schedulingstore | AGGREGATE | tenant_id | false | PERMANENT | none

-- +goose Up

CREATE TABLE IF NOT EXISTS appointment_requirement (
    row_id          uuid        NOT NULL,
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    requirement_id  text        NOT NULL,
    version         text        NOT NULL,
    revision        cas_version NOT NULL,
    purpose         text        NOT NULL,
    participants    jsonb       NOT NULL,
    duration        interval    NOT NULL,
    window_start    timestamptz,
    window_end      timestamptz,
    location_class  text,
    privacy_class   text        NOT NULL,
    lead_time       interval,
    state           text        NOT NULL,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, requirement_id, revision),
    CONSTRAINT appointment_requirement_window_pair CHECK (
        (window_start IS NULL) = (window_end IS NULL)
    ),
    CONSTRAINT appointment_requirement_window_order CHECK (
        window_end IS NULL OR window_end > window_start
    )
);

CREATE OR REPLACE TRIGGER appointment_requirement_forbid_mutation
    BEFORE UPDATE OR DELETE ON appointment_requirement
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON appointment_requirement FROM PUBLIC;

CREATE TABLE IF NOT EXISTS resource_type (
    row_id          uuid        NOT NULL,
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    resource_type_id text       NOT NULL,
    version         text        NOT NULL,
    revision        cas_version NOT NULL,
    name            text        NOT NULL,
    kind            text        NOT NULL,
    capacity_mode   text        NOT NULL,
    capacity_limit  numeric(9,2),
    privacy_class   text,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, resource_type_id, revision),
    CONSTRAINT resource_type_capacity_mode_allowed CHECK (
        capacity_mode IN ('EXCLUSIVE', 'SHARED', 'UNLIMITED')
    )
);

CREATE OR REPLACE TRIGGER resource_type_forbid_mutation
    BEFORE UPDATE OR DELETE ON resource_type
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON resource_type FROM PUBLIC;

CREATE TABLE IF NOT EXISTS worker_availability (
    row_id          uuid        NOT NULL,
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    availability_id text        NOT NULL,
    revision        cas_version NOT NULL,
    worker_ref      uuid        NOT NULL,
    employment_ref  uuid,
    assignment_ref  uuid,
    scope           text        NOT NULL,
    state           text        NOT NULL,
    reason          text,
    effective_from  timestamptz,
    effective_to    timestamptz,
    source          text        NOT NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, availability_id, revision),
    CONSTRAINT worker_availability_effective_order CHECK (
        effective_to IS NULL OR effective_to > effective_from
    )
);

CREATE OR REPLACE TRIGGER worker_availability_forbid_mutation
    BEFORE UPDATE OR DELETE ON worker_availability
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON worker_availability FROM PUBLIC;

CREATE TABLE IF NOT EXISTS time_device_registration (
    row_id             uuid        NOT NULL,
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    device_id          text        NOT NULL,
    version            text        NOT NULL,
    state              text        NOT NULL,
    owner_ref          uuid,
    location_ref       uuid,
    clock_trust_policy jsonb       NOT NULL,
    offline_policy     jsonb,
    replay_policy      jsonb,
    signature_policy   jsonb,
    firmware_policy    jsonb,
    certificate_policy jsonb,
    retention_policy   jsonb,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, device_id, version)
);

CREATE OR REPLACE TRIGGER time_device_registration_forbid_mutation
    BEFORE UPDATE OR DELETE ON time_device_registration
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON time_device_registration FROM PUBLIC;

ALTER TABLE appointment_requirement ENABLE ROW LEVEL SECURITY;
ALTER TABLE appointment_requirement FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON appointment_requirement
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE resource_type ENABLE ROW LEVEL SECURITY;
ALTER TABLE resource_type FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON resource_type
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE worker_availability ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_availability FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker_availability
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE time_device_registration ENABLE ROW LEVEL SECURITY;
ALTER TABLE time_device_registration FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON time_device_registration
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON appointment_requirement TO hcmnext_app;
GRANT SELECT, INSERT ON resource_type TO hcmnext_app;
GRANT SELECT, INSERT ON worker_availability TO hcmnext_app;
GRANT SELECT, INSERT ON time_device_registration TO hcmnext_app;

-- +goose Down

REVOKE ALL ON time_device_registration FROM hcmnext_app;
REVOKE ALL ON worker_availability FROM hcmnext_app;
REVOKE ALL ON resource_type FROM hcmnext_app;
REVOKE ALL ON appointment_requirement FROM hcmnext_app;

DROP POLICY tenant_isolation ON time_device_registration;
ALTER TABLE time_device_registration NO FORCE ROW LEVEL SECURITY;
ALTER TABLE time_device_registration DISABLE ROW LEVEL SECURITY;
DROP TABLE time_device_registration;

DROP POLICY tenant_isolation ON worker_availability;
ALTER TABLE worker_availability NO FORCE ROW LEVEL SECURITY;
ALTER TABLE worker_availability DISABLE ROW LEVEL SECURITY;
DROP TABLE worker_availability;

DROP POLICY tenant_isolation ON resource_type;
ALTER TABLE resource_type NO FORCE ROW LEVEL SECURITY;
ALTER TABLE resource_type DISABLE ROW LEVEL SECURITY;
DROP TABLE resource_type;

DROP POLICY tenant_isolation ON appointment_requirement;
ALTER TABLE appointment_requirement NO FORCE ROW LEVEL SECURITY;
ALTER TABLE appointment_requirement DISABLE ROW LEVEL SECURITY;
DROP TABLE appointment_requirement;
