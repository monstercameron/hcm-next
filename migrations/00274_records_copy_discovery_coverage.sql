-- Owner: privacy/records metadata. Phase: P1B. RECORDS-COPY-001.
-- A COMPLETE label is insufficient evidence: certification must reconcile the
-- expected discovery sources with independently recorded source watermarks and
-- the source attached to every discovered copy.

-- +goose Up
ALTER TABLE data_copy_inventory
    ADD COLUMN expected_sources jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN source_watermarks jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT data_copy_inventory_expected_sources_array CHECK (jsonb_typeof(expected_sources) = 'array'),
    ADD CONSTRAINT data_copy_inventory_source_watermarks_object CHECK (jsonb_typeof(source_watermarks) = 'object');

ALTER TABLE data_copy
    ADD COLUMN discovery_source text NOT NULL DEFAULT '',
    ADD COLUMN subject_ref text NOT NULL DEFAULT '',
    ADD COLUMN data_category text NOT NULL DEFAULT '',
    ADD COLUMN restore_policy text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE data_copy
    DROP COLUMN restore_policy,
    DROP COLUMN data_category,
    DROP COLUMN subject_ref,
    DROP COLUMN discovery_source;

ALTER TABLE data_copy_inventory
    DROP CONSTRAINT data_copy_inventory_source_watermarks_object,
    DROP CONSTRAINT data_copy_inventory_expected_sources_array,
    DROP COLUMN source_watermarks,
    DROP COLUMN expected_sources;
