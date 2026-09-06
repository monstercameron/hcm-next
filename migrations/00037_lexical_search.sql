-- Owner: data plane. Phase: P2. RETRIEVAL-001: authorized lexical search
-- before any specialized search infrastructure.
--
-- search_projection is the one PostgreSQL full-text index the cell has: a
-- tenant-scoped, per-subject row carrying a tsvector built only from fields
-- internal/data/search has declared classification-cleared for lexical
-- search (a small, explicit allowlist of directory-shaped facts -- worker
-- number, legal/preferred name, job code, org unit, location, position id --
-- never compensation, employment status, FTE or a manager relationship
-- reference). The Go store never writes a value for a field outside that
-- allowlist into search_text, so a value this table was never supposed to
-- carry cannot leak through the tsvector even if the caller building the
-- projection input handed every field it read to Project -- filtering is
-- this schema's own guarantee, not something every caller has to remember to
-- do first (see internal/data/search/fields.go).
--
-- The table is REBUILDABLE, not PERMANENT: it is a pure projection of
-- whatever the owning domain's own fact tables (migrations/00011's person/
-- worker and migrations/00023's journey_worker, for subject_kind='worker')
-- currently assert, keyed by (tenant_id, subject_kind, subject_ref) so a
-- rebuild is an idempotent upsert rather than a second, drifting copy of the
-- facts. search_text is plain text; search_vector is a STORED generated
-- column over it, so the tsvector a query matches against can never disagree
-- with the text that produced it and never has to be maintained by hand on
-- every write. 'simple' is used as the text-search configuration rather than
-- an English/stemming one so that ranking is deterministic and independent
-- of a locale's stemming dictionary -- the same "deterministic ranking"
-- requirement the RETRIEVAL-001 GREEN clause names.
--
-- search_projection_event is the append-only evidence log of every (re)pro-
-- jection: one row per Project call, naming the source revision the facts
-- were read at. It is what proves a projection is a *pure function* of the
-- source facts -- replaying the same revision reprojects byte-identical
-- search_text, and the event log is what a rebuild proof or an audit reads
-- to show that -- and it never records a value, only the fact that a
-- revision was projected. It follows migrations/00023's own append-only
-- pattern verbatim: the forbid_mutation trigger (declared once in
-- migrations/00001_platform_control.sql) on UPDATE and DELETE, and a
-- SELECT/INSERT-only grant.
--
-- Row level security, the fail-closed app.tenant_id predicate and the
-- tenant_isolation policy name are copied verbatim from
-- migrations/00023_journey_workforce.sql's own copy of migration 00008's
-- pattern.
--
-- Neither table carries a foreign key into the domain tables it is projected
-- from. migrations/00011's own header explains why a cross-aggregate
-- reference here is a plain column, not a static FK: which physical source
-- table a subject_ref resolves against depends on subject_kind, and a
-- projection covering more than one entity kind cannot point one FK column
-- at more than one parent table anyway. Referential correctness is the
-- projector's job (internal/data/search.Project always re-derives subject_ref
-- from the same EntityRef the source facts were read for), the same
-- trade-off ledger_event's own source_ref already makes.

-- +goose Up

CREATE TABLE search_projection (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),

    -- subject_kind + subject_ref name the projected subject as the canonical
    -- values.EntityRef the source facts were read for
    -- (values.EntityRef.String(), "eref:v1:<tenant>:<kind>:<id>"), so a query
    -- result can be handed back to the caller's own disclosure rules without
    -- this table ever having stored a field value those rules had not yet
    -- cleared.
    subject_kind    semantic_key NOT NULL,
    subject_ref     semantic_key NOT NULL,

    -- search_text is the concatenation of exactly the classification-cleared
    -- field values Project found present, in a fixed field order, joined by a
    -- single space. It is never every field the source facts carried.
    search_text     text         NOT NULL DEFAULT '',
    search_vector   tsvector GENERATED ALWAYS AS (to_tsvector('simple', search_text)) STORED,

    -- source_revision is the projected values.RevisionToken's own canonical
    -- text form ("rev:v1:..."), so a caller holding a fresher revision from
    -- the authoritative read can tell this projection is stale without this
    -- table needing its own freshness policy.
    source_revision semantic_key NOT NULL,
    projected_at    timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, subject_kind, subject_ref),
    CONSTRAINT search_projection_subject_kind_allowed CHECK (subject_kind IN ('worker'))
);

-- The one GIN index a lexical search query plans against.
CREATE INDEX search_projection_vector_gin ON search_projection USING GIN (search_vector);

ALTER TABLE search_projection ENABLE ROW LEVEL SECURITY;
ALTER TABLE search_projection FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON search_projection
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- REBUILDABLE serving state, not append-only evidence: a rebuild reprojects
-- the same row in place (UPDATE), which is exactly what Store.Project's
-- upsert does.
GRANT SELECT, INSERT, UPDATE ON search_projection TO hcmnext_app;

-- ---------------------------------------------------------------------------
-- search_projection_event: append-only evidence that a (subject, revision)
-- pair was projected. One row per Project call; never updated or deleted.
-- ---------------------------------------------------------------------------
CREATE TABLE search_projection_event (
    event_id        uuid         NOT NULL,
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    subject_kind    semantic_key NOT NULL,
    subject_ref     semantic_key NOT NULL,
    source_revision semantic_key NOT NULL,
    -- event_type is closed to 'PROJECTED' today; a future correction or
    -- tombstone path would add its own token rather than overload this one,
    -- the same convention migrations/00023's journey_worker.source declares.
    event_type      text         NOT NULL DEFAULT 'PROJECTED',
    recorded_at     timestamptz  NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, event_id),
    CONSTRAINT search_projection_event_type_allowed CHECK (event_type IN ('PROJECTED'))
);

CREATE INDEX search_projection_event_subject
    ON search_projection_event (tenant_id, subject_kind, subject_ref, recorded_at);

CREATE OR REPLACE TRIGGER search_projection_event_append_only
    BEFORE UPDATE OR DELETE ON search_projection_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON search_projection_event FROM PUBLIC;

ALTER TABLE search_projection_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE search_projection_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON search_projection_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON search_projection_event TO hcmnext_app;

-- +goose Down
REVOKE ALL ON search_projection_event FROM hcmnext_app;
DROP POLICY tenant_isolation ON search_projection_event;
ALTER TABLE search_projection_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE search_projection_event DISABLE ROW LEVEL SECURITY;
DROP TABLE search_projection_event;

REVOKE ALL ON search_projection FROM hcmnext_app;
DROP POLICY tenant_isolation ON search_projection;
ALTER TABLE search_projection NO FORCE ROW LEVEL SECURITY;
ALTER TABLE search_projection DISABLE ROW LEVEL SECURITY;
DROP TABLE search_projection;
