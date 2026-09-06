-- Owner: data plane (DataOps/control-plane). Phase: P1B minimal depth.
-- CP-001: the immutable configuration-object registry
-- (internal/platform/configregistry). planning/todos.md section 30's
-- disposition note scopes CP-001 to "immutable, versioned configuration
-- objects" only -- signed bundles, activation epochs and distribution are
-- CP-002/003/004, Gate A/Gate C work this migration does not attempt.
--
-- Two tables, both append-only, mirroring migration 00003's
-- definition_version / definition_active_pointer split but with BOTH sides
-- append-only here: a configuration object's activation history is itself
-- permanent evidence (internal/platform/configregistry's own doc.go: "a
-- later activation ... does not delete or edit the earlier record").
--
--   config_object            -- one immutable row per published revision,
--                                keyed by (tenant, cell, kind, id, revision).
--   config_object_activation -- one immutable row per governed activation
--                                act. "Active right now" is the row with the
--                                highest activation_sequence for a
--                                (tenant, cell, kind, id) group; superseded
--                                rows are never deleted or updated.
--
-- cell_id is text NOT NULL DEFAULT '' rather than nullable: a nullable
-- column would make two tenant-wide (no cell) rows for the same
-- (kind, id, revision) indistinguishable from two DIFFERENT cells under
-- Postgres's per-NULL uniqueness semantics (each NULL compares distinct from
-- every other NULL, so a UNIQUE/PRIMARY KEY constraint would not catch a
-- true duplicate). The empty string is the one "tenant-wide, no narrower
-- cell" value, so the primary key stays a real uniqueness guarantee.
--
-- Row level security, the least-privilege grant and the fail-closed
-- app.tenant_id predicate are copied verbatim from
-- migrations/00023_journey_workforce.sql's own copy of
-- migrations/00008_tenant_isolation.sql's pattern (00008 is frozen; every
-- migration after it carries its own copy), keyed on
-- internal/data/tenancy.WithTenant.

-- +goose Up

CREATE TABLE IF NOT EXISTS config_object (
    tenant_id tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    cell_id   text       NOT NULL DEFAULT '',

    -- kind is the closed vocabulary internal/platform/configregistry.Kind
    -- declares. The CHECK below is that Go-level closed vocabulary,
    -- enforced again at the storage boundary so a row can never carry a
    -- kind the Go package itself would refuse.
    kind      text         NOT NULL,
    object_id semantic_key NOT NULL,
    revision  cas_version  NOT NULL,

    -- canonical_body_digest is the content address of body alone (sha256
    -- over body, under configregistry's own canonicalization profile).
    -- record_digest is the whole-row identity digest
    -- (ConfigurationObject.Digest()) a caller can recompute to prove this
    -- row was never tampered with.
    canonical_body_digest content_digest NOT NULL,
    body                  bytea          NOT NULL,
    record_digest         content_digest NOT NULL,

    schema_ref          text NOT NULL,
    publisher_principal text NOT NULL,
    published_at        timestamptz NOT NULL,
    recorded_at         timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, cell_id, kind, object_id, revision),

    CONSTRAINT config_object_kind_allowed CHECK (
        kind IN (
            'WORKFLOW', 'POLICY', 'SCHEMA', 'RULE', 'CONNECTOR', 'AGENT',
            'REFERENCE', 'MAPPING', 'CAPABILITY'
        )
    ),
    CONSTRAINT config_object_schema_ref_not_blank CHECK (schema_ref <> ''),
    CONSTRAINT config_object_publisher_not_blank CHECK (publisher_principal <> '')
);

CREATE OR REPLACE TRIGGER config_object_append_only
    BEFORE UPDATE OR DELETE ON config_object
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON config_object FROM PUBLIC;

CREATE TABLE IF NOT EXISTS config_object_activation (
    tenant_id     tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    -- activation_id is caller-generated (a uuid, like
    -- migration 00017's work_item_transition.transition_id), so the
    -- adapter never depends on a database-assigned identity to name the
    -- row it just inserted.
    activation_id uuid       NOT NULL,

    cell_id   text         NOT NULL DEFAULT '',
    kind      text         NOT NULL,
    object_id semantic_key NOT NULL,
    revision  cas_version  NOT NULL,

    -- activation_sequence is gap-free and caller-computed (one more than the
    -- current maximum for this (tenant, cell, kind, object_id) group, the
    -- same pattern migration 00017's work_item_transition.item_version
    -- uses): the UNIQUE constraint below is what makes a race between two
    -- concurrent activations for the same group fail one of them with a
    -- constraint violation rather than silently losing an activation's
    -- evidence.
    activation_sequence bigint NOT NULL,

    activated_by text NOT NULL,
    authority    text NOT NULL DEFAULT '',
    reason       text NOT NULL DEFAULT '',
    activated_at timestamptz NOT NULL,

    -- object_digest is the activated config_object row's record_digest at
    -- the moment of activation: what this activation act actually named,
    -- independent of whether that row could later be proven tampered.
    object_digest content_digest NOT NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, activation_id),
    CONSTRAINT config_object_activation_sequence_unique
        UNIQUE (tenant_id, cell_id, kind, object_id, activation_sequence),
    CONSTRAINT config_object_activation_sequence_positive
        CHECK (activation_sequence >= 1),
    CONSTRAINT config_object_activation_by_not_blank CHECK (activated_by <> ''),

    -- Only a published revision may be activated: the activation must name
    -- a config_object row that already exists.
    FOREIGN KEY (tenant_id, cell_id, kind, object_id, revision)
        REFERENCES config_object (tenant_id, cell_id, kind, object_id, revision)
);

-- "The active revision right now for this group", the one read Resolve
-- performs: highest activation_sequence per (tenant, cell, kind, object_id).
CREATE INDEX IF NOT EXISTS config_object_activation_latest
    ON config_object_activation (tenant_id, cell_id, kind, object_id, activation_sequence DESC);

CREATE OR REPLACE TRIGGER config_object_activation_append_only
    BEFORE UPDATE OR DELETE ON config_object_activation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON config_object_activation FROM PUBLIC;

-- Tenant isolation (DB-017): fail closed on a missing/blank app.tenant_id
-- session setting.
ALTER TABLE config_object ENABLE ROW LEVEL SECURITY;
ALTER TABLE config_object FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON config_object
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE config_object_activation ENABLE ROW LEVEL SECURITY;
ALTER TABLE config_object_activation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON config_object_activation
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Both tables are append-only evidence: SELECT/INSERT only, exactly like
-- journey_worker, work_item_transition and work_item_decision.
GRANT SELECT, INSERT ON config_object TO hcmnext_app;
GRANT SELECT, INSERT ON config_object_activation TO hcmnext_app;

-- +goose Down
REVOKE ALL ON config_object_activation FROM hcmnext_app;
REVOKE ALL ON config_object FROM hcmnext_app;

DROP POLICY tenant_isolation ON config_object_activation;
ALTER TABLE config_object_activation NO FORCE ROW LEVEL SECURITY;
ALTER TABLE config_object_activation DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON config_object;
ALTER TABLE config_object NO FORCE ROW LEVEL SECURITY;
ALTER TABLE config_object DISABLE ROW LEVEL SECURITY;

DROP TABLE config_object_activation;
DROP TABLE config_object;
