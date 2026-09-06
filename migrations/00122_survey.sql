-- PERSIST-SURVEY-001: durable question-bank, survey, campaign, sample,
-- launch and response records. The four definition/sample tables are
-- permanent AGGREGATE data; launches and responses are permanent LEDGER data.
-- Every table is tenant scoped and is reachable by hcmnext_app only through a
-- transaction carrying app.tenant_id via tenancy.WithTenant.

-- +goose Up

CREATE TABLE IF NOT EXISTS survey_question_bank_revision (
    tenant_id   tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id      uuid NOT NULL,
    bank_id     text NOT NULL,
    revision    bigint NOT NULL,
    created_at  timestamptz NOT NULL,
    questions   jsonb NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, bank_id, revision)
);

CREATE TABLE IF NOT EXISTS survey_revision (
    tenant_id         tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id            uuid NOT NULL,
    survey_id         text NOT NULL,
    revision          bigint NOT NULL,
    title             text NOT NULL,
    description       text,
    question_bank_ref text NOT NULL,
    created_at        timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, survey_id, revision)
);

CREATE TABLE IF NOT EXISTS survey_campaign_revision (
    tenant_id              tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id                 uuid NOT NULL,
    campaign_id            text NOT NULL,
    revision               bigint NOT NULL,
    survey_ref             text NOT NULL,
    population_snapshot_ref text,
    window_start           timestamptz NOT NULL,
    window_end             timestamptz NOT NULL,
    anonymity_threshold    int NOT NULL,
    channel_refs           jsonb,
    created_at             timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, campaign_id, revision)
);

CREATE TABLE IF NOT EXISTS survey_campaign_sample (
    tenant_id           tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid NOT NULL,
    campaign_id         text NOT NULL,
    binding             jsonb,
    rule                jsonb,
    member_ids          jsonb NOT NULL,
    membership_protected boolean NOT NULL DEFAULT true,
    count               int NOT NULL,
    frozen_at           timestamptz NOT NULL,
    digest              content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, campaign_id)
);

CREATE TABLE IF NOT EXISTS survey_launch_record (
    tenant_id             tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id                uuid NOT NULL,
    campaign_id           text NOT NULL,
    campaign_revision     bigint NOT NULL,
    campaign_digest       content_digest,
    purpose               text,
    sample_digest         content_digest,
    membership_protected  boolean NOT NULL,
    template_key          text,
    template_version      text,
    template_digest       text,
    audience_digest       content_digest,
    delivery_plan_digest  content_digest,
    launched_by           text,
    approver              text,
    launched_at           timestamptz NOT NULL,
    reminder_policy       jsonb,
    digest                content_digest NOT NULL,
    event_sequence        bigint NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, campaign_id, event_sequence)
);

CREATE TABLE IF NOT EXISTS survey_response_record (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id             uuid NOT NULL,
    campaign_id        text NOT NULL,
    campaign_revision  bigint NOT NULL,
    form_definition    jsonb NOT NULL,
    response_key       text NOT NULL,
    response_revision  int NOT NULL,
    answers            jsonb NOT NULL,
    validation_digest  content_digest,
    submitted_at       timestamptz NOT NULL,
    digest             content_digest NOT NULL,
    event_sequence     bigint NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, campaign_id, response_key, event_sequence)
);

-- Revisions and frozen samples are immutable aggregate facts. Launches and
-- responses are immutable ledger facts. All six therefore fail closed even
-- for an owner-level accidental UPDATE/DELETE, while hcmnext_app is granted
-- only SELECT and INSERT below.
CREATE OR REPLACE TRIGGER survey_question_bank_revision_append_only
    BEFORE UPDATE OR DELETE ON survey_question_bank_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER survey_revision_append_only
    BEFORE UPDATE OR DELETE ON survey_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER survey_campaign_revision_append_only
    BEFORE UPDATE OR DELETE ON survey_campaign_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER survey_campaign_sample_append_only
    BEFORE UPDATE OR DELETE ON survey_campaign_sample
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER survey_launch_record_append_only
    BEFORE UPDATE OR DELETE ON survey_launch_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE OR REPLACE TRIGGER survey_response_record_append_only
    BEFORE UPDATE OR DELETE ON survey_response_record
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON survey_question_bank_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON survey_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON survey_campaign_revision FROM PUBLIC;
REVOKE UPDATE, DELETE ON survey_campaign_sample FROM PUBLIC;
REVOKE UPDATE, DELETE ON survey_launch_record FROM PUBLIC;
REVOKE UPDATE, DELETE ON survey_response_record FROM PUBLIC;

ALTER TABLE survey_question_bank_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_question_bank_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON survey_question_bank_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE survey_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON survey_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE survey_campaign_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_campaign_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON survey_campaign_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE survey_campaign_sample ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_campaign_sample FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON survey_campaign_sample
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE survey_launch_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_launch_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON survey_launch_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE survey_response_record ENABLE ROW LEVEL SECURITY;
ALTER TABLE survey_response_record FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON survey_response_record
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON survey_question_bank_revision TO hcmnext_app;
GRANT SELECT, INSERT ON survey_revision TO hcmnext_app;
GRANT SELECT, INSERT ON survey_campaign_revision TO hcmnext_app;
GRANT SELECT, INSERT ON survey_campaign_sample TO hcmnext_app;
GRANT SELECT, INSERT ON survey_launch_record TO hcmnext_app;
GRANT SELECT, INSERT ON survey_response_record TO hcmnext_app;

-- +goose Down

REVOKE ALL ON survey_response_record FROM hcmnext_app;
REVOKE ALL ON survey_launch_record FROM hcmnext_app;
REVOKE ALL ON survey_campaign_sample FROM hcmnext_app;
REVOKE ALL ON survey_campaign_revision FROM hcmnext_app;
REVOKE ALL ON survey_revision FROM hcmnext_app;
REVOKE ALL ON survey_question_bank_revision FROM hcmnext_app;

DROP POLICY tenant_isolation ON survey_response_record;
DROP POLICY tenant_isolation ON survey_launch_record;
DROP POLICY tenant_isolation ON survey_campaign_sample;
DROP POLICY tenant_isolation ON survey_campaign_revision;
DROP POLICY tenant_isolation ON survey_revision;
DROP POLICY tenant_isolation ON survey_question_bank_revision;

DROP TABLE survey_response_record;
DROP TABLE survey_launch_record;
DROP TABLE survey_campaign_sample;
DROP TABLE survey_campaign_revision;
DROP TABLE survey_revision;
DROP TABLE survey_question_bank_revision;
