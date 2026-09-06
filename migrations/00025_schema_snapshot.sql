-- Owner: connectivity. Phase: P1A.
-- INTG-004: ingest and quarantine externally discovered schema snapshots.
--
-- planning/specs/integration-platform.md's Schema Discovery pipeline is
-- "discover/import -> normalize schema snapshot -> registry", and its own
-- coverage table marks schema snapshots as a MINIMAL CONTRACT: this migration
-- gives that normalize step somewhere durable to land the snapshot *before*
-- it is trusted, not a full schema-diff/registry engine (that is INTG-005's
-- job). The RED clause this answers is "untrusted discovered schema
-- auto-publishes or missing descriptor/version/digest/source/effective
-- context is accepted" -- so every identity column here is NOT NULL and the
-- row is born QUARANTINED; nothing in this schema can insert a row any other
-- way.
--
-- Two tables:
--
--   integration_schema_snapshot          -- one row per (tenant, snapshot_id).
--     snapshot_id is deterministic (internal/connectivity/schemasnapshot's
--     Identity.SnapshotID, derived from tenant + provider + canonical
--     digest), so re-ingesting byte-identical content for the same provider
--     lands on the existing row instead of creating a second one -- the same
--     content-addressed idempotency internal/data/artifacts.Put already
--     gives the raw bytes themselves (canonical_digest and artifact_ref are
--     in fact the same sha256 content id: the artifact store already proved
--     what the bytes hash to, and this row about the schema simply names it
--     twice under two different intentions -- one is "this is the content
--     identity", the other is "this is where the bytes physically live").
--     The row starts life with state = QUARANTINED and every other column
--     populated and immutable; state (with its paired state_reason and
--     decided_at) is the only thing that may ever change, and only once,
--     moving to ADMITTED or REJECTED.
--
--   integration_schema_snapshot_evidence -- append-only, exactly one row per
--     snapshot (a unique index on snapshot_id enforces that a decision is
--     recorded once), holding the full validator-by-validator verdict as
--     jsonb. "an evidence record either way" from the todo's own language:
--     admission and rejection both produce one of these, never neither.
--
-- Why a controlled UPDATE trigger, not the append-only forbid_mutation used
-- everywhere else in this tree
-- -----------------------------------------------------------------------
-- Every other authoritative table either never updates at all
-- (forbid_mutation, migrations/00001) or supersedes-and-appends
-- (aggregate_forbid_inplace_update, migrations/00011): a bitemporal fact
-- log where "changing" a row always means writing a new one and marking the
-- old one superseded. Neither fits here. A schema snapshot is not a
-- reissuable fact with a history of versions -- it is a single content-
-- addressed capture whose *disposition* (still under review, or decided)
-- is a one-way, one-time state machine on that same row, exactly like
-- internal/connectivity's own ConnectorConnection lifecycle. Modeling the
-- decision as a second append-only row instead (an "admission event" table
-- with no matching UPDATE) was the other option considered; it was rejected
-- because it would let a snapshot accumulate more than one decision event
-- with nothing at the row level stopping it, pushing "there is exactly one
-- verdict" onto every future reader instead of the schema. So:
-- schema_snapshot_forbid_identity_mutation, defined once below, is
-- migration 00011's exact technique (diff OLD and NEW as jsonb, ignoring
-- the columns the transition is allowed to touch) aimed at a state-field
-- transition instead of a supersede-and-append: it permits an UPDATE only
-- when OLD.state = 'QUARANTINED', NEW.state moves to a terminal state, and
-- nothing besides state/state_reason/decided_at differs -- so the raw-bytes
-- identity (canonical_digest, artifact_ref, byte_size) and every other
-- column are exactly as immutable as forbid_mutation would make them,
-- without forbidding the one transition this table exists to allow.

-- +goose Up

CREATE TABLE integration_schema_snapshot (
    tenant_id         tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    snapshot_id       uuid             NOT NULL,
    -- Provider identity: which connector, which tenant connection, which
    -- external system instance the schema was captured from.
    connector_id      semantic_key     NOT NULL,
    connection_id     semantic_key     NOT NULL,
    source_ref        semantic_key     NOT NULL,
    -- Business time of capture, distinct from created_at (system time of the
    -- row write) exactly as external_observation separates retrieved_at from
    -- recorded_at (migrations/00007).
    captured_at       timestamptz      NOT NULL,
    declared_format   text             NOT NULL,
    digest_algorithm  digest_algorithm NOT NULL DEFAULT 'sha256',
    -- canonical_digest is the sha256 of the exact raw schema bytes; it is
    -- also the artifact store's own content id for those bytes, which
    -- artifact_ref repeats verbatim as the pointer to where they are stored.
    canonical_digest  content_digest   NOT NULL,
    artifact_ref      content_digest   NOT NULL,
    byte_size         bigint           NOT NULL,
    -- Supersession: this snapshot supersedes an earlier one for the *same*
    -- provider. Enforced same-provider by the trigger below, immutable by
    -- schema_snapshot_forbid_identity_mutation like every other identity
    -- column here.
    supersedes_ref    uuid,
    state             text             NOT NULL DEFAULT 'QUARANTINED',
    state_reason      text,
    decided_at        timestamptz,
    created_at        timestamptz      NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, snapshot_id),
    FOREIGN KEY (tenant_id, supersedes_ref) REFERENCES integration_schema_snapshot (tenant_id, snapshot_id),
    CONSTRAINT integration_schema_snapshot_format_allowed CHECK (
        declared_format IN ('JSON_SCHEMA', 'OPENAPI', 'XSD', 'WSDL', 'CSV_HEADER', 'GRAPHQL_SDL', 'PROTOBUF')
    ),
    CONSTRAINT integration_schema_snapshot_state_allowed CHECK (
        state IN ('QUARANTINED', 'ADMITTED', 'REJECTED')
    ),
    -- Matches internal/data/artifacts' own absolute backstop
    -- (artifact_byte_size_bounded, migrations/00010): a snapshot's declared
    -- size can never claim more than the artifact store could ever hold.
    CONSTRAINT integration_schema_snapshot_byte_size_bounded CHECK (byte_size > 0 AND byte_size <= 67108864),
    -- A QUARANTINED row carries no decision; a decided row always does. This
    -- is what makes "evidence record either way" checkable at the schema
    -- level rather than trusted to application code.
    CONSTRAINT integration_schema_snapshot_decision_consistent CHECK (
        (state = 'QUARANTINED' AND state_reason IS NULL AND decided_at IS NULL)
        OR (state <> 'QUARANTINED' AND state_reason IS NOT NULL AND length(state_reason) > 0 AND decided_at IS NOT NULL)
    ),
    CONSTRAINT integration_schema_snapshot_no_self_supersede CHECK (supersedes_ref IS NULL OR supersedes_ref <> snapshot_id)
);

CREATE INDEX integration_schema_snapshot_provider ON integration_schema_snapshot
    (tenant_id, connector_id, connection_id, source_ref, captured_at DESC);
CREATE INDEX integration_schema_snapshot_state ON integration_schema_snapshot (tenant_id, state);

-- Enforces "supersession links between snapshots of the same provider": a
-- supersedes_ref must name a snapshot that already exists for this tenant
-- under the identical (connector_id, connection_id, source_ref) triple.
-- +goose StatementBegin
CREATE FUNCTION schema_snapshot_supersedes_same_provider() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    prior RECORD;
BEGIN
    IF NEW.supersedes_ref IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT connector_id, connection_id, source_ref INTO prior
        FROM integration_schema_snapshot
        WHERE tenant_id = NEW.tenant_id AND snapshot_id = NEW.supersedes_ref;
    IF NOT FOUND THEN
        RAISE EXCEPTION
            'integration_schema_snapshot %: supersedes_ref % names no existing snapshot for this tenant',
            NEW.snapshot_id, NEW.supersedes_ref
            USING ERRCODE = '23514';
    END IF;
    IF prior.connector_id <> NEW.connector_id
        OR prior.connection_id <> NEW.connection_id
        OR prior.source_ref <> NEW.source_ref THEN
        RAISE EXCEPTION
            'integration_schema_snapshot %: cannot supersede % of a different provider (%/%/% vs %/%/%)',
            NEW.snapshot_id, NEW.supersedes_ref,
            NEW.connector_id, NEW.connection_id, NEW.source_ref,
            prior.connector_id, prior.connection_id, prior.source_ref
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER integration_schema_snapshot_supersedes_check BEFORE INSERT ON integration_schema_snapshot
    FOR EACH ROW EXECUTE FUNCTION schema_snapshot_supersedes_same_provider();

-- The controlled-update guard described in the file header: the only UPDATE
-- this table ever accepts is QUARANTINED -> {ADMITTED, REJECTED} together
-- with state_reason and decided_at; every other column, including
-- canonical_digest and artifact_ref, is exactly as immutable as
-- forbid_mutation would make it.
-- +goose StatementBegin
CREATE FUNCTION schema_snapshot_forbid_identity_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_rest jsonb := to_jsonb(OLD) - 'state' - 'state_reason' - 'decided_at';
    new_rest jsonb := to_jsonb(NEW) - 'state' - 'state_reason' - 'decided_at';
BEGIN
    IF OLD.state <> 'QUARANTINED' THEN
        RAISE EXCEPTION
            'integration_schema_snapshot % is already decided (%) and cannot be updated again',
            OLD.snapshot_id, OLD.state
            USING ERRCODE = '23514';
    END IF;
    IF NEW.state = 'QUARANTINED' THEN
        RAISE EXCEPTION
            'integration_schema_snapshot % update must move state out of QUARANTINED',
            OLD.snapshot_id
            USING ERRCODE = '23514';
    END IF;
    IF old_rest IS DISTINCT FROM new_rest THEN
        RAISE EXCEPTION
            'integration_schema_snapshot % is otherwise immutable; only state, state_reason and decided_at may change',
            OLD.snapshot_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION schema_snapshot_forbid_identity_mutation() IS
    'INTG-004 controlled transition: the only UPDATE integration_schema_snapshot accepts is QUARANTINED -> ADMITTED/REJECTED with state_reason and decided_at; every other column is immutable.';

CREATE TRIGGER integration_schema_snapshot_forbid_identity_mutation BEFORE UPDATE ON integration_schema_snapshot
    FOR EACH ROW EXECUTE FUNCTION schema_snapshot_forbid_identity_mutation();

REVOKE DELETE ON integration_schema_snapshot FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON integration_schema_snapshot TO hcmnext_app;

ALTER TABLE integration_schema_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_schema_snapshot FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_schema_snapshot
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- integration_schema_snapshot_evidence: append-only, exactly one row per
-- snapshot (integration_schema_snapshot_evidence_one_per_snapshot below).
CREATE TABLE integration_schema_snapshot_evidence (
    tenant_id         tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    evidence_id       uuid        NOT NULL,
    snapshot_id       uuid        NOT NULL,
    verdict           text        NOT NULL,
    validator_results jsonb       NOT NULL,
    decided_at        timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, evidence_id),
    FOREIGN KEY (tenant_id, snapshot_id) REFERENCES integration_schema_snapshot (tenant_id, snapshot_id),
    CONSTRAINT integration_schema_snapshot_evidence_verdict_allowed CHECK (
        verdict IN ('ADMITTED', 'REJECTED')
    ),
    CONSTRAINT integration_schema_snapshot_evidence_results_present CHECK (
        jsonb_typeof(validator_results) = 'array' AND jsonb_array_length(validator_results) > 0
    )
);

CREATE UNIQUE INDEX integration_schema_snapshot_evidence_one_per_snapshot
    ON integration_schema_snapshot_evidence (tenant_id, snapshot_id);

CREATE TRIGGER integration_schema_snapshot_evidence_append_only BEFORE UPDATE OR DELETE
    ON integration_schema_snapshot_evidence FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON integration_schema_snapshot_evidence FROM PUBLIC;
GRANT SELECT, INSERT ON integration_schema_snapshot_evidence TO hcmnext_app;

ALTER TABLE integration_schema_snapshot_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_schema_snapshot_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_schema_snapshot_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- +goose Down

DROP POLICY tenant_isolation ON integration_schema_snapshot_evidence;
REVOKE ALL ON integration_schema_snapshot_evidence FROM hcmnext_app;
DROP TABLE integration_schema_snapshot_evidence;

DROP POLICY tenant_isolation ON integration_schema_snapshot;
REVOKE ALL ON integration_schema_snapshot FROM hcmnext_app;
DROP TABLE integration_schema_snapshot;

DROP FUNCTION schema_snapshot_forbid_identity_mutation();
DROP FUNCTION schema_snapshot_supersedes_same_provider();
