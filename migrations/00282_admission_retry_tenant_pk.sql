-- DATA-001 corrective: 00280 declared admission_retry_budget's primary key
-- as (budget_id) alone, gave admission_retry_receipt a single-column
-- foreign key, and left admission_retry_receipt's own primary key as
-- (receipt_id) alone. Tenant-scoped tables must join on tenant_id
-- (internal/data/schema's primary_keys_are_tenant_scoped and
-- no_foreign_key_crosses_a_tenant_or_schema_boundary subtests), so both
-- primary keys become tenant-scoped and the single-column foreign key goes
-- away; the composite (tenant_id, budget_id) foreign key stays and now
-- references the budget primary key instead of its UNIQUE twin.

-- +goose Up
ALTER TABLE admission_retry_receipt DROP CONSTRAINT admission_retry_receipt_budget_id_fkey;
ALTER TABLE admission_retry_receipt DROP CONSTRAINT admission_retry_receipt_tenant_id_budget_id_fkey;
ALTER TABLE admission_retry_budget DROP CONSTRAINT admission_retry_budget_pkey;
ALTER TABLE admission_retry_budget DROP CONSTRAINT admission_retry_budget_tenant_id_budget_id_key;
ALTER TABLE admission_retry_budget ADD PRIMARY KEY (tenant_id, budget_id);
ALTER TABLE admission_retry_receipt DROP CONSTRAINT admission_retry_receipt_pkey;
ALTER TABLE admission_retry_receipt ADD PRIMARY KEY (tenant_id, receipt_id);
ALTER TABLE admission_retry_receipt ADD FOREIGN KEY (tenant_id, budget_id) REFERENCES admission_retry_budget (tenant_id, budget_id);

-- +goose Down
-- The budget tables are durable evidence once written; the key shape is not
-- rolled back. This migration is intentionally irreversible.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00282 is irreversible: admission retry key shape is durable evidence'; END $$;
-- +goose StatementEnd
