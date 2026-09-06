-- Owner: data plane (journey vertical slice). Phase: P1B.
-- Durable workforce for the Promotion journey: the employees a user creates
-- through the journey surface, so a demo population is a fact in the database
-- rather than four rows compiled into the binary.
--
-- Until this migration the only workers the cell could see were the four in
-- internal/domains/fixtures/testdata/workers.json, read through
-- fixtures.MemoryWorkerFacts. That corpus is deliberately immutable -- it is
-- the shared regression population, and a test that could add to it would stop
-- being a regression -- so "create an employee" had nowhere to land.
-- journey_worker is that place: one row per created worker, read back through
-- internal/data/workforce's own people.WorkerFacts implementation, layered
-- behind the corpus so the capability gateway, the workspace and the journey
-- all see created workers through the single cell-level governed worker read
-- and never through a second path.
--
-- An employee here is a FACT, not an editable row. The table is append-only,
-- enforced the same way migration 00017's work_item_transition and migration
-- 00022's work_item_decision are: the forbid_mutation trigger from
-- migrations/00001_platform_control.sql on UPDATE and DELETE, plus a grant of
-- SELECT/INSERT only. That is why every row carries its own bitemporal
-- coordinates (effective_from, known_at, recorded_at) and its own revision
-- stream position (revision_stream, revision_sequence): the shape a correction
-- would use is already here.
--
-- Correcting a created worker is OUT OF SCOPE for this migration. When it
-- lands it will be a NEW revision row on the same revision_stream at
-- revision_sequence + 1, with its own known_at/recorded_at, and the read side
-- will select the highest sequence visible at the query's known-at coordinate.
-- Doing that properly needs the primary key to admit more than one row per
-- worker and needs the read to become a bitemporal as-of select; both are
-- schema changes, so this migration deliberately does not pretend to support
-- them: PRIMARY KEY (tenant_id, worker_id) admits exactly one row per worker,
-- and a second insert for the same worker collides rather than shadowing the
-- first.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from
-- migrations/00022_work_item_decision.sql's own copy of the
-- migrations/00008_tenant_isolation.sql pattern, keyed on
-- internal/data/tenancy.WithTenant.

-- +goose Up

CREATE TABLE IF NOT EXISTS journey_worker (
    tenant_id                tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    worker_id                uuid         NOT NULL,

    -- worker_key is the stable, human-typable reference the journey surface
    -- lists and accepts ("lena-park-2"). It is what the promote_worker request
    -- payload's worker_ref carries, so a created worker is named on the wire
    -- exactly the way a corpus worker is (fixtures.WorkerRef's own key). It is
    -- unique per tenant, which is what makes "create the same employee twice"
    -- a collision rather than two indistinguishable people.
    worker_key               semantic_key NOT NULL,

    legal_name               text         NOT NULL,
    preferred_name           text         NOT NULL,
    worker_number            text         NOT NULL,

    -- worker_type and lifecycle_status carry the vocabulary tokens
    -- internal/domains/people's FieldWorkerType and FieldLifecycleStatus are
    -- projected from. The column defaults are the neutral upper-case tokens;
    -- internal/intent/app's CreateWorker writes the corpus spelling
    -- (internal/domains/promotion's own lifecycleActive is the lower-case
    -- literal "active"), because a created worker that could not pass the
    -- promotion preflight would be a worker the journey cannot run.
    worker_type              text         NOT NULL DEFAULT 'EMPLOYEE',
    lifecycle_status         text         NOT NULL DEFAULT 'ACTIVE',

    employment_id            text         NOT NULL,
    assignment_id            text         NOT NULL,

    job_code                 text         NOT NULL,
    grade                    text         NOT NULL,
    org_unit                 text         NOT NULL,
    position_id              text         NOT NULL,
    location                 text         NOT NULL,
    pay_zone                 text         NOT NULL,
    fte                      numeric(6,4) NOT NULL,
    manager_relationship_ref text         NOT NULL,

    hire_date                date         NOT NULL,
    effective_from           date         NOT NULL,

    -- The declared compensation baseline the promotion simulation reads for
    -- this worker. It lives here rather than being taken from the ported
    -- legacy corpus (which describes exactly one worker, omar-reyes) because a
    -- created worker's current pay is its own fact, not that worker's.
    base_pay                 numeric(14,2) NOT NULL,
    currency                 char(3)       NOT NULL,
    pay_basis                text          NOT NULL,
    bonus_target             numeric(6,4)  NOT NULL,

    -- The revision stream position and the bitemporal coordinates the
    -- projected people.Fact carries, so a created worker's fact is complete
    -- evidence in exactly the way a corpus worker's is.
    revision_stream          semantic_key NOT NULL,
    revision_sequence        bigint       NOT NULL,
    known_at                 timestamptz  NOT NULL,
    recorded_at              timestamptz  NOT NULL DEFAULT now(),

    created_by               text         NOT NULL,
    -- source names how this worker entered the cell. Only 'CREATED' exists
    -- today; an imported or observed population would add its own token
    -- alongside it rather than overloading this one.
    source                   text         NOT NULL DEFAULT 'CREATED',

    PRIMARY KEY (tenant_id, worker_id),
    CONSTRAINT journey_worker_key_unique UNIQUE (tenant_id, worker_key),

    CONSTRAINT journey_worker_source_allowed CHECK (source IN ('CREATED')),
    CONSTRAINT journey_worker_revision_positive CHECK (revision_sequence >= 1),
    CONSTRAINT journey_worker_base_pay_positive CHECK (base_pay > 0),
    CONSTRAINT journey_worker_fte_positive CHECK (fte > 0),
    CONSTRAINT journey_worker_currency_shape CHECK (currency ~ '^[A-Z]{3}$'),
    -- Knowledge may not precede its own recording, the same invariant
    -- internal/kernel/values.ValidateKnowledgeOrder enforces in Go. Declaring
    -- it here too means a row that could never produce a valid people.Fact
    -- cannot be inserted at all.
    CONSTRAINT journey_worker_knowledge_order CHECK (known_at <= recorded_at)
);

-- "Every worker this tenant created, newest first", which is the one read the
-- journey's own worker list performs.
CREATE INDEX IF NOT EXISTS journey_worker_recorded
    ON journey_worker (tenant_id, recorded_at DESC);

CREATE OR REPLACE TRIGGER journey_worker_append_only
    BEFORE UPDATE OR DELETE ON journey_worker
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON journey_worker FROM PUBLIC;

-- Tenant isolation (DB-017): fail closed on a missing/blank app.tenant_id
-- session setting, keyed on internal/data/tenancy.WithTenant.
ALTER TABLE journey_worker ENABLE ROW LEVEL SECURITY;
ALTER TABLE journey_worker FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON journey_worker
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- journey_worker is append-only evidence, so SELECT/INSERT only, exactly like
-- work_item_transition and work_item_decision.
GRANT SELECT, INSERT ON journey_worker TO hcmnext_app;

-- +goose Down
REVOKE ALL ON journey_worker FROM hcmnext_app;
DROP POLICY tenant_isolation ON journey_worker;
ALTER TABLE journey_worker NO FORCE ROW LEVEL SECURITY;
ALTER TABLE journey_worker DISABLE ROW LEVEL SECURITY;
DROP TABLE journey_worker;
