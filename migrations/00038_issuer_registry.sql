-- Owner: identity/authn lane. Phase: Gate A.
-- AUTHN-001: the tenant federation issuer registry
-- (internal/authn/issuerregistry).
--
-- Storage disposition (see internal/authn/issuerregistry/doc.go for the
-- full reasoning). AUTHN-001 offered two options: reuse
-- internal/platform/configregistry's existing config_object /
-- config_object_activation tables (migration 00027) under a new "ISSUER"
-- object kind, or take a new migration. The first does not fit:
-- internal/platform/configregistry.Kind is a closed, nine-member
-- vocabulary (WORKFLOW, POLICY, SCHEMA, RULE, CONNECTOR, AGENT, REFERENCE,
-- MAPPING, CAPABILITY) with no ISSUER member, enforced both in Go
-- (Kind.Valid) and again by migration 00027's own CHECK constraint, and
-- neither internal/platform/configregistry, internal/data/configregistry
-- nor migration 00027 are in this lane's file roots to widen. Repurposing
-- an existing member (CONNECTOR) to secretly mean "federation issuer" would
-- mislabel every row under a kind that does not describe it -- exactly what
-- a closed, storage-enforced vocabulary exists to prevent. So this
-- migration mints two dedicated tables instead, copying migration 00027's
-- own shape (content-addressed immutable revision + separate append-only
-- governed lifecycle event log) verbatim rather than reinventing it.
--
--   issuer_profile      -- one immutable row per published issuer revision,
--                           keyed by (tenant, issuer_url, revision).
--   issuer_state_event  -- one immutable row per governed lifecycle
--                           transition (DRAFT -> ACTIVE -> SUSPENDED ->
--                           ACTIVE, or -> RETIRED from anywhere non-
--                           terminal). "Active right now" is the row with
--                           the highest event_sequence for a
--                           (tenant, issuer_url) group; superseded rows are
--                           never deleted or updated. RETIRED is terminal:
--                           internal/authn/issuerregistry.Status.terminal
--                           refuses every transition out of it, and this
--                           table's own history makes that permanent and
--                           auditable rather than merely enforced in
--                           application code.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from migration 00027's own
-- copy of migration 00008's pattern, keyed on
-- internal/data/tenancy.WithTenant -- see migration 00027's header comment
-- for the full rationale (cell_id as '' rather than nullable, in
-- particular; this registry has no cell-scoping need, so cell_id is simply
-- omitted rather than carried as a permanently-empty column).

-- +goose Up

CREATE TABLE IF NOT EXISTS issuer_profile (
    tenant_id  tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    issuer_url semantic_key NOT NULL,
    revision   cas_version  NOT NULL,

    audience text NOT NULL,

    -- jwks_kind is internal/authn/issuerregistry.JWKSSourceKind's closed
    -- vocabulary, enforced again here at the storage boundary.
    jwks_kind          text NOT NULL,
    jwks_pinned_keys   jsonb NOT NULL DEFAULT '[]'::jsonb,
    jwks_bundle_ref    text NOT NULL DEFAULT '',
    jwks_bundle_version integer NOT NULL DEFAULT 0,
    jwks_discovery_url text NOT NULL DEFAULT '',

    -- algorithms is internal/trust/federation.Algorithm's closed set
    -- (RS256, ES256, EdDSA); the CHECK below is that same closed
    -- vocabulary, enforced again at the storage boundary.
    algorithms text[] NOT NULL,

    claim_mappings jsonb NOT NULL DEFAULT '[]'::jsonb,

    clock_skew_seconds integer NOT NULL,
    staleness_seconds  integer NOT NULL,

    publisher_principal text        NOT NULL,
    published_at         timestamptz NOT NULL,
    recorded_at           timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, issuer_url, revision),

    CONSTRAINT issuer_profile_jwks_kind_allowed CHECK (
        jwks_kind IN ('PINNED_KEYS', 'PINNED_BUNDLE')
    ),
    CONSTRAINT issuer_profile_algorithms_allowed CHECK (
        algorithms <@ ARRAY['RS256', 'ES256', 'EdDSA']::text[]
        AND array_length(algorithms, 1) >= 1
    ),
    CONSTRAINT issuer_profile_audience_not_blank CHECK (audience <> ''),
    CONSTRAINT issuer_profile_publisher_not_blank CHECK (publisher_principal <> ''),
    CONSTRAINT issuer_profile_clock_skew_non_negative CHECK (clock_skew_seconds >= 0),
    CONSTRAINT issuer_profile_staleness_positive CHECK (staleness_seconds > 0)
);

CREATE OR REPLACE TRIGGER issuer_profile_append_only
    BEFORE UPDATE OR DELETE ON issuer_profile
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON issuer_profile FROM PUBLIC;

CREATE TABLE IF NOT EXISTS issuer_state_event (
    tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    -- event_id is caller-generated (a uuid), matching migration 00027's own
    -- activation_id: the adapter never depends on a database-assigned
    -- identity to name the row it just inserted.
    event_id  uuid       NOT NULL,

    issuer_url semantic_key NOT NULL,
    revision   cas_version  NOT NULL,

    -- event_sequence is gap-free and caller-computed (one more than the
    -- current maximum for this (tenant, issuer_url) group), the same
    -- pattern migration 00027's activation_sequence uses: the UNIQUE
    -- constraint below is what makes a race between two concurrent
    -- transitions for the same issuer fail one of them with a constraint
    -- violation rather than silently losing a lifecycle event.
    event_sequence bigint NOT NULL,

    from_status text NOT NULL DEFAULT '',
    to_status   text NOT NULL,

    acted_by  text NOT NULL,
    authority text NOT NULL DEFAULT '',
    reason    text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, event_id),
    CONSTRAINT issuer_state_event_sequence_unique
        UNIQUE (tenant_id, issuer_url, event_sequence),
    CONSTRAINT issuer_state_event_sequence_positive
        CHECK (event_sequence >= 1),
    CONSTRAINT issuer_state_event_acted_by_not_blank CHECK (acted_by <> ''),
    CONSTRAINT issuer_state_event_from_status_allowed CHECK (
        from_status IN ('', 'DRAFT', 'ACTIVE', 'SUSPENDED', 'RETIRED')
    ),
    CONSTRAINT issuer_state_event_to_status_allowed CHECK (
        to_status IN ('DRAFT', 'ACTIVE', 'SUSPENDED', 'RETIRED')
    ),

    -- Only a published revision may receive a lifecycle event.
    FOREIGN KEY (tenant_id, issuer_url, revision)
        REFERENCES issuer_profile (tenant_id, issuer_url, revision)
);

-- "The active state right now for this issuer", the read Lookup performs:
-- highest event_sequence per (tenant_id, issuer_url).
CREATE INDEX IF NOT EXISTS issuer_state_event_latest
    ON issuer_state_event (tenant_id, issuer_url, event_sequence DESC);

CREATE OR REPLACE TRIGGER issuer_state_event_append_only
    BEFORE UPDATE OR DELETE ON issuer_state_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON issuer_state_event FROM PUBLIC;

-- Tenant isolation (DB-017): fail closed on a missing/blank app.tenant_id
-- session setting.
ALTER TABLE issuer_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE issuer_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON issuer_profile
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE issuer_state_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE issuer_state_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON issuer_state_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Both tables are append-only evidence: SELECT/INSERT only, exactly like
-- config_object and config_object_activation.
GRANT SELECT, INSERT ON issuer_profile TO hcmnext_app;
GRANT SELECT, INSERT ON issuer_state_event TO hcmnext_app;

-- +goose Down
REVOKE ALL ON issuer_state_event FROM hcmnext_app;
REVOKE ALL ON issuer_profile FROM hcmnext_app;

DROP POLICY tenant_isolation ON issuer_state_event;
ALTER TABLE issuer_state_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE issuer_state_event DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON issuer_profile;
ALTER TABLE issuer_profile NO FORCE ROW LEVEL SECURITY;
ALTER TABLE issuer_profile DISABLE ROW LEVEL SECURITY;

DROP TABLE issuer_state_event;
DROP TABLE issuer_profile;
