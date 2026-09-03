-- Owner: data plane. Phase: P1A.
-- DB-005: shared tenant, cell and temporal primitives. Business time is always
-- explicit and half-open [from, to); it is never inferred from created_at.

-- +goose Up

-- Canonical identifier domains. Every authoritative table below builds its
-- tenant scope from these, so tenancy cannot be a session default.
CREATE DOMAIN tenant_ref AS uuid;
COMMENT ON DOMAIN tenant_ref IS 'Tenant identity. Authoritative rows must carry it explicitly.';

CREATE DOMAIN semantic_key AS text
    CONSTRAINT semantic_key_not_blank CHECK (VALUE <> '' AND VALUE = btrim(VALUE));

CREATE DOMAIN cas_version AS bigint
    CONSTRAINT cas_version_positive CHECK (VALUE >= 1);

CREATE DOMAIN digest_algorithm AS text
    CONSTRAINT digest_algorithm_allowed CHECK (VALUE IN ('sha256'));

CREATE DOMAIN content_digest AS text
    CONSTRAINT content_digest_hex CHECK (VALUE ~ '^[0-9a-f]{64}$');

CREATE TABLE tenant (
    tenant_id          tenant_ref   PRIMARY KEY,
    tenant_key         semantic_key NOT NULL UNIQUE,
    cell_id            semantic_key NOT NULL,
    cell_epoch         bigint       NOT NULL DEFAULT 1,
    display_name       text         NOT NULL,
    status             text         NOT NULL,
    -- Business validity of the tenant registration, half-open.
    effective_from     timestamptz  NOT NULL,
    effective_to       timestamptz,
    -- Recording time. Distinct from business time by construction.
    recorded_at        timestamptz  NOT NULL DEFAULT now(),
    created_at         timestamptz  NOT NULL DEFAULT now(),
    instance_version   cas_version  NOT NULL DEFAULT 1,
    CONSTRAINT tenant_status_allowed CHECK (
        status IN ('PROVISIONING', 'ACTIVE', 'SUSPENDED', 'EXITED')
    ),
    CONSTRAINT tenant_cell_epoch_positive CHECK (cell_epoch >= 1),
    CONSTRAINT tenant_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);

CREATE INDEX tenant_cell ON tenant (cell_id, cell_epoch);

-- +goose Down
DROP TABLE tenant;
DROP DOMAIN content_digest;
DROP DOMAIN digest_algorithm;
DROP DOMAIN cas_version;
DROP DOMAIN semantic_key;
DROP DOMAIN tenant_ref;
