-- Owner: connectivity. Phase: P1A.
-- INTG-008 / INTG-009: immutable external observation evidence and the fenced
-- checkpoints that make a bounded read resumable.
--
-- An observation is never a domain fact. It records that a named external
-- authority reported something, under a named schema version, at a named
-- position in a named source snapshot. Promotion to truth is a governed
-- decision made elsewhere, so classification is constrained to the single
-- value EXTERNAL_OBSERVATION here rather than left open.
--
-- The stored payload is the canonical bytes of the normalized page, not the
-- provider's raw response. Raw provider bytes follow their own retention
-- policy and appear only as raw_artifact_ref, so that deleting a raw response
-- under a retention rule never destroys the evidence that the read happened.

-- +goose Up

CREATE TABLE external_observation (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    observation_id    uuid           NOT NULL,
    connection_id     semantic_key   NOT NULL,
    connector_id      semantic_key   NOT NULL,
    connector_version semantic_key   NOT NULL,
    source_ref        semantic_key   NOT NULL,
    authority_ref     semantic_key   NOT NULL,
    object_kind       text           NOT NULL,
    schema_version    semantic_key   NOT NULL,
    snapshot_id       semantic_key   NOT NULL,
    page_sequence     bigint         NOT NULL,
    start_cursor      text           NOT NULL,
    next_cursor       text           NOT NULL,
    record_count      integer        NOT NULL,
    complete          boolean        NOT NULL,
    classification    text           NOT NULL,
    freshness         text           NOT NULL,
    -- Three distinct times. retrieved_at is when we received the page,
    -- watermark_at is the source's own high-water mark for it, and recorded_at
    -- is when this row was written. None substitutes for another.
    retrieved_at      timestamptz    NOT NULL,
    watermark_at      timestamptz    NOT NULL,
    recorded_at       timestamptz    NOT NULL DEFAULT now(),
    content_digest    content_digest NOT NULL,
    digest_algorithm  digest_algorithm NOT NULL DEFAULT 'sha256',
    canonical_length  integer        NOT NULL,
    payload           bytea          NOT NULL,
    -- Separately retained raw provider bytes, when a retention policy kept
    -- them. Never the evidence itself.
    raw_artifact_ref  text,
    PRIMARY KEY (tenant_id, observation_id),
    -- One immutable observation per semantic source version: re-reading a page
    -- after a restart lands on the same row rather than creating a second one.
    CONSTRAINT external_observation_page_unique
        UNIQUE (tenant_id, connection_id, object_kind, snapshot_id, page_sequence),
    CONSTRAINT external_observation_object_allowed CHECK (
        object_kind IN ('WORKER', 'POSITION', 'COMPENSATION')
    ),
    -- This table cannot hold a domain fact.
    CONSTRAINT external_observation_classification_allowed CHECK (
        classification = 'EXTERNAL_OBSERVATION'
    ),
    CONSTRAINT external_observation_freshness_allowed CHECK (
        freshness IN ('FRESH', 'STALE', 'PARTIAL', 'UNKNOWN', 'UNAVAILABLE')
    ),
    CONSTRAINT external_observation_page_sequence_positive CHECK (page_sequence >= 1),
    CONSTRAINT external_observation_record_count_non_negative CHECK (record_count >= 0),
    CONSTRAINT external_observation_canonical_length_positive CHECK (canonical_length > 0),
    CONSTRAINT external_observation_payload_matches_length CHECK (
        octet_length(payload) = canonical_length
    ),
    -- Large pages become a governed artifact rather than an inline payload.
    CONSTRAINT external_observation_payload_bounded CHECK (octet_length(payload) <= 1048576),
    CONSTRAINT external_observation_start_cursor_present CHECK (length(start_cursor) > 0)
);

CREATE TRIGGER external_observation_append_only
    BEFORE UPDATE OR DELETE ON external_observation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON external_observation FROM PUBLIC;

COMMENT ON TABLE external_observation IS
    'Immutable EXTERNAL_OBSERVATION evidence: one row per observed page (INTG-009).';

CREATE INDEX external_observation_replay
    ON external_observation (tenant_id, connection_id, object_kind, snapshot_id, page_sequence);

CREATE INDEX external_observation_retrieved
    ON external_observation (tenant_id, connection_id, retrieved_at);

-- The resume point of one traversal. Unlike the observation rows this is
-- mutable state, and fence is what keeps it safe: a commit at or behind the
-- stored fence is rejected, so a worker that was replaced mid-run cannot
-- rewind the traversal it no longer owns.
CREATE TABLE observation_checkpoint (
    tenant_id         tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    connection_id     semantic_key NOT NULL,
    object_kind       text         NOT NULL,
    run_id            semantic_key NOT NULL,
    snapshot_id       semantic_key NOT NULL,
    cursor_token      text         NOT NULL,
    fence             bigint       NOT NULL,
    pages_committed   bigint       NOT NULL,
    records_committed bigint       NOT NULL,
    complete          boolean      NOT NULL DEFAULT false,
    updated_at        timestamptz  NOT NULL,
    recorded_at       timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, connection_id, object_kind),
    CONSTRAINT observation_checkpoint_object_allowed CHECK (
        object_kind IN ('WORKER', 'POSITION', 'COMPENSATION')
    ),
    CONSTRAINT observation_checkpoint_fence_positive CHECK (fence >= 1),
    CONSTRAINT observation_checkpoint_pages_non_negative CHECK (pages_committed >= 0),
    CONSTRAINT observation_checkpoint_records_non_negative CHECK (records_committed >= 0)
);

COMMENT ON TABLE observation_checkpoint IS
    'Fenced resume point of one bounded observation traversal (INTG-008).';

-- +goose Down
DROP TABLE observation_checkpoint;
DROP TRIGGER external_observation_append_only ON external_observation;
DROP TABLE external_observation;
