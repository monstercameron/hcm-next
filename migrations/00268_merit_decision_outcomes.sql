-- +goose Up

ALTER TABLE merit_recommendation
    DROP CONSTRAINT merit_recommendation_state_allowed;

ALTER TABLE merit_recommendation
    ADD CONSTRAINT merit_recommendation_state_allowed CHECK (
        state IN ('PROPOSED', 'ADJUSTED', 'APPROVED', 'REJECTED', 'FINALIZED')
    ),
    ADD COLUMN decision_evidence jsonb NOT NULL DEFAULT '{"version":1}'::jsonb;

ALTER TABLE merit_population_snapshot
    ADD COLUMN frozen_at_ns_remainder smallint;

ALTER TABLE merit_cycle_revision
    ADD COLUMN effective_at_ns_remainder smallint,
    ADD COLUMN known_at_ns_remainder smallint;

-- +goose Down

ALTER TABLE merit_recommendation
    DROP CONSTRAINT merit_recommendation_state_allowed,
    DROP COLUMN decision_evidence;

ALTER TABLE merit_cycle_revision
    DROP COLUMN effective_at_ns_remainder,
    DROP COLUMN known_at_ns_remainder;

ALTER TABLE merit_population_snapshot
    DROP COLUMN frozen_at_ns_remainder;

ALTER TABLE merit_recommendation
    ADD CONSTRAINT merit_recommendation_state_allowed CHECK (
        state IN ('PROPOSED', 'ADJUSTED', 'APPROVED', 'FINALIZED')
    );
