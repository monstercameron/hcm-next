-- Owner: data plane. Phase: P1A. EVENT-001.
-- Extend the original transactional outbox with the state required by a
-- tenant-scoped, priority-aware claim protocol. The logical outbox identity
-- remains (tenant_id, effect_identity); these fields only describe delivery.

-- +goose Up

ALTER TABLE outbox
    ADD COLUMN criticality text NOT NULL DEFAULT 'P2',
    ADD COLUMN lease_version bigint NOT NULL DEFAULT 0;

ALTER TABLE outbox
    ADD CONSTRAINT outbox_criticality_allowed
        CHECK (criticality IN ('P0', 'P1', 'P2', 'P3', 'P4')),
    ADD CONSTRAINT outbox_lease_version_non_negative
        CHECK (lease_version >= 0),
    ADD CONSTRAINT outbox_lease_pair_consistent
        CHECK ((lease_token IS NULL) = (lease_until IS NULL));

-- +goose Down

ALTER TABLE outbox
    DROP CONSTRAINT outbox_lease_pair_consistent,
    DROP CONSTRAINT outbox_lease_version_non_negative,
    DROP CONSTRAINT outbox_criticality_allowed;

ALTER TABLE outbox
    DROP COLUMN lease_version,
    DROP COLUMN criticality;
