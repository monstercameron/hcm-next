-- Owner: data plane. DATA-008 transactional-outbox lease fencing.
-- The outbox row identity remains the stable logical delivery identity. These
-- columns add a per-claim fence so an old worker cannot acknowledge a lease
-- reclaimed by a newer worker after a crash.

-- +goose Up

ALTER TABLE outbox
    ADD COLUMN lease_token uuid,
    ADD COLUMN lease_until timestamptz;

ALTER TABLE outbox
    ADD CONSTRAINT outbox_in_flight_has_lease CHECK (
        (status = 'IN_FLIGHT') = (lease_token IS NOT NULL AND lease_until IS NOT NULL)
    );

CREATE INDEX outbox_lease_reclaim
    ON outbox (tenant_id, status, lease_until, ordering_key);

-- +goose Down

DROP INDEX outbox_lease_reclaim;
ALTER TABLE outbox DROP CONSTRAINT outbox_in_flight_has_lease;
ALTER TABLE outbox DROP COLUMN lease_until;
ALTER TABLE outbox DROP COLUMN lease_token;
