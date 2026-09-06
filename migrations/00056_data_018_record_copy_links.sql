-- Owner: data plane. DATA-018 records/holds copy coverage.
-- A declaration names every material copy touched by the lifecycle. The live
-- link is updated for serving decisions; the event table preserves each hold
-- and disposition transition as immutable evidence.

-- +goose Up

CREATE TABLE record_copy_link (
    tenant_id              tenant_ref  NOT NULL REFERENCES tenant (tenant_id),
    link_id                uuid        NOT NULL,
    declaration_id         uuid        NOT NULL,
    copy_type              text        NOT NULL,
    store_ref               semantic_key NOT NULL,
    artifact_ref            text        NOT NULL DEFAULT '',
    ledger_stream           text        NOT NULL DEFAULT '',
    ledger_sequence         bigint      NOT NULL DEFAULT 0,
    outbox_id               uuid,
    hold_state              text        NOT NULL DEFAULT 'NONE',
    disposition_state       text        NOT NULL DEFAULT 'PENDING',
    exception_reason        text        NOT NULL DEFAULT '',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, link_id),
    FOREIGN KEY (tenant_id, declaration_id) REFERENCES record_declaration (tenant_id, declaration_id),
    CONSTRAINT record_copy_link_type_allowed CHECK (copy_type IN ('CANONICAL','PROJECTION','EXPORT','ARTIFACT','OUTBOX','BACKUP','PROVIDER','SEARCH')),
    CONSTRAINT record_copy_link_hold_allowed CHECK (hold_state IN ('NONE','HELD','RELEASED')),
    CONSTRAINT record_copy_link_disposition_allowed CHECK (disposition_state IN ('PENDING','HELD','DISPOSED','EXCEPTION')),
    CONSTRAINT record_copy_link_sequence_non_negative CHECK (ledger_sequence >= 0),
    CONSTRAINT record_copy_link_unique UNIQUE (tenant_id, declaration_id, copy_type, store_ref)
);

CREATE TABLE record_copy_event (
    tenant_id       tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    event_id        uuid       NOT NULL,
    link_id         uuid       NOT NULL,
    declaration_id  uuid       NOT NULL,
    event_type      text       NOT NULL,
    hold_id         uuid,
    detail          jsonb      NOT NULL DEFAULT '{}'::jsonb,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, event_id),
    FOREIGN KEY (tenant_id, link_id) REFERENCES record_copy_link (tenant_id, link_id),
    FOREIGN KEY (tenant_id, declaration_id) REFERENCES record_declaration (tenant_id, declaration_id),
    CONSTRAINT record_copy_event_type_allowed CHECK (event_type IN ('REGISTERED','HOLD_PLACED','HOLD_RELEASED','DISPOSITIONED','EXCEPTION')),
    CONSTRAINT record_copy_event_detail_object CHECK (jsonb_typeof(detail) = 'object')
);

CREATE TRIGGER record_copy_event_append_only
    BEFORE UPDATE OR DELETE ON record_copy_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

ALTER TABLE record_copy_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE record_copy_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON record_copy_link
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE record_copy_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE record_copy_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON record_copy_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON record_copy_link TO hcmnext_app;
GRANT SELECT, INSERT ON record_copy_event TO hcmnext_app;
REVOKE UPDATE, DELETE ON record_copy_event FROM PUBLIC;

-- +goose Down

REVOKE ALL ON record_copy_event FROM hcmnext_app;
REVOKE ALL ON record_copy_link FROM hcmnext_app;
DROP POLICY tenant_isolation ON record_copy_event;
DROP POLICY tenant_isolation ON record_copy_link;
ALTER TABLE record_copy_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE record_copy_event DISABLE ROW LEVEL SECURITY;
ALTER TABLE record_copy_link NO FORCE ROW LEVEL SECURITY;
ALTER TABLE record_copy_link DISABLE ROW LEVEL SECURITY;
DROP TABLE record_copy_event;
DROP TABLE record_copy_link;
