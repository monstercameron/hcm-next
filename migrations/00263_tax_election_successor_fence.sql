-- Owner: taxprofile data adapter. TAXPROFILE-002 election successor fencing.
-- +goose Up

ALTER TABLE withholding_election_revision
    ADD COLUMN IF NOT EXISTS effective_to timestamptz,
    ADD COLUMN IF NOT EXISTS amount_scale smallint,
    ADD COLUMN IF NOT EXISTS effective_from_ns_remainder smallint,
    ADD COLUMN IF NOT EXISTS effective_to_ns_remainder smallint,
    ADD COLUMN IF NOT EXISTS known_at_ns_remainder smallint;

ALTER TABLE withholding_election_revision
    ALTER COLUMN amount TYPE numeric;

ALTER TABLE withholding_election_revision
    ADD CONSTRAINT withholding_election_revision_effective_bounds
        CHECK (effective_to IS NULL
            OR effective_to > effective_from
            OR (effective_to = effective_from
                AND COALESCE(effective_to_ns_remainder, 0) > COALESCE(effective_from_ns_remainder, 0))),
    ADD CONSTRAINT withholding_election_revision_amount_scale
        CHECK (amount_scale IS NULL
            OR (amount IS NOT NULL AND amount_scale BETWEEN 0 AND 18)),
    ADD CONSTRAINT withholding_election_revision_ns_remainders
        CHECK ((effective_from_ns_remainder IS NULL OR effective_from_ns_remainder BETWEEN 0 AND 999)
            AND (effective_to_ns_remainder IS NULL OR effective_to_ns_remainder BETWEEN 0 AND 999)
            AND (known_at_ns_remainder IS NULL OR known_at_ns_remainder BETWEEN 0 AND 999));

CREATE TABLE IF NOT EXISTS withholding_election_head (
    tenant_id      tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    election_id    text NOT NULL,
    current_digest content_digest NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, election_id)
);

INSERT INTO withholding_election_head (tenant_id, election_id, current_digest)
SELECT DISTINCT ON (tenant_id, election_id) tenant_id, election_id, canonical_digest
FROM withholding_election_revision
ORDER BY tenant_id, election_id, known_at DESC, row_id DESC, effective_from DESC
ON CONFLICT (tenant_id, election_id) DO NOTHING;

ALTER TABLE withholding_election_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE withholding_election_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON withholding_election_head
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON withholding_election_head TO hcmnext_app;

-- +goose Down
REVOKE ALL ON withholding_election_head FROM hcmnext_app;
DROP TABLE withholding_election_head;
ALTER TABLE withholding_election_revision DROP COLUMN IF EXISTS effective_to;
ALTER TABLE withholding_election_revision DROP COLUMN IF EXISTS amount_scale;
ALTER TABLE withholding_election_revision DROP COLUMN IF EXISTS effective_from_ns_remainder;
ALTER TABLE withholding_election_revision DROP COLUMN IF EXISTS effective_to_ns_remainder;
ALTER TABLE withholding_election_revision DROP COLUMN IF EXISTS known_at_ns_remainder;
