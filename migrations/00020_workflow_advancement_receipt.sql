-- Owner: workflow-runtime lane. Phase: P1B.
-- Durable identity for one successfully committed workflow advancement.
--
-- The optimistic instance version is part of the command identity. That is
-- important for nodes which first park and later resume: both commands may
-- address the same node attempt, but they act on distinct instance versions.
-- The resulting version bounds replay to the instant immediately after that
-- command. Once any later node advances the instance, the old command is
-- stale even if its recorded node outcome still matches byte-for-byte.

-- +goose Up

CREATE TABLE IF NOT EXISTS workflow_advancement_receipt (
    tenant_id                 tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    instance_id               uuid           NOT NULL,
    node_id                   semantic_key   NOT NULL,
    attempt                   integer        NOT NULL,
    expected_instance_version cas_version    NOT NULL,

    request_digest            content_digest NOT NULL,
    resulting_instance_version cas_version   NOT NULL,
    receipt                   jsonb          NOT NULL,
    recorded_at               timestamptz    NOT NULL DEFAULT now(),

    PRIMARY KEY (
        tenant_id, instance_id, node_id, attempt, expected_instance_version
    ),
    FOREIGN KEY (tenant_id, instance_id)
        REFERENCES workflow_instance (tenant_id, instance_id),

    CONSTRAINT workflow_advancement_receipt_attempt_positive CHECK (attempt >= 1),
    CONSTRAINT workflow_advancement_receipt_version_advances CHECK (
        resulting_instance_version > expected_instance_version
    ),
    CONSTRAINT workflow_advancement_receipt_payload_object CHECK (
        jsonb_typeof(receipt) = 'object'
    )
);

CREATE INDEX IF NOT EXISTS workflow_advancement_receipt_result
    ON workflow_advancement_receipt (
        tenant_id, instance_id, resulting_instance_version
    );

ALTER TABLE workflow_advancement_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE workflow_advancement_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workflow_advancement_receipt
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- An advancement receipt is immutable evidence of a committed command.
REVOKE UPDATE, DELETE ON workflow_advancement_receipt FROM PUBLIC;
GRANT SELECT, INSERT ON workflow_advancement_receipt TO hcmnext_app;

-- +goose Down
REVOKE ALL ON workflow_advancement_receipt FROM hcmnext_app;
DROP POLICY tenant_isolation ON workflow_advancement_receipt;
ALTER TABLE workflow_advancement_receipt NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workflow_advancement_receipt DISABLE ROW LEVEL SECURITY;
DROP TABLE workflow_advancement_receipt;
