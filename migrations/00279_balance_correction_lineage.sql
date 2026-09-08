-- +goose Up
-- BAL-006: correction entries retain immutable parent lineage.  The digest is
-- optional for legacy entries and is only populated by correction postings.
-- numeric without a typmod preserves the kernel's full bounded decimal
-- precision. Existing numeric(19,4) values retain their exact numeric value.
ALTER TABLE balance_entry ALTER COLUMN amount TYPE numeric USING amount::numeric;

ALTER TABLE balance_entry
    ADD COLUMN supersedes_digest text,
    ADD COLUMN supersedes_row_id uuid,
    ADD COLUMN amount_exact text,
    ADD COLUMN amount_scale integer,
    ADD COLUMN amount_rounding text,
    ADD COLUMN effective_at_submicro integer,
    ADD COLUMN authorized_at_submicro integer;

ALTER TABLE balance_entry
    ADD CONSTRAINT balance_entry_exact_amount_check
    CHECK ((amount_exact IS NULL AND amount_scale IS NULL AND amount_rounding IS NULL) OR
           (amount_exact IS NOT NULL AND amount_scale IS NOT NULL AND amount_scale BETWEEN 0 AND 18 AND
            amount_rounding IS NOT NULL AND amount_rounding IN ('HALF_EVEN','HALF_UP','HALF_AWAY_FROM_ZERO','FLOOR','CEILING','TOWARD_ZERO','AWAY_FROM_ZERO','EXACT_REQUIRED'))),
    ADD CONSTRAINT balance_entry_effective_submicro_check
    CHECK ((amount_exact IS NULL AND effective_at_submicro IS NULL) OR
           (amount_exact IS NOT NULL AND
           ((effective_at IS NULL AND effective_at_submicro IS NULL) OR
            (effective_at IS NOT NULL AND effective_at_submicro IS NOT NULL AND effective_at_submicro BETWEEN 0 AND 999)))),
    ADD CONSTRAINT balance_entry_authorized_submicro_check
    CHECK ((amount_exact IS NULL AND authorized_at_submicro IS NULL) OR
           (amount_exact IS NOT NULL AND
           ((authorized_at IS NULL AND authorized_at_submicro IS NULL) OR
            (authorized_at IS NOT NULL AND authorized_at_submicro IS NOT NULL AND authorized_at_submicro BETWEEN 0 AND 999))));

ALTER TABLE balance_entry
    ADD CONSTRAINT balance_entry_correction_digest_check
    CHECK ((supersedes_digest IS NULL AND supersedes_row_id IS NULL) OR
           (supersedes_digest IS NOT NULL AND supersedes_row_id IS NOT NULL AND entry_type = 'CORRECTION' AND
            length(supersedes_digest) > 0 AND amount_exact IS NOT NULL));

CREATE UNIQUE INDEX balance_entry_one_correction_per_parent
    ON balance_entry (tenant_id, supersedes_row_id)
    WHERE supersedes_row_id IS NOT NULL;

ALTER TABLE balance_entry
    ADD CONSTRAINT balance_entry_correction_parent_fk
    FOREIGN KEY (tenant_id, supersedes_row_id)
    REFERENCES balance_entry (tenant_id, row_id);

-- A correction may only target an entry in the same tenant, account and
-- definition revision. The adapter resolves the asserted digest to this row;
-- the baseline table has no stored digest for SQL to independently recompute.
-- This trigger therefore protects row scope, while digest verification remains
-- an adapter responsibility.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION balance_entry_validate_correction_parent()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.supersedes_row_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF EXISTS (
        SELECT 1 FROM balance_entry p
        WHERE p.tenant_id = NEW.tenant_id
          AND p.row_id = NEW.supersedes_row_id
          AND p.account_id = NEW.account_id
          AND p.definition_id = NEW.definition_id
          AND p.definition_version = NEW.definition_version
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'correction parent digest is not present in the same account and definition';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER balance_entry_correction_parent
    BEFORE INSERT ON balance_entry
    FOR EACH ROW EXECUTE FUNCTION balance_entry_validate_correction_parent();

-- +goose Down
-- Lineage is historical evidence and must not be silently discarded.
-- This migration is intentionally irreversible.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'BAL-006 correction lineage migration is irreversible'; END $$;
-- +goose StatementEnd
