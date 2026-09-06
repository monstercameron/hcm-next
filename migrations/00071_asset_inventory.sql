-- Owner: asset data plane. Phase: PERSIST-ASSET-001.
--
-- This table set is deliberately separate from migration 00030's companion
-- artifact-quarantine schema. It stores committed equipment inventory and
-- custody facts, not quarantined bytes or quarantine state.

-- +goose Up

CREATE TABLE IF NOT EXISTS asset_inventory (
    row_id          uuid          NOT NULL,
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    inventory_id    text          NOT NULL,
    owner_ref       uuid          NOT NULL,
    classification  text          NOT NULL,
    serial_number   text,
    revision        cas_version   NOT NULL,
    effective_at    timestamptz   NOT NULL,
    status          text          NOT NULL,
    digest          content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, inventory_id, revision),
    CONSTRAINT asset_inventory_status_allowed CHECK (
        status IN ('AVAILABLE', 'ASSIGNED', 'LOST', 'RETURN_PENDING', 'RETURNED', 'RETIRED')
    )
);

CREATE INDEX IF NOT EXISTS asset_inventory_history
    ON asset_inventory (tenant_id, inventory_id, revision DESC);

-- Inventory revisions are immutable facts. A correction appends a new
-- revision, so the prior row must remain readable as history.
CREATE TRIGGER asset_inventory_append_only
    BEFORE UPDATE OR DELETE ON asset_inventory
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON asset_inventory FROM PUBLIC;

CREATE TABLE IF NOT EXISTS asset_custody_event (
    row_id                    uuid          NOT NULL,
    tenant_id                 tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    custody_id                text          NOT NULL,
    asset_ref                 uuid          NOT NULL,
    worker_ref                uuid,
    location                  text,
    condition                 text,
    assignee_receipt          jsonb,
    issuer_receipt            jsonb,
    revision                  cas_version   NOT NULL,
    effective_at              timestamptz   NOT NULL,
    status                    text          NOT NULL,
    event_sequence            bigint        NOT NULL,
    digest                    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, asset_ref, event_sequence),
    CONSTRAINT asset_custody_event_status_allowed CHECK (
        status IN ('AVAILABLE', 'ASSIGNED', 'LOST', 'RETURN_PENDING', 'RETURNED', 'RETIRED')
    ),
    CONSTRAINT asset_custody_event_sequence_positive CHECK (event_sequence >= 1)
);

CREATE INDEX IF NOT EXISTS asset_custody_event_history
    ON asset_custody_event (tenant_id, asset_ref, event_sequence DESC);

-- Custody is an append-only stream. asset_ref is the UUID portion of the
-- domain reference; the natural inventory key is text, so the application
-- validates that the two refer to the same tenant-owned inventory row.
CREATE TRIGGER asset_custody_event_append_only
    BEFORE UPDATE OR DELETE ON asset_custody_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON asset_custody_event FROM PUBLIC;

-- +goose StatementBegin
DO $$
DECLARE target text;
BEGIN
    FOREACH target IN ARRAY ARRAY['asset_inventory', 'asset_custody_event'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I '
            'USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid) '
            'WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
            target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

GRANT SELECT, INSERT ON asset_inventory, asset_custody_event TO hcmnext_app;

-- +goose Down
REVOKE ALL ON asset_custody_event, asset_inventory FROM hcmnext_app;
DROP POLICY tenant_isolation ON asset_custody_event;
DROP POLICY tenant_isolation ON asset_inventory;
DROP TABLE asset_custody_event;
DROP TABLE asset_inventory;
