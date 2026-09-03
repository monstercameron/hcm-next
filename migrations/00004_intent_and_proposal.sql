-- Owner: data plane. Phase: P1A.
-- DATA-001: intent instances carry exactly the five lifecycle dimensions from
-- specs/business-intent-and-change-request.md. No single `status` column exists.
-- Proposal revisions are immutable records.

-- +goose Up

CREATE TABLE intent_instance (
    tenant_id                tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    intent_id                uuid           NOT NULL,
    definition_ref           semantic_key   NOT NULL,
    definition_version       cas_version    NOT NULL,
    request_digest           content_digest NOT NULL,
    request_digest_algorithm digest_algorithm NOT NULL DEFAULT 'sha256',
    idempotency_key          semantic_key   NOT NULL,
    -- The five lifecycle dimensions. Exactly five, no more, no fewer.
    request_state            text           NOT NULL,
    execution_state          text           NOT NULL,
    business_state           text           NOT NULL,
    consistency_state        text           NOT NULL,
    obligation_state         text           NOT NULL,
    -- Compare-and-swap version for command transitions.
    instance_version         cas_version    NOT NULL DEFAULT 1,
    created_at               timestamptz    NOT NULL,
    recorded_at              timestamptz    NOT NULL DEFAULT now(),
    last_transition_at       timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, intent_id),
    CONSTRAINT intent_instance_idempotency_unique UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT intent_instance_request_state_allowed CHECK (
        request_state IN (
            'DRAFT', 'PREFLIGHTED', 'SIMULATED', 'SUBMITTED', 'APPROVED', 'REJECTED',
            'WITHDRAWN', 'CANCELLED', 'SUPERSEDED', 'CLOSED', 'REOPENED'
        )
    ),
    CONSTRAINT intent_instance_execution_state_allowed CHECK (
        execution_state IN (
            'NOT_PLANNED', 'SCHEDULED', 'REVALIDATING', 'EXECUTING', 'COMMITTED',
            'BLOCKED', 'REPAIR_REQUIRED'
        )
    ),
    CONSTRAINT intent_instance_business_state_allowed CHECK (
        business_state IN (
            'NOT_STARTED', 'IN_PROGRESS', 'COMPLETED', 'NOT_ACHIEVED', 'CORRECTED', 'UNKNOWN'
        )
    ),
    CONSTRAINT intent_instance_consistency_state_allowed CHECK (
        consistency_state IN (
            'NOT_APPLICABLE', 'PENDING_OBSERVATION', 'CONSISTENT', 'DEGRADED',
            'REPAIRING', 'UNKNOWN'
        )
    ),
    CONSTRAINT intent_instance_obligation_state_allowed CHECK (
        obligation_state IN (
            'NOT_APPLICABLE', 'PENDING', 'SATISFIED', 'OVERDUE', 'WAIVED', 'UNKNOWN'
        )
    ),
    -- A terminated request can never be executing.
    CONSTRAINT intent_instance_terminated_not_executing CHECK (
        request_state NOT IN ('CANCELLED', 'SUPERSEDED', 'REJECTED', 'WITHDRAWN')
        OR execution_state <> 'EXECUTING'
    ),
    CONSTRAINT intent_instance_transition_after_creation CHECK (last_transition_at >= created_at)
);

CREATE INDEX intent_instance_definition ON intent_instance (tenant_id, definition_ref);

CREATE TABLE proposal_revision (
    tenant_id         tenant_ref     NOT NULL,
    intent_id         uuid           NOT NULL,
    revision          cas_version    NOT NULL,
    proposal_digest   content_digest NOT NULL,
    material_digest   content_digest NOT NULL,
    digest_algorithm  digest_algorithm NOT NULL DEFAULT 'sha256',
    schema_ref        semantic_key   NOT NULL,
    payload           bytea,
    artifact_ref      text,
    produced_by       text           NOT NULL,
    produced_at       timestamptz    NOT NULL,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, intent_id, revision),
    CONSTRAINT proposal_revision_intent
        FOREIGN KEY (tenant_id, intent_id) REFERENCES intent_instance (tenant_id, intent_id),
    CONSTRAINT proposal_revision_payload_xor_artifact CHECK (
        (payload IS NULL) <> (artifact_ref IS NULL)
    )
);

CREATE TRIGGER proposal_revision_append_only
    BEFORE UPDATE OR DELETE ON proposal_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON proposal_revision FROM PUBLIC;

-- +goose Down
DROP TABLE proposal_revision;
DROP TABLE intent_instance;
