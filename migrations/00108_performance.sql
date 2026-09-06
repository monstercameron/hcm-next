-- Owner: performance persistence lane. Phase: Gate B.
-- PERSIST-PERFORMANCE-001: durable performance-cycle, rating, calibration,
-- review and outcome-link state.
--
-- The domain keeps the rich kernel values immutable in memory. This migration
-- stores the durable identity and governed references named by the todo; it
-- deliberately does not duplicate the referenced population, graph, review
-- narrative, or rating-scale documents. A new cycle/review revision is a new
-- row. A rating case is the one current-state row because finalization is a
-- guarded state transition; its event history remains append-only.
--
-- Storage disposition (STORE-001):
--   performance_cycle, performance_rating_case,
--   performance_final_rating, performance_calibration_session,
--   performance_review, performance_outcome_link: AGGREGATE, PERMANENT,
--   tenant_id, rebuild_source none.
--   performance_rating_event: LEDGER, PERMANENT, append-only, tenant_id,
--   rebuild_source none.

-- +goose Up

CREATE TABLE IF NOT EXISTS performance_cycle (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid         NOT NULL,
    cycle_id              text         NOT NULL,
    revision              bigint       NOT NULL,
    state                 text         NOT NULL,
    supersedes_revision   bigint,
    canonical_digest      content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_cycle_revision_unique
        UNIQUE (tenant_id, cycle_id, revision),
    CONSTRAINT performance_cycle_revision_positive CHECK (revision >= 1),
    CONSTRAINT performance_cycle_state_allowed CHECK (
        state IN ('PLANNED', 'OPEN', 'CALIBRATING', 'CLOSED', 'LOCKED')
    ),
    CONSTRAINT performance_cycle_supersedes_prior CHECK (
        supersedes_revision IS NULL OR supersedes_revision < revision
    )
);

CREATE OR REPLACE TRIGGER performance_cycle_append_only
    BEFORE UPDATE OR DELETE ON performance_cycle
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_cycle FROM PUBLIC;

CREATE TABLE IF NOT EXISTS performance_rating_case (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    case_id            text           NOT NULL,
    participant_id     text           NOT NULL,
    finalized          boolean        NOT NULL DEFAULT false,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_rating_case_unique UNIQUE (tenant_id, case_id)
);

-- A case can only be finalized once. Its identity and digest are immutable;
-- the application uses this single allowed transition as the finalization CAS.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION performance_rating_case_finalize_only() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.finalized THEN
        RAISE EXCEPTION 'performance rating case % is already finalized', OLD.case_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.case_id IS DISTINCT FROM OLD.case_id
        OR NEW.participant_id IS DISTINCT FROM OLD.participant_id
        OR NEW.finalized IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'performance rating case % may only move false to true', OLD.case_id
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE OR REPLACE TRIGGER performance_rating_case_finalize
    BEFORE UPDATE ON performance_rating_case
    FOR EACH ROW EXECUTE FUNCTION performance_rating_case_finalize_only();

REVOKE DELETE ON performance_rating_case FROM PUBLIC;

CREATE TABLE IF NOT EXISTS performance_rating_event (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    case_id            text           NOT NULL,
    kind               text           NOT NULL,
    actor_id           text,
    at                 timestamptz    NOT NULL,
    contest_digest     content_digest,
    correction_digest  content_digest,
    digest             content_digest NOT NULL,
    event_sequence     bigint         NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_rating_event_sequence_unique
        UNIQUE (tenant_id, case_id, event_sequence),
    CONSTRAINT performance_rating_event_case_fk
        FOREIGN KEY (tenant_id, case_id)
        REFERENCES performance_rating_case (tenant_id, case_id),
    CONSTRAINT performance_rating_event_kind_allowed CHECK (
        kind IN ('CONTEST_RAISED', 'CORRECTION_DECIDED', 'FINALIZED')
    ),
    CONSTRAINT performance_rating_event_sequence_positive CHECK (
        event_sequence >= 1
    )
);

CREATE OR REPLACE TRIGGER performance_rating_event_append_only
    BEFORE UPDATE OR DELETE ON performance_rating_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_rating_event FROM PUBLIC;

CREATE TABLE IF NOT EXISTS performance_final_rating (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    rating_ref         uuid           NOT NULL,
    participant_id     text           NOT NULL,
    cycle_id           text           NOT NULL,
    final_rating       numeric(5,2)   NOT NULL,
    finalized_at       timestamptz    NOT NULL,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_final_rating_unique
        UNIQUE (tenant_id, participant_id, cycle_id)
);

CREATE OR REPLACE TRIGGER performance_final_rating_append_only
    BEFORE UPDATE OR DELETE ON performance_final_rating
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_final_rating FROM PUBLIC;

CREATE TABLE IF NOT EXISTS performance_calibration_session (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    session_id         text           NOT NULL,
    cycle_id           text           NOT NULL,
    cycle_revision     bigint         NOT NULL,
    graph              jsonb,
    adjustments        jsonb,
    canonical_digest   content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_calibration_session_unique
        UNIQUE (tenant_id, session_id),
    CONSTRAINT performance_calibration_session_revision_positive CHECK (
        cycle_revision >= 1
    )
);

CREATE OR REPLACE TRIGGER performance_calibration_session_append_only
    BEFORE UPDATE OR DELETE ON performance_calibration_session
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_calibration_session FROM PUBLIC;

CREATE TABLE IF NOT EXISTS performance_review (
    tenant_id                    tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id                       uuid           NOT NULL,
    review_id                    text           NOT NULL,
    reviewer_id                  text           NOT NULL,
    participant_id               text           NOT NULL,
    cycle_id                     text           NOT NULL,
    cycle_revision               bigint         NOT NULL,
    review_revision              bigint         NOT NULL,
    supersedes_review_revision   bigint,
    submitted_at                 timestamptz,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_review_revision_unique
        UNIQUE (tenant_id, review_id, review_revision),
    CONSTRAINT performance_review_cycle_revision_positive CHECK (cycle_revision >= 1),
    CONSTRAINT performance_review_revision_positive CHECK (review_revision >= 1),
    CONSTRAINT performance_review_supersedes_prior CHECK (
        supersedes_review_revision IS NULL
        OR supersedes_review_revision < review_revision
    )
);

CREATE OR REPLACE TRIGGER performance_review_append_only
    BEFORE UPDATE OR DELETE ON performance_review
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_review FROM PUBLIC;

CREATE TABLE IF NOT EXISTS performance_outcome_link (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid           NOT NULL,
    rating_ref         uuid           NOT NULL,
    rating_digest      content_digest,
    rating_revision    bigint,
    action             text           NOT NULL,
    effective_at       timestamptz,
    digest             content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT performance_outcome_link_action_allowed CHECK (
        action IN ('LINK', 'UNLINK')
    ),
    CONSTRAINT performance_outcome_link_revision_positive CHECK (
        rating_revision IS NULL OR rating_revision >= 1
    )
);

CREATE OR REPLACE TRIGGER performance_outcome_link_append_only
    BEFORE UPDATE OR DELETE ON performance_outcome_link
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_outcome_link FROM PUBLIC;

-- DB-017: all seven tables fail closed without a transaction-local tenant.
ALTER TABLE performance_cycle ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_cycle FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_cycle
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE performance_rating_case ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_rating_case FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_rating_case
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE performance_rating_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_rating_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_rating_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE performance_final_rating ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_final_rating FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_final_rating
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE performance_calibration_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_calibration_session FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_calibration_session
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE performance_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_review
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE performance_outcome_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_outcome_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_outcome_link
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON
    performance_cycle,
    performance_rating_event,
    performance_final_rating,
    performance_calibration_session,
    performance_review,
    performance_outcome_link
TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON performance_rating_case TO hcmnext_app;

-- +goose Down

REVOKE ALL ON performance_rating_case FROM hcmnext_app;
REVOKE ALL ON performance_outcome_link FROM hcmnext_app;
REVOKE ALL ON performance_review FROM hcmnext_app;
REVOKE ALL ON performance_calibration_session FROM hcmnext_app;
REVOKE ALL ON performance_final_rating FROM hcmnext_app;
REVOKE ALL ON performance_rating_event FROM hcmnext_app;
REVOKE ALL ON performance_cycle FROM hcmnext_app;

DROP POLICY tenant_isolation ON performance_outcome_link;
ALTER TABLE performance_outcome_link NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_outcome_link DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON performance_review;
ALTER TABLE performance_review NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_review DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON performance_calibration_session;
ALTER TABLE performance_calibration_session NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_calibration_session DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON performance_final_rating;
ALTER TABLE performance_final_rating NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_final_rating DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON performance_rating_event;
ALTER TABLE performance_rating_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_rating_event DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON performance_rating_case;
ALTER TABLE performance_rating_case NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_rating_case DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON performance_cycle;
ALTER TABLE performance_cycle NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_cycle DISABLE ROW LEVEL SECURITY;

DROP TRIGGER performance_outcome_link_append_only ON performance_outcome_link;
DROP TABLE performance_outcome_link;
DROP TRIGGER performance_review_append_only ON performance_review;
DROP TABLE performance_review;
DROP TRIGGER performance_calibration_session_append_only ON performance_calibration_session;
DROP TABLE performance_calibration_session;
DROP TRIGGER performance_final_rating_append_only ON performance_final_rating;
DROP TABLE performance_final_rating;
DROP TRIGGER performance_rating_event_append_only ON performance_rating_event;
DROP TABLE performance_rating_event;
DROP TRIGGER performance_rating_case_finalize ON performance_rating_case;
DROP TABLE performance_rating_case;
DROP FUNCTION performance_rating_case_finalize_only();
DROP TRIGGER performance_cycle_append_only ON performance_cycle;
DROP TABLE performance_cycle;
