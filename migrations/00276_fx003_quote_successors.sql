-- Owner: FX domain/data lane. FX-003 append-only quote correction lineage.
-- +goose Up
ALTER TABLE fx_quote_revision
    ADD COLUMN IF NOT EXISTS quote_revision cas_version NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS parent_quote_id text,
    ADD COLUMN IF NOT EXISTS parent_digest content_digest,
    ADD COLUMN IF NOT EXISTS base_currency text,
    ADD COLUMN IF NOT EXISTS quote_currency text,
    ADD COLUMN IF NOT EXISTS rate text,
    ADD COLUMN IF NOT EXISTS rate_scale smallint,
    ADD COLUMN IF NOT EXISTS market_convention text,
    ADD COLUMN IF NOT EXISTS confidence text,
    ADD COLUMN IF NOT EXISTS as_of_submicrosecond smallint,
    ADD COLUMN IF NOT EXISTS known_at_submicrosecond smallint;

ALTER TABLE fx_quote_revision
    ADD CONSTRAINT fx_quote_revision_parent_pair
        CHECK ((parent_quote_id IS NULL) = (parent_digest IS NULL)),
    ADD CONSTRAINT fx_quote_revision_parent_order
        CHECK (parent_quote_id IS NULL OR quote_revision > 1);

CREATE UNIQUE INDEX IF NOT EXISTS fx_quote_revision_one_successor
    ON fx_quote_revision (tenant_id, parent_quote_id)
    WHERE parent_quote_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS fx_quote_revision_lineage_target
    ON fx_quote_revision (tenant_id, quote_id, canonical_digest);

ALTER TABLE fx_quote_revision
    ADD CONSTRAINT fx_quote_revision_parent_fk
        FOREIGN KEY (tenant_id, parent_quote_id, parent_digest)
        REFERENCES fx_quote_revision (tenant_id, quote_id, canonical_digest);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_fx_quote_successor_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    predecessor_revision cas_version;
BEGIN
    IF NEW.parent_quote_id IS NULL THEN
        IF NEW.quote_revision <> 1 THEN
            RAISE EXCEPTION 'initial FX quote revision must be 1';
        END IF;
        RETURN NEW;
    END IF;
    SELECT quote_revision INTO predecessor_revision
      FROM fx_quote_revision
     WHERE tenant_id = NEW.tenant_id
       AND quote_id = NEW.parent_quote_id
       AND canonical_digest = NEW.parent_digest;
    IF predecessor_revision IS NULL OR NEW.quote_revision <> predecessor_revision + 1 THEN
        RAISE EXCEPTION 'FX quote successor revision is not sequential';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER fx_quote_revision_successor_revision
    BEFORE INSERT ON fx_quote_revision
    FOR EACH ROW EXECUTE FUNCTION enforce_fx_quote_successor_revision();

-- +goose Down
DROP TRIGGER IF EXISTS fx_quote_revision_successor_revision ON fx_quote_revision;
DROP FUNCTION IF EXISTS enforce_fx_quote_successor_revision();
ALTER TABLE fx_quote_revision DROP CONSTRAINT IF EXISTS fx_quote_revision_parent_fk;
DROP INDEX IF EXISTS fx_quote_revision_lineage_target;
DROP INDEX IF EXISTS fx_quote_revision_one_successor;
ALTER TABLE fx_quote_revision
    DROP CONSTRAINT IF EXISTS fx_quote_revision_parent_order,
    DROP CONSTRAINT IF EXISTS fx_quote_revision_parent_pair,
    DROP COLUMN IF EXISTS parent_digest,
    DROP COLUMN IF EXISTS parent_quote_id,
    DROP COLUMN IF EXISTS quote_revision,
    DROP COLUMN IF EXISTS base_currency,
    DROP COLUMN IF EXISTS quote_currency,
    DROP COLUMN IF EXISTS rate,
    DROP COLUMN IF EXISTS rate_scale,
    DROP COLUMN IF EXISTS market_convention,
    DROP COLUMN IF EXISTS confidence,
    DROP COLUMN IF EXISTS as_of_submicrosecond,
    DROP COLUMN IF EXISTS known_at_submicrosecond;
