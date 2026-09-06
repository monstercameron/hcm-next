-- Owner: data plane. Phase: P1A. EVENT-001.
-- Partial indexes keep due work and expired leases bounded without making
-- delivered history part of the hot claim path.

-- +goose Up

CREATE INDEX outbox_claim_pending
    ON outbox (tenant_id, criticality, available_at, ordering_key, created_at, outbox_id)
    WHERE status = 'PENDING';

CREATE INDEX outbox_claim_expired
    ON outbox (tenant_id, criticality, lease_until, ordering_key, created_at, outbox_id)
    WHERE status = 'IN_FLIGHT';

-- +goose Down

DROP INDEX outbox_claim_expired;
DROP INDEX outbox_claim_pending;
