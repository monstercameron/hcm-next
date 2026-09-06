-- Owner: governance-and-trust. Phase: P1A.
-- SECARCH-002: a tenant-scoped, RLS-protected durable backing for
-- internal/trust/session.Manager, replacing the "in memory only" gap
-- doc.go names explicitly. See internal/trust/session/store.go for the
-- durable [session.Store] port this migration backs, and
-- internal/trust/session/pgstore for the adapter.
--
-- Number 00039/00040 are deliberately skipped here: other lanes working
-- concurrently against this same migrations/ directory are expected to take
-- those two (reconciliation jobs, subject links), so this migration takes
-- 00041 to avoid a collision rather than racing on the same next-free
-- number.
--
-- Four tables, two different trust postures within the same feature:
--
--   trust_session_pointer          -- session_id -> tenant_id only, NOT
--                                      RLS-protected (see below).
--   trust_session                  -- the actual session record (subject,
--                                      principal fingerprint, assurance,
--                                      status, timeouts, the CURRENT refresh
--                                      token's hash). RLS-protected,
--                                      tenant-scoped, mutable (status
--                                      transitions and rotation are updates
--                                      in place, never a delete).
--   trust_session_refresh_generation -- append-only: one row per
--                                      (tenant, session, generation), the
--                                      refresh-token family history TRUST-003's
--                                      replay detection depends on. NOT
--                                      RLS-protected (see below).
--   trust_session_evidence         -- append-only lifecycle evidence,
--                                      mirroring internal/trust/session's own
--                                      in-memory Evidence trail. RLS-protected.
--
-- Why trust_session_pointer and trust_session_refresh_generation are not
-- row-level-security scoped, while trust_session and trust_session_evidence
-- are: internal/trust/session.Manager's public contract looks a session up
-- by its own opaque, server-generated identifier (Get/Touch/Revoke/Evidence)
-- or by a presented, opaque refresh-token hash (Refresh) -- never by a
-- caller-supplied tenant plus id. That is a deliberate part of TRUST-003's
-- design (a bearer session id or refresh token proves its own possession;
-- the caller is not trusted to also correctly assert which tenant it
-- belongs to before the lookup even happens), but it means the *very first*
-- statement of any of those four operations cannot yet set
-- migration 00008's app.tenant_id session setting -- there is no tenant to
-- set it to until the id or the token hash has been resolved to one.
-- Every tenant-scoped table in this codebase enforces RLS by comparing
-- tenant_id against that session setting (00008's own fail-closed
-- predicate), so a table that must answer "which tenant does this opaque
-- identifier belong to" cannot itself carry that same policy without making
-- the lookup it exists to answer impossible for every tenant at once
-- (0008's NULLIF(...)::uuid predicate matches no row when the setting is
-- unset). internal/authn/issuerregistry/pgstore.go hits the same
-- chicken-and-egg shape resolving a tenant_key to a tenant_id and resolves
-- it by requiring the caller-supplied identity be the storage uuid
-- directly; this migration resolves it by giving the two id/token-keyed
-- lookup tables no row content beyond a hash-or-id-to-(tenant,session)
-- pointer -- no subject, no principal fingerprint, no assurance level, no
-- status. Both are reachable only by a caller who already possesses the
-- exact unguessable identifier (a 128-bit random session id) or the exact
-- unguessable bearer secret (a 256-bit random refresh token, stored here
-- only as its SHA-256 hash, exactly as internal/trust/session's own
-- in-memory Manager already treats it) -- the same trust boundary a bearer
-- credential already carries, not a weaker one. The tables that hold actual
-- session content (trust_session) or a readable lifecycle history
-- (trust_session_evidence) are reached only after that pointer resolution
-- has already established the tenant scope, inside the very same
-- transaction, and both carry migration 00008's ordinary tenant_isolation
-- policy with FORCE ROW LEVEL SECURITY, unmodified from every other
-- tenant-scoped table in this tree.
--
-- Storage disposition (see also planning/todos.md's SECARCH-002 entry):
--   trust_session                    -- role: RUNTIME.  Retention: for the
--     life of the session plus whatever the evidence trail's retention
--     class requires (rows are never deleted by this migration; a future
--     retention sweep is a separate, undelivered todo). rebuild_source:
--     none -- this *is* the durable source of truth TRUST-003's in-memory
--     Manager stood in for; it is not a projection of anything else.
--     tenant column: tenant_id. RLS: tenant_isolation (FORCE).
--   trust_session_pointer            -- role: RUNTIME (routing index).
--     Retention: same lifetime as the trust_session row it points at.
--     rebuild_source: trust_session itself (session_id, tenant_id) --
--     technically re-derivable by scanning trust_session without RLS, so
--     this table is an index for a lookup RLS makes otherwise impossible,
--     not independent authority. tenant column: tenant_id (not RLS-scoped;
--     see rationale above). RLS: none (documented exception).
--   trust_session_refresh_generation -- role: LEDGER (append-only replay
--     history). Retention: for the life of the session family; never
--     mutated or deleted once written. rebuild_source: none -- this is the
--     authoritative record of which refresh-token generation was ever
--     issued, which is exactly what replay detection is checked against.
--     tenant column: tenant_id (not RLS-scoped; see rationale above). RLS:
--     none (documented exception); append-only via forbid_mutation.
--   trust_session_evidence           -- role: LEDGER (append-only lifecycle
--     audit trail). Retention: matches internal/trust/session's own
--     evidence semantics (durable, currently undated for expiry by this
--     migration). rebuild_source: none. tenant column: tenant_id. RLS:
--     tenant_isolation (FORCE); append-only via forbid_mutation.

-- +goose Up

CREATE TABLE IF NOT EXISTS trust_session_pointer (
    session_id text       NOT NULL,
    tenant_id  tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (session_id),
    CONSTRAINT trust_session_pointer_session_id_not_blank CHECK (session_id <> '')
);

CREATE OR REPLACE TRIGGER trust_session_pointer_append_only
    BEFORE UPDATE OR DELETE ON trust_session_pointer
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON trust_session_pointer FROM PUBLIC;

CREATE TABLE IF NOT EXISTS trust_session (
    tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    session_id text      NOT NULL,

    subject               text NOT NULL,
    principal_fingerprint text NOT NULL,
    -- assurance is trust.Assurance's closed wire vocabulary
    -- (low/substantial/high), enforced again here at the storage boundary.
    assurance text NOT NULL,
    -- status is session.Status's closed wire vocabulary.
    status         text NOT NULL,
    revoked_reason text NOT NULL DEFAULT '',

    created_at           timestamptz NOT NULL,
    last_activity_at     timestamptz NOT NULL,
    idle_timeout_seconds bigint      NOT NULL,
    absolute_expires_at  timestamptz NOT NULL,
    rotation_count       integer     NOT NULL DEFAULT 0,

    -- current_token_hash/current_generation name the one refresh token that
    -- currently rotates this session; both are also carried, redundantly,
    -- by whichever row of trust_session_refresh_generation has this
    -- session's highest generation -- redundant on purpose, since that is
    -- exactly the fact [pgstore.PGStore.Refresh] must check with a single
    -- indexed read on this row, not a join, on every refresh call.
    current_token_hash text        NOT NULL,
    current_generation bigint      NOT NULL DEFAULT 0,
    -- version is this row's compare-and-swap fence: every mutating
    -- statement's WHERE clause requires the version it last observed and
    -- increments it by exactly one, so a second writer -- another replica,
    -- or a concurrent request against the same replica -- that last observed
    -- a now-stale version has its write refused (zero rows affected) rather
    -- than silently overwriting a change it never saw.
    version cas_version NOT NULL DEFAULT 1,

    PRIMARY KEY (tenant_id, session_id),
    CONSTRAINT trust_session_status_allowed CHECK (
        status IN ('ACTIVE', 'EXPIRED_IDLE', 'EXPIRED_ABSOLUTE', 'REVOKED')
    ),
    CONSTRAINT trust_session_assurance_allowed CHECK (
        assurance IN ('low', 'substantial', 'high')
    ),
    CONSTRAINT trust_session_subject_not_blank CHECK (subject <> ''),
    CONSTRAINT trust_session_fingerprint_not_blank CHECK (principal_fingerprint <> ''),
    CONSTRAINT trust_session_token_hash_not_blank CHECK (current_token_hash <> ''),
    CONSTRAINT trust_session_idle_timeout_positive CHECK (idle_timeout_seconds > 0),
    CONSTRAINT trust_session_generation_non_negative CHECK (current_generation >= 0),
    FOREIGN KEY (session_id) REFERENCES trust_session_pointer (session_id)
);

REVOKE DELETE ON trust_session FROM PUBLIC;

CREATE TABLE IF NOT EXISTS trust_session_refresh_generation (
    tenant_id  tenant_ref NOT NULL,
    session_id text       NOT NULL,
    generation bigint     NOT NULL,
    token_hash text       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, session_id, generation),
    CONSTRAINT trust_session_refresh_generation_hash_unique UNIQUE (token_hash),
    CONSTRAINT trust_session_refresh_generation_non_negative CHECK (generation >= 0),
    CONSTRAINT trust_session_refresh_generation_hash_not_blank CHECK (token_hash <> ''),
    FOREIGN KEY (tenant_id, session_id) REFERENCES trust_session (tenant_id, session_id)
);

CREATE OR REPLACE TRIGGER trust_session_refresh_generation_append_only
    BEFORE UPDATE OR DELETE ON trust_session_refresh_generation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON trust_session_refresh_generation FROM PUBLIC;

CREATE TABLE IF NOT EXISTS trust_session_evidence (
    tenant_id   tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    evidence_id text        NOT NULL DEFAULT gen_random_uuid()::text,
    session_id  text        NOT NULL,

    -- kind is session.EvidenceKind's closed wire vocabulary.
    kind   text NOT NULL,
    reason text NOT NULL DEFAULT '',

    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, evidence_id),
    CONSTRAINT trust_session_evidence_kind_allowed CHECK (
        kind IN ('CREATED', 'ROTATED', 'DENIED', 'REVOKED', 'EXPIRED')
    ),
    FOREIGN KEY (tenant_id, session_id) REFERENCES trust_session (tenant_id, session_id)
);

CREATE INDEX IF NOT EXISTS trust_session_evidence_by_session
    ON trust_session_evidence (tenant_id, session_id, occurred_at);

CREATE OR REPLACE TRIGGER trust_session_evidence_append_only
    BEFORE UPDATE OR DELETE ON trust_session_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON trust_session_evidence FROM PUBLIC;

-- Row level security: trust_session and trust_session_evidence only. See
-- this migration's header comment for why trust_session_pointer and
-- trust_session_refresh_generation deliberately do not carry it.
ALTER TABLE trust_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE trust_session FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON trust_session
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE trust_session_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE trust_session_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON trust_session_evidence
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Grants. trust_session is mutable in place (status transitions, rotation)
-- but never deleted, matching migration 00008's own "no DELETE anywhere"
-- rule. The other three are append-only: SELECT and INSERT only.
GRANT SELECT, INSERT ON trust_session_pointer TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON trust_session TO hcmnext_app;
GRANT SELECT, INSERT ON trust_session_refresh_generation TO hcmnext_app;
GRANT SELECT, INSERT ON trust_session_evidence TO hcmnext_app;

-- +goose Down
REVOKE ALL ON trust_session_evidence FROM hcmnext_app;
REVOKE ALL ON trust_session_refresh_generation FROM hcmnext_app;
REVOKE ALL ON trust_session FROM hcmnext_app;
REVOKE ALL ON trust_session_pointer FROM hcmnext_app;

DROP POLICY tenant_isolation ON trust_session_evidence;
ALTER TABLE trust_session_evidence NO FORCE ROW LEVEL SECURITY;
ALTER TABLE trust_session_evidence DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON trust_session;
ALTER TABLE trust_session NO FORCE ROW LEVEL SECURITY;
ALTER TABLE trust_session DISABLE ROW LEVEL SECURITY;

DROP TABLE trust_session_evidence;
DROP TABLE trust_session_refresh_generation;
DROP TABLE trust_session;
DROP TABLE trust_session_pointer;
