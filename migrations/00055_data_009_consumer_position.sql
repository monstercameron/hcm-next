-- Owner: data plane. DATA-009 consumer positions and semantic dedupe.
-- A position is mutable serving state; each dedupe row is immutable evidence
-- that one source event was admitted to one consumer group.

-- +goose Up

CREATE TABLE consumer_position (
    tenant_id          tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    consumer_group     semantic_key NOT NULL,
    stream_key         semantic_key NOT NULL,
    source_partition   semantic_key NOT NULL DEFAULT 'default',
    source_sequence    bigint      NOT NULL DEFAULT 0,
    source_digest      content_digest,
    source_schema_ref  semantic_key,
    source_watermark   bigint      NOT NULL DEFAULT 0,
    status             text        NOT NULL DEFAULT 'CURRENT',
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, consumer_group, stream_key, source_partition),
    CONSTRAINT consumer_position_sequence_non_negative CHECK (source_sequence >= 0),
    CONSTRAINT consumer_position_watermark_non_negative CHECK (source_watermark >= 0),
    CONSTRAINT consumer_position_status_allowed CHECK (status IN ('CURRENT','REBUILDING','POISONED','DEGRADED'))
);

CREATE TABLE consumer_dedupe (
    tenant_id          tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    consumer_group     semantic_key NOT NULL,
    event_id           uuid        NOT NULL,
    stream_key         semantic_key NOT NULL,
    source_partition   semantic_key NOT NULL DEFAULT 'default',
    source_sequence    bigint      NOT NULL,
    schema_ref         semantic_key NOT NULL,
    event_digest       content_digest NOT NULL,
    processed_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, consumer_group, event_id),
    CONSTRAINT consumer_dedupe_sequence_positive CHECK (source_sequence >= 1)
);

CREATE INDEX consumer_dedupe_position
    ON consumer_dedupe (tenant_id, consumer_group, stream_key, source_partition, source_sequence);

CREATE TRIGGER consumer_dedupe_append_only
    BEFORE UPDATE OR DELETE ON consumer_dedupe
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE consumer_position ENABLE ROW LEVEL SECURITY;
ALTER TABLE consumer_position FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON consumer_position
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE consumer_dedupe ENABLE ROW LEVEL SECURITY;
ALTER TABLE consumer_dedupe FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON consumer_dedupe
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON consumer_position TO hcmnext_app;
GRANT SELECT, INSERT ON consumer_dedupe TO hcmnext_app;
REVOKE UPDATE, DELETE ON consumer_dedupe FROM PUBLIC;

-- +goose Down

REVOKE ALL ON consumer_dedupe FROM hcmnext_app;
REVOKE ALL ON consumer_position FROM hcmnext_app;
DROP POLICY tenant_isolation ON consumer_dedupe;
DROP POLICY tenant_isolation ON consumer_position;
ALTER TABLE consumer_dedupe NO FORCE ROW LEVEL SECURITY;
ALTER TABLE consumer_dedupe DISABLE ROW LEVEL SECURITY;
ALTER TABLE consumer_position NO FORCE ROW LEVEL SECURITY;
ALTER TABLE consumer_position DISABLE ROW LEVEL SECURITY;
DROP TABLE consumer_dedupe;
DROP TABLE consumer_position;
