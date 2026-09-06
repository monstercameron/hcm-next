-- Owner: subscription data plane. Phase: P1B.
-- PERSIST-SUBSCRIPTION-001: durable tenant-scoped event-subscription
-- revisions and the authorization-decision evidence that governs them.
--
-- This migration stores subscription vocabulary and authorization evidence only.
-- Event payloads, delivery attempts and provider calls remain outside the
-- kernel boundary. event_subscription is immutable revision history; the
-- authorization table is append-only evidence.

-- +goose Up

CREATE TABLE IF NOT EXISTS event_subscription (
    tenant_id               tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id                  uuid          NOT NULL,
    subscription_id         semantic_key NOT NULL,
    revision                cas_version  NOT NULL,
    state                   text          NOT NULL,
    requester               text,
    approver                text,
    subscriber              text          NOT NULL,
    event_kinds             jsonb         NOT NULL,
    declared_fields         jsonb,
    filter                  jsonb,
    delivery_endpoint_ref   text          NOT NULL,
    delivery_guarantee      text          NOT NULL,
    tenant_scope            text          NOT NULL,
    organization_scope_ref  text,
    population_scope_ref    text,
    digest                  content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT event_subscription_identity
        UNIQUE (tenant_id, subscription_id, revision),
    CONSTRAINT event_subscription_event_kinds_array
        CHECK (jsonb_typeof(event_kinds) = 'array'),
    CONSTRAINT event_subscription_declared_fields_object
        CHECK (declared_fields IS NULL OR jsonb_typeof(declared_fields) = 'object'),
    CONSTRAINT event_subscription_filter_object
        CHECK (filter IS NULL OR jsonb_typeof(filter) = 'object'),
    CONSTRAINT event_subscription_state_allowed
        CHECK (state IN ('DRAFT', 'ACTIVE', 'PAUSED', 'REVOKED')),
    CONSTRAINT event_subscription_delivery_guarantee_allowed
        CHECK (delivery_guarantee IN ('AT_MOST_ONCE', 'AT_LEAST_ONCE', 'EFFECTIVELY_ONCE')),
    CONSTRAINT event_subscription_requester_not_blank
        CHECK (requester IS NULL OR requester <> ''),
    CONSTRAINT event_subscription_approver_not_blank
        CHECK (approver IS NULL OR approver <> ''),
    CONSTRAINT event_subscription_tenant_scope_not_blank
        CHECK (tenant_scope <> ''),
    CONSTRAINT event_subscription_endpoint_not_blank
        CHECK (delivery_endpoint_ref <> '')
);

CREATE OR REPLACE TRIGGER event_subscription_append_only
    BEFORE UPDATE OR DELETE ON event_subscription
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON event_subscription FROM PUBLIC;

CREATE TABLE IF NOT EXISTS subscription_authorization_event (
    tenant_id         tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    row_id            uuid          NOT NULL,
    subscription_id   semantic_key NOT NULL,
    revision          cas_version  NOT NULL,
    allowed           boolean       NOT NULL,
    rule              text,
    reason            text,
    grant_digest      content_digest,
    decision_digest   content_digest,
    digest            content_digest NOT NULL,
    event_sequence    bigint        NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT subscription_authorization_event_sequence_unique
        UNIQUE (tenant_id, subscription_id, event_sequence),
    CONSTRAINT subscription_authorization_event_sequence_positive
        CHECK (event_sequence >= 1),
    CONSTRAINT subscription_authorization_event_rule_not_blank
        CHECK (rule IS NULL OR rule <> ''),
    CONSTRAINT subscription_authorization_event_reason_not_blank
        CHECK (reason IS NULL OR reason <> ''),
    CONSTRAINT subscription_authorization_event_subscription_fk
        FOREIGN KEY (tenant_id, subscription_id, revision)
        REFERENCES event_subscription (tenant_id, subscription_id, revision)
);

CREATE INDEX IF NOT EXISTS subscription_authorization_event_latest
    ON subscription_authorization_event (tenant_id, subscription_id, event_sequence DESC);

CREATE OR REPLACE TRIGGER subscription_authorization_event_append_only
    BEFORE UPDATE OR DELETE ON subscription_authorization_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON subscription_authorization_event FROM PUBLIC;

-- Tenant isolation (DB-017): missing or blank app.tenant_id fails closed.
ALTER TABLE event_subscription ENABLE ROW LEVEL SECURITY;
ALTER TABLE event_subscription FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON event_subscription
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE subscription_authorization_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscription_authorization_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON subscription_authorization_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON event_subscription TO hcmnext_app;
GRANT SELECT, INSERT ON subscription_authorization_event TO hcmnext_app;

-- +goose Down

REVOKE ALL ON subscription_authorization_event FROM hcmnext_app;
REVOKE ALL ON event_subscription FROM hcmnext_app;

DROP POLICY tenant_isolation ON subscription_authorization_event;
ALTER TABLE subscription_authorization_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE subscription_authorization_event DISABLE ROW LEVEL SECURITY;

DROP POLICY tenant_isolation ON event_subscription;
ALTER TABLE event_subscription NO FORCE ROW LEVEL SECURITY;
ALTER TABLE event_subscription DISABLE ROW LEVEL SECURITY;

DROP TRIGGER subscription_authorization_event_append_only ON subscription_authorization_event;
DROP TABLE subscription_authorization_event;
DROP TRIGGER event_subscription_append_only ON event_subscription;
DROP TABLE event_subscription;
