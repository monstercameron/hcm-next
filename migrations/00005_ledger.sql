-- Owner: data plane. Phase: P1A.
-- LEDGER-001 / LEDGER-004: tenant-scoped stream registry, stream head, hash
-- partitioned append-only ledger_event, typed payload schema references and
-- source-authority assignments.
--
-- Authority is attached to the assertion, never inferred from the fact that an
-- event was recorded (specs/transaction-ledger-reconciliation-and-repair.md 8.5).

-- +goose Up

CREATE TABLE payload_schema (
    tenant_id               tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    schema_ref              semantic_key NOT NULL,
    schema_id               semantic_key NOT NULL,
    schema_version          cas_version  NOT NULL,
    message_full_name       semantic_key NOT NULL,
    wire_format             text         NOT NULL,
    canonicalization_profile text        NOT NULL,
    recorded_at             timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, schema_ref),
    CONSTRAINT payload_schema_version_unique UNIQUE (tenant_id, schema_id, schema_version),
    CONSTRAINT payload_schema_wire_format_allowed CHECK (wire_format IN ('PROTOBUF')),
    CONSTRAINT payload_schema_profile_allowed CHECK (
        canonicalization_profile IN (
            'LEDGER_EVENT', 'PROPOSAL', 'IDEMPOTENT_REQUEST', 'CONFIG_BUNDLE',
            'EVIDENCE_MANIFEST', 'ARTIFACT_REFERENCE', 'EXTERNAL_PAYLOAD_EVIDENCE'
        )
    )
);

CREATE TABLE authority_assignment (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    authority_ref  semantic_key NOT NULL,
    authority_kind text         NOT NULL,
    domain_scope   semantic_key NOT NULL,
    -- Half-open business interval over which this authority governs the scope.
    effective_from timestamptz  NOT NULL,
    effective_to   timestamptz,
    recorded_at    timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, authority_ref),
    CONSTRAINT authority_assignment_kind_allowed CHECK (
        authority_kind IN ('INTERNAL', 'EXTERNAL_SYSTEM', 'HUMAN', 'IMPORT', 'AGENT')
    ),
    CONSTRAINT authority_assignment_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);

CREATE TABLE ledger_stream (
    tenant_id   tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    stream_key  semantic_key NOT NULL,
    stream_kind text         NOT NULL,
    subject_ref semantic_key NOT NULL,
    created_at  timestamptz  NOT NULL DEFAULT now(),
    recorded_at timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, stream_key),
    CONSTRAINT ledger_stream_kind_allowed CHECK (
        stream_kind IN (
            'TENANT', 'PERSON', 'WORKER', 'EMPLOYMENT', 'POSITION', 'PAYROLL_RUN',
            'BENEFIT_ENROLLMENT', 'TIMECARD', 'WORKFLOW_INSTANCE',
            'INTEGRATION_OPERATION', 'TRANSACTION'
        )
    )
);

CREATE TABLE stream_head (
    tenant_id             tenant_ref   NOT NULL,
    stream_key            semantic_key NOT NULL,
    head_sequence         bigint       NOT NULL DEFAULT 0,
    head_digest           content_digest,
    head_digest_algorithm digest_algorithm,
    updated_at            timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, stream_key),
    CONSTRAINT stream_head_stream
        FOREIGN KEY (tenant_id, stream_key) REFERENCES ledger_stream (tenant_id, stream_key),
    CONSTRAINT stream_head_sequence_non_negative CHECK (head_sequence >= 0),
    CONSTRAINT stream_head_digest_matches_sequence CHECK (
        (head_sequence = 0) = (head_digest IS NULL)
    ),
    CONSTRAINT stream_head_digest_algorithm_present CHECK (
        (head_digest IS NULL) = (head_digest_algorithm IS NULL)
    )
);

CREATE TABLE ledger_event (
    tenant_id           tenant_ref     NOT NULL,
    stream_key          semantic_key   NOT NULL,
    sequence            bigint         NOT NULL,
    event_id            uuid           NOT NULL,
    assertion_class     text           NOT NULL,
    authority_ref       semantic_key,
    source_ref          semantic_key   NOT NULL,
    schema_ref          semantic_key   NOT NULL,
    payload             bytea,
    artifact_ref        text,
    canonical_length    integer        NOT NULL,
    digest              content_digest NOT NULL,
    digest_algorithm    digest_algorithm NOT NULL,
    -- Three distinct times (spec 8.11).
    occurred_at         timestamptz    NOT NULL,
    effective_at        timestamptz    NOT NULL,
    recorded_at         timestamptz    NOT NULL DEFAULT now(),
    correlation_id      uuid           NOT NULL,
    causation_id        uuid,
    idempotency_key     semantic_key   NOT NULL,
    corrects_stream_key semantic_key,
    corrects_sequence   bigint,
    PRIMARY KEY (tenant_id, stream_key, sequence),
    CONSTRAINT ledger_event_id_unique UNIQUE (tenant_id, event_id),
    CONSTRAINT ledger_event_idempotency_unique UNIQUE (tenant_id, stream_key, idempotency_key),
    CONSTRAINT ledger_event_sequence_positive CHECK (sequence >= 1),
    CONSTRAINT ledger_event_assertion_class_allowed CHECK (
        assertion_class IN (
            'TRANSACTION_FACT', 'DOMAIN_FACT', 'EXTERNAL_OBSERVATION', 'CLAIM', 'CORRECTION'
        )
    ),
    -- Recording an assertion never implies authority over its subject: the two
    -- authority-bearing classes must name an authority assignment.
    CONSTRAINT ledger_event_authority_required CHECK (
        assertion_class NOT IN ('DOMAIN_FACT', 'EXTERNAL_OBSERVATION')
        OR authority_ref IS NOT NULL
    ),
    -- A correction must reference the exact assertion it corrects.
    CONSTRAINT ledger_event_correction_target CHECK (
        (assertion_class = 'CORRECTION')
        = (corrects_stream_key IS NOT NULL AND corrects_sequence IS NOT NULL)
    ),
    CONSTRAINT ledger_event_correction_sequence_positive CHECK (
        corrects_sequence IS NULL OR corrects_sequence >= 1
    ),
    -- Exactly one of inline typed payload bytes or a governed artifact reference.
    CONSTRAINT ledger_event_payload_xor_artifact CHECK (
        (payload IS NULL) <> (artifact_ref IS NULL)
    ),
    -- Large bytes never enter the envelope; they become an artifact reference.
    CONSTRAINT ledger_event_payload_bounded CHECK (
        payload IS NULL OR octet_length(payload) <= 65536
    ),
    CONSTRAINT ledger_event_canonical_length_non_negative CHECK (canonical_length >= 0),
    CONSTRAINT ledger_event_stream
        FOREIGN KEY (tenant_id, stream_key) REFERENCES ledger_stream (tenant_id, stream_key),
    CONSTRAINT ledger_event_schema
        FOREIGN KEY (tenant_id, schema_ref) REFERENCES payload_schema (tenant_id, schema_ref),
    CONSTRAINT ledger_event_authority
        FOREIGN KEY (tenant_id, authority_ref) REFERENCES authority_assignment (tenant_id, authority_ref)
) PARTITION BY HASH (tenant_id);

CREATE TABLE ledger_event_p0 PARTITION OF ledger_event FOR VALUES WITH (MODULUS 4, REMAINDER 0);
CREATE TABLE ledger_event_p1 PARTITION OF ledger_event FOR VALUES WITH (MODULUS 4, REMAINDER 1);
CREATE TABLE ledger_event_p2 PARTITION OF ledger_event FOR VALUES WITH (MODULUS 4, REMAINDER 2);
CREATE TABLE ledger_event_p3 PARTITION OF ledger_event FOR VALUES WITH (MODULUS 4, REMAINDER 3);

CREATE INDEX ledger_event_correlation ON ledger_event (tenant_id, correlation_id);
CREATE INDEX ledger_event_effective ON ledger_event (tenant_id, stream_key, effective_at);

-- Append-only enforcement. The trigger is declared on the partitioned parent and
-- is cloned onto every partition, so it holds no matter which table is targeted.
CREATE TRIGGER ledger_event_append_only
    BEFORE UPDATE OR DELETE ON ledger_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON ledger_event FROM PUBLIC;
REVOKE UPDATE, DELETE ON ledger_event_p0 FROM PUBLIC;
REVOKE UPDATE, DELETE ON ledger_event_p1 FROM PUBLIC;
REVOKE UPDATE, DELETE ON ledger_event_p2 FROM PUBLIC;
REVOKE UPDATE, DELETE ON ledger_event_p3 FROM PUBLIC;

-- +goose Down
DROP TABLE ledger_event;
DROP TABLE stream_head;
DROP TABLE ledger_stream;
DROP TABLE authority_assignment;
DROP TABLE payload_schema;
