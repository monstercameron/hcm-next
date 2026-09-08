-- Owner: intent plane. Phase: P1B. LEGAL-014.
-- These columns retain the immutable, trusted receipt-to-proposal binding
-- atomically with the obligation-state projection. They are nullable so
-- proposals that declare no legal obligations remain source compatible.

-- +goose Up
ALTER TABLE intent_instance
    ADD COLUMN legal_evaluation_receipt_ref text,
    ADD COLUMN legal_evaluation_receipt_digest text,
    ADD COLUMN legal_evaluation_binding_digest text,
    ADD COLUMN legal_evaluation_proposal_revision_id text,
    ADD COLUMN legal_evaluation_material_digest text,
    ADD COLUMN legal_applied_obligations jsonb,
    ADD COLUMN legal_obligation_discharges jsonb;

-- +goose Down
ALTER TABLE intent_instance
    DROP COLUMN legal_obligation_discharges,
    DROP COLUMN legal_applied_obligations,
    DROP COLUMN legal_evaluation_material_digest,
    DROP COLUMN legal_evaluation_proposal_revision_id,
    DROP COLUMN legal_evaluation_binding_digest,
    DROP COLUMN legal_evaluation_receipt_digest,
    DROP COLUMN legal_evaluation_receipt_ref;
