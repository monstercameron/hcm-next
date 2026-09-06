-- Owner: equity data lane. Phase: P4.
-- PERSIST-EQUITY-001: immutable equity plan and grant revisions plus the
-- acceptance-evidence ledger. Allocation, broker calls and document
-- generation remain outside this table set.
--
-- Storage disposition:
--   equity_plan_revision: AGGREGATE, PERMANENT, tenant_id, immutable revision.
--   equity_grant: AGGREGATE, PERMANENT, tenant_id, immutable revision.
--   equity_acceptance_event: LEDGER, PERMANENT, tenant_id, append-only.

-- +goose Up

CREATE TABLE IF NOT EXISTS equity_plan_revision (
    row_id               uuid          NOT NULL,
    tenant_id            tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    plan_id              semantic_key  NOT NULL,
    revision             cas_version   NOT NULL,
    name                 text          NOT NULL,
    pool_ref             text          NOT NULL,
    authorized_quantity  numeric(19,4),
    currency             text,
    instrument_kinds     jsonb,
    approval_ref         text          NOT NULL,
    parent_digest        content_digest,
    supersedes_revision  cas_version,
    canonical_digest     content_digest NOT NULL,
    digest               content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, plan_id, revision)
);

CREATE TABLE IF NOT EXISTS equity_grant (
    row_id               uuid          NOT NULL,
    tenant_id            tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    grant_id             semantic_key  NOT NULL,
    revision             cas_version   NOT NULL,
    -- plan_id is required by the composite foreign key in the PERSIST-
    -- EQUITY-001 contract; the domain grant carries the plan digest/revision,
    -- and the adapter resolves that digest to this stable plan identity.
    plan_id              semantic_key  NOT NULL,
    plan_digest          content_digest NOT NULL,
    plan_revision        cas_version   NOT NULL,
    pool_ref             text          NOT NULL,
    worker_ref           uuid          NOT NULL,
    instrument_kind      text          NOT NULL,
    quantity             numeric(19,4) NOT NULL,
    grant_date           date,
    strike_price         numeric(19,4),
    currency             text,
    vesting              jsonb,
    state                text          NOT NULL,
    acceptance_digest    content_digest,
    approval_ref         text,
    evidence_ref         text,
    parent_digest        content_digest,
    supersedes_revision  cas_version,
    canonical_digest     content_digest NOT NULL,
    digest               content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, grant_id, revision),
    FOREIGN KEY (tenant_id, plan_id, plan_revision)
        REFERENCES equity_plan_revision (tenant_id, plan_id, revision)
);

CREATE TABLE IF NOT EXISTS equity_acceptance_event (
    row_id               uuid          NOT NULL,
    tenant_id            tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    event_id             semantic_key  NOT NULL,
    grant_digest         content_digest NOT NULL,
    grant_revision       cas_version   NOT NULL,
    accepted_by          uuid          NOT NULL,
    accepted_at          timestamptz   NOT NULL,
    evidence_ref         text          NOT NULL,
    canonical_digest     content_digest NOT NULL,
    digest               content_digest NOT NULL,
    event_sequence       bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, event_id),
    UNIQUE (tenant_id, grant_digest, event_sequence),
    CONSTRAINT equity_acceptance_event_sequence_positive CHECK (event_sequence >= 1)
);

-- Plan and grant revisions are immutable rows. Acceptance evidence is an
-- append-only ledger; all three use the shared mutation guard.
CREATE OR REPLACE TRIGGER equity_plan_revision_append_only
    BEFORE UPDATE OR DELETE ON equity_plan_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER equity_grant_append_only
    BEFORE UPDATE OR DELETE ON equity_grant
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER equity_acceptance_event_append_only
    BEFORE UPDATE OR DELETE ON equity_acceptance_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE ALL ON equity_plan_revision FROM PUBLIC;
REVOKE ALL ON equity_grant FROM PUBLIC;
REVOKE ALL ON equity_acceptance_event FROM PUBLIC;

ALTER TABLE equity_plan_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE equity_plan_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON equity_plan_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE equity_grant ENABLE ROW LEVEL SECURITY;
ALTER TABLE equity_grant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON equity_grant
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE equity_acceptance_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE equity_acceptance_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON equity_acceptance_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON equity_plan_revision TO hcmnext_app;
GRANT SELECT, INSERT ON equity_grant TO hcmnext_app;
GRANT SELECT, INSERT ON equity_acceptance_event TO hcmnext_app;

-- +goose Down

REVOKE ALL ON equity_acceptance_event FROM hcmnext_app;
REVOKE ALL ON equity_grant FROM hcmnext_app;
REVOKE ALL ON equity_plan_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON equity_acceptance_event;
ALTER TABLE equity_acceptance_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE equity_acceptance_event DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON equity_grant;
ALTER TABLE equity_grant NO FORCE ROW LEVEL SECURITY;
ALTER TABLE equity_grant DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON equity_plan_revision;
ALTER TABLE equity_plan_revision NO FORCE ROW LEVEL SECURITY;
ALTER TABLE equity_plan_revision DISABLE ROW LEVEL SECURITY;

DROP TABLE equity_acceptance_event;
DROP TABLE equity_grant;
DROP TABLE equity_plan_revision;
