-- Owner: data plane. Phase: Gate B.
-- PERSIST-TAXPROFILE-001: durable, tenant-isolated tax-profile revisions.
--
-- The worker_ref column is deliberately a soft reference. The people identity
-- spine uses (tenant_id, entity_id) and is not the same identity as this
-- domain's worker reference, so adding a foreign key here would conflate two
-- authority boundaries. All four tables are immutable revisions; replacement
-- is represented by another row and, for the chained tables, its parent.
--
-- Storage disposition: all four tables are AGGREGATE, tenant-scoped by
-- tenant_id, append-only at the SQL boundary, and retained permanently.

-- +goose Up

CREATE TABLE IF NOT EXISTS worker_tax_profile_revision (
    tenant_id                  tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id                     uuid NOT NULL,
    worker_ref                 uuid NOT NULL,
    revision                   bigint NOT NULL,
    parent_revision            bigint,
    parent_digest              content_digest,
    residence_jurisdictions    jsonb,
    work_jurisdictions         jsonb,
    filing_status              text,
    classification              text NOT NULL,
    classification_evidence_ref text,
    effective_from             timestamptz,
    known_at                   timestamptz NOT NULL,
    canonical_digest            content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT worker_tax_profile_revision_unique
        UNIQUE (tenant_id, worker_ref, revision),
    CONSTRAINT worker_tax_profile_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT worker_tax_profile_revision_parent_valid
        CHECK ((revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
            OR (revision > 1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL
                AND parent_revision < revision))
);

CREATE TABLE IF NOT EXISTS tax_registration_revision (
    tenant_id           tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id              uuid NOT NULL,
    registration_id     text NOT NULL,
    jurisdiction        text NOT NULL,
    authority_ref       text,
    revision            bigint NOT NULL,
    parent_revision     bigint,
    parent_digest       content_digest,
    effective_from      timestamptz,
    known_at            timestamptz NOT NULL,
    raw_registration_id text,
    canonical_digest    content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT tax_registration_revision_unique
        UNIQUE (tenant_id, registration_id, revision),
    CONSTRAINT tax_registration_revision_positive
        CHECK (revision >= 1),
    CONSTRAINT tax_registration_revision_parent_valid
        CHECK ((revision = 1 AND parent_revision IS NULL AND parent_digest IS NULL)
            OR (revision > 1 AND parent_revision IS NOT NULL AND parent_digest IS NOT NULL
                AND parent_revision < revision)),
    CONSTRAINT tax_registration_revision_raw_absent
        CHECK (raw_registration_id IS NULL)
);

CREATE TABLE IF NOT EXISTS withholding_election_revision (
    tenant_id         tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id            uuid NOT NULL,
    election_id       text NOT NULL,
    worker_ref        uuid NOT NULL,
    jurisdiction      text NOT NULL,
    kind              text NOT NULL,
    form_revision_ref text,
    evidence_ref      text,
    amount            numeric(19,4),
    effective_from    timestamptz,
    known_at          timestamptz NOT NULL,
    canonical_digest  content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT withholding_election_revision_unique
        UNIQUE (tenant_id, election_id, effective_from)
);

CREATE TABLE IF NOT EXISTS tax_exemption_revision (
    tenant_id        tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id           uuid NOT NULL,
    exemption_id     text NOT NULL,
    worker_ref       uuid NOT NULL,
    jurisdiction     text NOT NULL,
    kind             text NOT NULL,
    evidence_refs    jsonb,
    expires_at       timestamptz,
    effective_from   timestamptz,
    known_at         timestamptz NOT NULL,
    canonical_digest content_digest NOT NULL,

    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT tax_exemption_revision_unique
        UNIQUE (tenant_id, exemption_id, effective_from)
);

CREATE OR REPLACE TRIGGER worker_tax_profile_revision_append_only
    BEFORE UPDATE OR DELETE ON worker_tax_profile_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER tax_registration_revision_append_only
    BEFORE UPDATE OR DELETE ON tax_registration_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER withholding_election_revision_append_only
    BEFORE UPDATE OR DELETE ON withholding_election_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

CREATE OR REPLACE TRIGGER tax_exemption_revision_append_only
    BEFORE UPDATE OR DELETE ON tax_exemption_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON
    worker_tax_profile_revision,
    tax_registration_revision,
    withholding_election_revision,
    tax_exemption_revision
FROM PUBLIC;

-- +goose StatementBegin
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'worker_tax_profile_revision',
        'tax_registration_revision',
        'withholding_election_revision',
        'tax_exemption_revision'
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

GRANT SELECT, INSERT ON
    worker_tax_profile_revision,
    tax_registration_revision,
    withholding_election_revision,
    tax_exemption_revision
TO hcmnext_app;

-- +goose Down

REVOKE ALL ON
    tax_exemption_revision,
    withholding_election_revision,
    tax_registration_revision,
    worker_tax_profile_revision
FROM hcmnext_app;

DROP TABLE tax_exemption_revision;
DROP TABLE withholding_election_revision;
DROP TABLE tax_registration_revision;
DROP TABLE worker_tax_profile_revision;
