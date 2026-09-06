-- Owner: contact data plane. Phase: PERSIST-CONTACT-001.
-- Digest-backed contact endpoint revisions, current verification challenge
-- state, and the append-only challenge audit stream.

-- +goose Up

CREATE TABLE contact_endpoint_revision (
    row_id                    uuid          NOT NULL,
    tenant_id                 tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    subject_ref               uuid          NOT NULL,
    endpoint_id               uuid          NOT NULL,
    revision                  cas_version   NOT NULL,
    supersedes_revision       cas_version,
    kind                      text          NOT NULL,
    purpose                   text          NOT NULL,
    priority                  integer,
    source                    text,
    normalized_value_digest   content_digest NOT NULL,
    display_hint              text,
    verification              jsonb,
    canonical_digest          content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT contact_endpoint_revision_unique
        UNIQUE (tenant_id, endpoint_id, revision),
    CONSTRAINT contact_endpoint_revision_supersedes_valid
        CHECK (supersedes_revision IS NULL OR supersedes_revision < revision),
    CONSTRAINT contact_endpoint_revision_kind_allowed
        CHECK (kind IN ('EMAIL', 'PHONE')),
    CONSTRAINT contact_endpoint_revision_purpose_not_blank
        CHECK (purpose <> ''),
    CONSTRAINT contact_endpoint_revision_priority_non_negative
        CHECK (priority IS NULL OR priority >= 0)
);

-- Endpoint revisions are immutable facts. Verification changes are represented
-- by a new digest-backed revision, never by rewriting this row.
CREATE TRIGGER contact_endpoint_revision_append_only
    BEFORE UPDATE OR DELETE ON contact_endpoint_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON contact_endpoint_revision FROM PUBLIC;

CREATE TABLE contact_verification_challenge (
    row_id                    uuid          NOT NULL,
    tenant_id                 tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    challenge_id              uuid          NOT NULL,
    subject_ref               uuid          NOT NULL,
    endpoint_id               uuid          NOT NULL,
    endpoint_revision_digest  content_digest,
    normalized_value_digest   content_digest NOT NULL,
    purpose                   text,
    issued_at                 timestamptz   NOT NULL,
    expires_at                timestamptz   NOT NULL,
    attempt_budget            integer       NOT NULL,
    attempts                  integer       NOT NULL DEFAULT 0,
    token_digest              content_digest,
    status                    text          NOT NULL,
    canonical_digest          content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT contact_verification_challenge_unique
        UNIQUE (tenant_id, challenge_id),
    CONSTRAINT contact_verification_challenge_expiry_valid
        CHECK (issued_at < expires_at),
    CONSTRAINT contact_verification_challenge_budget_valid
        CHECK (attempt_budget > 0 AND attempts >= 0 AND attempts <= attempt_budget),
    CONSTRAINT contact_verification_challenge_status_allowed
        CHECK (status IN ('ISSUED', 'VERIFIED', 'EXPIRED', 'EXHAUSTED'))
);

-- This is the one mutable table in the set: it is the current serving state;
-- its complete transition history is kept in contact_challenge_event below.
REVOKE DELETE ON contact_verification_challenge FROM PUBLIC;

CREATE TABLE contact_challenge_event (
    row_id          uuid          NOT NULL,
    tenant_id       tenant_ref    NOT NULL REFERENCES tenant (tenant_id),
    challenge_ref   uuid          NOT NULL,
    kind            text          NOT NULL,
    at              timestamptz   NOT NULL,
    attempt         integer       NOT NULL,
    answer_digest   content_digest,
    event_sequence  bigint        NOT NULL,
    digest          content_digest NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT contact_challenge_event_unique
        UNIQUE (tenant_id, challenge_ref, event_sequence),
    CONSTRAINT contact_challenge_event_challenge_fk
        FOREIGN KEY (tenant_id, challenge_ref)
        REFERENCES contact_verification_challenge (tenant_id, row_id),
    CONSTRAINT contact_challenge_event_kind_allowed
        CHECK (kind IN ('ISSUED', 'ANSWERED', 'VERIFIED', 'EXPIRED', 'EXHAUSTED')),
    CONSTRAINT contact_challenge_event_attempt_non_negative
        CHECK (attempt >= 0),
    CONSTRAINT contact_challenge_event_sequence_positive
        CHECK (event_sequence >= 1)
);

CREATE INDEX contact_endpoint_revision_history
    ON contact_endpoint_revision (tenant_id, endpoint_id, revision DESC);

CREATE INDEX contact_challenge_event_history
    ON contact_challenge_event (tenant_id, challenge_ref, event_sequence);

CREATE TRIGGER contact_challenge_event_append_only
    BEFORE UPDATE OR DELETE ON contact_challenge_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON contact_challenge_event FROM PUBLIC;

-- DB-017: all three tables fail closed unless the transaction has selected a
-- tenant through internal/data/tenancy.WithTenant.
-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'contact_endpoint_revision',
        'contact_verification_challenge',
        'contact_challenge_event'
    ] LOOP
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

GRANT SELECT, INSERT ON contact_endpoint_revision, contact_challenge_event TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON contact_verification_challenge TO hcmnext_app;

-- +goose Down
REVOKE ALL ON contact_challenge_event, contact_verification_challenge, contact_endpoint_revision FROM hcmnext_app;
DROP TABLE contact_challenge_event;
DROP TABLE contact_verification_challenge;
DROP TABLE contact_endpoint_revision;
