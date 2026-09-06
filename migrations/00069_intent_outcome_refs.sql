-- Owner: intent plane (INTENT-007 follow-up). Phase: P1B.
-- The terminal tuple BindOutcome projects onto intent_instance is only legal
-- with the references the lifecycle rules demand: a COMMITTED execution needs
-- the commit receipt it produced and a REPAIR_REQUIRED execution needs its
-- RepairPlan or incident. Without these columns a committed intent could not
-- be re-validated on read (rule committed-requires-receipt). Both are nullable:
-- they stay NULL until a terminal is bound.

-- +goose Up
ALTER TABLE intent_instance
    ADD COLUMN commit_receipt_ref text,
    ADD COLUMN repair_ref text;

-- +goose Down
ALTER TABLE intent_instance
    DROP COLUMN repair_ref,
    DROP COLUMN commit_receipt_ref;
