-- Owner: intentcontrol. Additive typed replay metadata for proposal writes.
-- Legacy rows remain NULL and therefore uncertified for fenced execution.
-- +goose Up
ALTER TABLE proposal_write_item
    ADD COLUMN authority_domain text,
    ADD COLUMN operation text,
    ADD COLUMN effective_interval_kind text,
    ADD COLUMN effective_interval_start text,
    ADD COLUMN effective_interval_end text,
    ADD COLUMN effective_interval_calendar_ref text,
    ADD COLUMN effective_interval_calendar_version text,
    ADD COLUMN effective_interval_zone_id text,
    ADD COLUMN effective_interval_tzdb_version text,
    ADD COLUMN effective_interval_disambiguation text;

ALTER TABLE proposal_write_item
    ADD CONSTRAINT proposal_write_item_operation_closed
        CHECK (operation IS NULL OR operation IN ('CREATE','UPDATE','DELETE','UPSERT')),
    ADD CONSTRAINT proposal_write_item_interval_closed
        CHECK (effective_interval_kind IS NULL OR effective_interval_kind IN ('LOCAL_DATE','INSTANT')),
    ADD CONSTRAINT proposal_write_item_semantics_complete
        CHECK ((operation IS NULL AND authority_domain IS NULL AND effective_interval_kind IS NULL AND effective_interval_start IS NULL
                AND effective_interval_end IS NULL AND effective_interval_calendar_ref IS NULL
                AND effective_interval_calendar_version IS NULL AND effective_interval_zone_id IS NULL
                AND effective_interval_tzdb_version IS NULL AND effective_interval_disambiguation IS NULL)
            OR (operation IS NOT NULL AND authority_domain IS NOT NULL AND authority_domain <> ''
                AND effective_interval_kind IS NOT NULL AND effective_interval_start IS NOT NULL AND effective_interval_start <> ''
                AND ((effective_interval_kind = 'LOCAL_DATE'
                        AND effective_interval_calendar_ref IS NOT NULL AND effective_interval_calendar_ref <> ''
                        AND effective_interval_calendar_version IS NOT NULL AND effective_interval_calendar_version <> ''
                        AND ((effective_interval_zone_id IS NULL AND effective_interval_tzdb_version IS NULL AND effective_interval_disambiguation IS NULL)
                            OR (effective_interval_zone_id IS NOT NULL AND effective_interval_zone_id <> ''
                                AND effective_interval_tzdb_version IS NOT NULL AND effective_interval_tzdb_version <> ''
                                AND effective_interval_disambiguation IS NOT NULL
                                AND effective_interval_disambiguation IN ('REJECT_GAP','EARLIER','LATER','EXPLICIT_OFFSET'))))
                    OR (effective_interval_kind = 'INSTANT'
                        AND effective_interval_calendar_ref IS NULL AND effective_interval_calendar_version IS NULL
                        AND effective_interval_zone_id IS NULL AND effective_interval_tzdb_version IS NULL
                        AND effective_interval_disambiguation IS NULL))));

-- +goose Down
ALTER TABLE proposal_write_item
    DROP CONSTRAINT proposal_write_item_semantics_complete,
    DROP CONSTRAINT proposal_write_item_interval_closed,
    DROP CONSTRAINT proposal_write_item_operation_closed,
    DROP COLUMN authority_domain,
    DROP COLUMN operation,
    DROP COLUMN effective_interval_kind,
    DROP COLUMN effective_interval_start,
    DROP COLUMN effective_interval_end,
    DROP COLUMN effective_interval_calendar_ref,
    DROP COLUMN effective_interval_calendar_version,
    DROP COLUMN effective_interval_zone_id,
    DROP COLUMN effective_interval_tzdb_version,
    DROP COLUMN effective_interval_disambiguation;
