-- Owner: data plane. Phase: P1A.
-- DB-008: Person, identity, Worker, Employment and Assignment aggregates.
--
-- Every table here is one bitemporal fact log, not a root/revision pair: a
-- single row already carries both the entity's own stable identity
-- (entity_id, canonical_id) and its current attribute values, dated by two
-- independent axes -- exactly the shared envelope
-- planning/data/models/modeling-conventions.md declares and the shared
-- primitives migrations/00002_tenant_primitives.sql already established for
-- tenant itself:
--
--   effective_from / effective_to   business (valid) time, half-open
--   recorded_at / superseded_at     system (transaction) time, half-open
--
-- A change to an entity is never an in-place UPDATE of its business columns.
-- It is always: (1) supersede whichever live row(s) -- superseded_at IS NULL
-- -- would otherwise still be in effect over the new row's business-time
-- range, by setting their superseded_at (the one and only mutation this
-- schema permits on an existing row), then (2) INSERT the new row. Both
-- steps run in the same transaction from internal/data/aggregates' Put
-- helper. aggregate_forbid_inplace_update, defined once below and reused by
-- migrations/00012 and 00013, is the trigger that makes the narrower
-- "supersede" transition the only UPDATE these tables ever accept: it diffs
-- OLD and NEW as jsonb, ignoring superseded_at, and raises unless (a) OLD had
-- no superseded_at yet, (b) NEW sets exactly one, and (c) nothing else
-- differs. A direct UPDATE of any business column -- from psql, from a typo'd
-- repository method, from anything other than the supersede step -- is
-- refused, which is what TestTodo_DB_008 proves in RED.
--
-- Cross-table references (person_ref, employment_ref, package_ref, ...) are
-- plain uuid columns, not foreign keys: the referenced row is itself one
-- version among many for that entity_id, and which one is "the" row a
-- reference resolves to depends on the reader's own AsOf/KnownAt bounds, so a
-- static FK constraint would either point at the wrong row or refuse to let
-- history exist at all. Referential integrity here is the reader's
-- responsibility (resolve the reference at the same AsOf/KnownAt it is
-- reading everything else at), the same tradeoff migrations/00005_ledger.sql
-- already makes for ledger_event's source_ref/schema_ref.
--
-- Every table is tenant-scoped and carries migration 00008's exact row level
-- security pattern: FORCE ROW LEVEL SECURITY plus one tenant_isolation policy
-- keyed on the fail-closed app.tenant_id session setting, and hcmnext_app is
-- granted SELECT/INSERT/UPDATE (never DELETE -- this data plane has no delete
-- semantics, only append and supersede) on each one.

-- +goose Up

-- Shared "supersede, never update in place" guard. Schema-scoped (created
-- fresh in every pgtest schema alongside the tables that use it), so unlike
-- migrations/00008's hcmnext_app role or a shared extension it needs no
-- cross-test race guard.
-- +goose StatementBegin
CREATE FUNCTION aggregate_forbid_inplace_update() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_rest jsonb := to_jsonb(OLD) - 'superseded_at';
    new_rest jsonb := to_jsonb(NEW) - 'superseded_at';
BEGIN
    IF OLD.superseded_at IS NOT NULL THEN
        RAISE EXCEPTION
            'table % is append-only; row % was already superseded at % and cannot be updated again',
            TG_TABLE_NAME, OLD.row_id, OLD.superseded_at
            USING ERRCODE = '23514';
    END IF;
    IF NEW.superseded_at IS NULL THEN
        RAISE EXCEPTION
            'table % is append-only; the only permitted update is setting superseded_at once, row %',
            TG_TABLE_NAME, OLD.row_id
            USING ERRCODE = '23514';
    END IF;
    IF old_rest IS DISTINCT FROM new_rest THEN
        RAISE EXCEPTION
            'table % is append-only; row % may only have superseded_at set, every other column is immutable -- append a new row instead',
            TG_TABLE_NAME, OLD.row_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION aggregate_forbid_inplace_update() IS
    'DB-008/009/010 bitemporal aggregate guard: the only UPDATE a fact row ever accepts is setting superseded_at from NULL once; every other column is append-only.';

-- ---------------------------------------------------------------------------
-- aggregate_entity: the shared identity spine every DB-008/009/010 table (in
-- this migration and in migrations/00012 and 00013) is foreign-keyed to.
--
-- A fact-log table's own entity_id is deliberately not unique -- many rows
-- share it, one per bitemporal version -- so nothing in person/worker/...
-- itself can serve as a Postgres foreign key target. aggregate_entity holds
-- exactly one row per (tenant_id, entity_id), written once by
-- internal/data/aggregates' Put helper (an ON CONFLICT DO NOTHING upsert)
-- before that entity's first fact row, so every table below can carry a real,
-- tenant-qualified foreign key on entity_id, and every cross-entity reference
-- column (person_ref, employment_ref, budget_ref, ...) can carry one too --
-- closing exactly the gap a plain uuid column would otherwise leave (DATA-001
-- "no foreign key crosses a tenant boundary" only has teeth where a foreign
-- key exists at all).
--
-- kind is informational (which table's Put call registered the entity), not
-- itself enforced by any FK here: a person_ref FK below proves "some
-- registered entity with this id exists in this tenant", not "and it is
-- specifically a person" -- the same limitation ledger_event's schema_ref
-- reference has, traded for a spine simple enough not to need its own
-- migration lane.
CREATE TABLE aggregate_entity (
    tenant_id    tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    entity_id    uuid         NOT NULL,
    kind         semantic_key NOT NULL,
    canonical_id semantic_key NOT NULL,
    created_at   timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, entity_id)
);
ALTER TABLE aggregate_entity ENABLE ROW LEVEL SECURITY;
ALTER TABLE aggregate_entity FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON aggregate_entity
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
-- SELECT and INSERT only: an entity is registered once and never updated or
-- deleted (the fact tables that key off it are themselves append-only).
GRANT SELECT, INSERT ON aggregate_entity TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- Person: party identity. lifecycle_state is the aggregate's own state, not a
-- role -- Candidate/Worker/Contractor/... are roles layered on top of one
-- Person and are out of this thin adapter's scope (planning/data/models/
-- people-workforce.md's own "Candidate, worker, contractor ... are roles or
-- relationships around Person; they are not mutually exclusive Person
-- states").
-- ---------------------------------------------------------------------------
CREATE TABLE person (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    lifecycle_state  text             NOT NULL,
    legal_name       text             NOT NULL,
    preferred_name   text,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT person_lifecycle_state_allowed CHECK (
        lifecycle_state IN ('ACTIVE', 'INACTIVE', 'MERGED', 'SEPARATED')
    ),
    CONSTRAINT person_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT person_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX person_history ON person (tenant_id, entity_id, recorded_at);
CREATE INDEX person_live ON person (tenant_id, entity_id) WHERE superseded_at IS NULL;
CREATE TRIGGER person_forbid_inplace_update BEFORE UPDATE ON person
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE person ENABLE ROW LEVEL SECURITY;
ALTER TABLE person FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON person
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON person TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- IdentityClaim: a typed, effective-dated claim linking a raw identifier
-- (already hashed by the caller -- normalized_value_hash never carries a raw
-- SSN/email) to a person. person_ref is nullable: an unresolved claim may
-- exist before an IdentityResolutionDecision links it (planning/data/models/
-- people-workforce.md IdentityClaim / IdentityResolutionDecision).
-- ---------------------------------------------------------------------------
CREATE TABLE identity_claim (
    row_id                 uuid             NOT NULL,
    tenant_id              tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id              uuid             NOT NULL,
    canonical_id           semantic_key     NOT NULL,
    person_ref             uuid,
    claim_type             semantic_key     NOT NULL,
    namespace              semantic_key     NOT NULL,
    normalized_value_hash  semantic_key     NOT NULL,
    issuer                 text             NOT NULL,
    assurance              text             NOT NULL,
    effective_from         timestamptz      NOT NULL,
    effective_to           timestamptz,
    recorded_at            timestamptz      NOT NULL DEFAULT now(),
    superseded_at          timestamptz,
    digest_algorithm       digest_algorithm NOT NULL,
    digest                 content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, person_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT identity_claim_assurance_allowed CHECK (
        assurance IN ('SELF_ASSERTED', 'VERIFIED', 'AUTHORITATIVE')
    ),
    CONSTRAINT identity_claim_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT identity_claim_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX identity_claim_history ON identity_claim (tenant_id, entity_id, recorded_at);
CREATE INDEX identity_claim_person ON identity_claim (tenant_id, person_ref) WHERE superseded_at IS NULL;
CREATE TRIGGER identity_claim_forbid_inplace_update BEFORE UPDATE ON identity_claim
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE identity_claim ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity_claim FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON identity_claim
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON identity_claim TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- Worker: the employment-side role over a Person.
-- ---------------------------------------------------------------------------
CREATE TABLE worker (
    row_id           uuid             NOT NULL,
    tenant_id        tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id        uuid             NOT NULL,
    canonical_id     semantic_key     NOT NULL,
    person_ref       uuid             NOT NULL,
    worker_number    semantic_key,
    worker_type      text             NOT NULL,
    lifecycle_status text             NOT NULL,
    effective_from   timestamptz      NOT NULL,
    effective_to     timestamptz,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    superseded_at    timestamptz,
    digest_algorithm digest_algorithm NOT NULL,
    digest           content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, person_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT worker_type_allowed CHECK (
        worker_type IN ('EMPLOYEE', 'CONTRACTOR', 'INTERN', 'TEMPORARY')
    ),
    CONSTRAINT worker_lifecycle_status_allowed CHECK (
        lifecycle_status IN ('PENDING', 'ACTIVE', 'ON_LEAVE', 'TERMINATED')
    ),
    CONSTRAINT worker_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT worker_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX worker_history ON worker (tenant_id, entity_id, recorded_at);
CREATE INDEX worker_person ON worker (tenant_id, person_ref) WHERE superseded_at IS NULL;
CREATE TRIGGER worker_forbid_inplace_update BEFORE UPDATE ON worker
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE worker ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON worker TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- Employment: worker_ref + legal_entity_ref plus its own status revision,
-- folded into one row per planning/data/models/people-workforce.md's
-- Employment / EmploymentStatusRevision.
-- ---------------------------------------------------------------------------
CREATE TABLE employment (
    row_id             uuid             NOT NULL,
    tenant_id          tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id          uuid             NOT NULL,
    canonical_id       semantic_key     NOT NULL,
    worker_ref         uuid             NOT NULL,
    legal_entity_ref   uuid             NOT NULL,
    employment_type    text             NOT NULL,
    employment_status  text             NOT NULL,
    hire_date          date,
    effective_from     timestamptz      NOT NULL,
    effective_to       timestamptz,
    recorded_at        timestamptz      NOT NULL DEFAULT now(),
    superseded_at      timestamptz,
    digest_algorithm   digest_algorithm NOT NULL,
    digest             content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, worker_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, legal_entity_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT employment_type_allowed CHECK (
        employment_type IN ('EMPLOYEE', 'CONTRACTOR', 'INTERN', 'TEMPORARY')
    ),
    CONSTRAINT employment_status_allowed CHECK (
        employment_status IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'LEAVE', 'ENDED', 'REINSTATED')
    ),
    CONSTRAINT employment_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT employment_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX employment_history ON employment (tenant_id, entity_id, recorded_at);
CREATE INDEX employment_worker ON employment (tenant_id, worker_ref) WHERE superseded_at IS NULL;
CREATE TRIGGER employment_forbid_inplace_update BEFORE UPDATE ON employment
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE employment ENABLE ROW LEVEL SECURITY;
ALTER TABLE employment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON employment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON employment TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- Assignment: job/position/org placement, folding AssignmentRevision into
-- the same row (see file header). manager_relationship_ref is carried
-- verbatim as an opaque ref; WorkerRelationship itself is not modeled by
-- this thin adapter.
-- ---------------------------------------------------------------------------
CREATE TABLE assignment (
    row_id                   uuid             NOT NULL,
    tenant_id                tenant_ref       NOT NULL REFERENCES tenant (tenant_id),
    entity_id                uuid             NOT NULL,
    canonical_id             semantic_key     NOT NULL,
    employment_ref           uuid             NOT NULL,
    primary_flag             boolean          NOT NULL DEFAULT true,
    job_code                 semantic_key     NOT NULL,
    grade                    semantic_key,
    organization_ref         uuid,
    position_ref             uuid,
    location                 text,
    pay_zone                 semantic_key,
    fte                      numeric(9, 4)    NOT NULL,
    manager_relationship_ref text,
    effective_from           timestamptz      NOT NULL,
    effective_to             timestamptz,
    recorded_at              timestamptz      NOT NULL DEFAULT now(),
    superseded_at            timestamptz,
    digest_algorithm         digest_algorithm NOT NULL,
    digest                   content_digest   NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    FOREIGN KEY (tenant_id, entity_id) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, employment_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, organization_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    FOREIGN KEY (tenant_id, position_ref) REFERENCES aggregate_entity (tenant_id, entity_id),
    CONSTRAINT assignment_fte_positive CHECK (fte > 0),
    CONSTRAINT assignment_effective_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    CONSTRAINT assignment_superseded_after_recorded CHECK (superseded_at IS NULL OR superseded_at > recorded_at)
);
CREATE INDEX assignment_history ON assignment (tenant_id, entity_id, recorded_at);
CREATE INDEX assignment_employment ON assignment (tenant_id, employment_ref) WHERE superseded_at IS NULL;
CREATE TRIGGER assignment_forbid_inplace_update BEFORE UPDATE ON assignment
    FOR EACH ROW EXECUTE FUNCTION aggregate_forbid_inplace_update();
ALTER TABLE assignment ENABLE ROW LEVEL SECURITY;
ALTER TABLE assignment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON assignment
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON assignment TO hcmnext_app;

-- +goose Down
DROP POLICY tenant_isolation ON assignment;
REVOKE ALL ON assignment FROM hcmnext_app;
DROP TABLE assignment;

DROP POLICY tenant_isolation ON employment;
REVOKE ALL ON employment FROM hcmnext_app;
DROP TABLE employment;

DROP POLICY tenant_isolation ON worker;
REVOKE ALL ON worker FROM hcmnext_app;
DROP TABLE worker;

DROP POLICY tenant_isolation ON identity_claim;
REVOKE ALL ON identity_claim FROM hcmnext_app;
DROP TABLE identity_claim;

DROP POLICY tenant_isolation ON person;
REVOKE ALL ON person FROM hcmnext_app;
DROP TABLE person;

-- Safe only because goose applies Down in descending version order: 00012's
-- and 00013's tables (every one of them foreign-keyed to aggregate_entity)
-- are already gone by the time this runs.
DROP POLICY tenant_isolation ON aggregate_entity;
REVOKE ALL ON aggregate_entity FROM hcmnext_app;
DROP TABLE aggregate_entity;

DROP FUNCTION aggregate_forbid_inplace_update();
