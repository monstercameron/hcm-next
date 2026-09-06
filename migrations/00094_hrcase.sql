-- Owner: HR case domain/data plane. Phase: P3.
-- PERSIST-HRCASE-001: the append-only, tenant-isolated HR case revision
-- stream backing internal/domains/hrcase.CaseRevision and CaseAggregate.
--
-- Storage disposition: hr_case_revision is LEDGER, PERMANENT retention, and
-- has no rebuild source. "Current" is the row with the highest
-- event_sequence for a (tenant_id, case_id); state is never updated in place.

-- +goose Up

CREATE TABLE IF NOT EXISTS hr_case_revision (
    tenant_id        tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid        NOT NULL,
    case_id          semantic_key NOT NULL,
    revision         bigint      NOT NULL,
    definition       jsonb       NOT NULL,
    requester        text,
    subject          text,
    purpose          text,
    classification   text,
    retention        text,
    participants     jsonb       NOT NULL,
    state            text        NOT NULL,
    previous_revision bigint,
    disposition      text,
    digest           content_digest NOT NULL,
    event_sequence   bigint      NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT hr_case_revision_event_sequence_unique
        UNIQUE (tenant_id, case_id, event_sequence),
    CONSTRAINT hr_case_revision_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT hr_case_revision_event_sequence_positive
        CHECK (event_sequence >= 1),
    CONSTRAINT hr_case_revision_state_allowed
        CHECK (state IN ('DRAFT', 'OPEN', 'WAITING', 'PAUSED', 'RESOLVED', 'CLOSED', 'REOPENED', 'APPEALED')),
    CONSTRAINT hr_case_revision_definition_object
        CHECK (jsonb_typeof(definition) = 'object'),
    CONSTRAINT hr_case_revision_participants_array
        CHECK (jsonb_typeof(participants) = 'array')
);

CREATE INDEX IF NOT EXISTS hr_case_revision_latest
    ON hr_case_revision (tenant_id, case_id, event_sequence DESC);

CREATE OR REPLACE TRIGGER hr_case_revision_append_only
    BEFORE UPDATE OR DELETE ON hr_case_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON hr_case_revision FROM PUBLIC;

ALTER TABLE hr_case_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE hr_case_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON hr_case_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON hr_case_revision TO hcmnext_app;

-- +goose Down

REVOKE ALL ON hr_case_revision FROM hcmnext_app;
DROP POLICY tenant_isolation ON hr_case_revision;
ALTER TABLE hr_case_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE hr_case_revision DISABLE ROW LEVEL SECURITY;
DROP TABLE hr_case_revision;
