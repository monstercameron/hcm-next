-- Owner: data plane. Phase: P1A. EVENT-001.
-- The mutable lease is fenced by a monotonically increasing version and an
-- opaque token. A stale worker can therefore never acknowledge a reclaimed
-- row, while the outbox remains at-least-once rather than exactly-once.

-- +goose Up

ALTER TABLE outbox
    ADD CONSTRAINT outbox_in_flight_claim_complete
        CHECK (
            status <> 'IN_FLIGHT'
            OR (lease_token IS NOT NULL AND lease_until IS NOT NULL AND lease_version > 0)
        ),
    ADD CONSTRAINT outbox_lease_until_after_update
        CHECK (lease_until IS NULL OR lease_until > updated_at);

COMMENT ON COLUMN outbox.effect_identity IS
    'Stable logical operation identity; duplicate enqueue requests must carry the same immutable message.';
COMMENT ON COLUMN outbox.lease_version IS
    'Monotonic claim fence. Every successful claim increments it before delivery.';

-- +goose Down

COMMENT ON COLUMN outbox.lease_version IS NULL;
COMMENT ON COLUMN outbox.effect_identity IS NULL;

ALTER TABLE outbox
    DROP CONSTRAINT outbox_lease_until_after_update,
    DROP CONSTRAINT outbox_in_flight_claim_complete;
