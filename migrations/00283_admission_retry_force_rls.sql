-- RLS hardening: 00280 enabled row-level security and created the
-- tenant_isolation policies on the admission retry tables but did not FORCE
-- them, so table owners bypass the policy (rlsparity FORCE_RLS_MISSING on
-- admission_retry_budget and admission_retry_receipt). Force both tables to
-- match the tenant-isolation convention (00008_tenant_isolation.sql). No data
-- or key shape changes; existing policies are untouched.

-- +goose Up
ALTER TABLE admission_retry_budget FORCE ROW LEVEL SECURITY;
ALTER TABLE admission_retry_receipt FORCE ROW LEVEL SECURITY;

-- +goose Down
-- The retry tables are durable evidence once written; the RLS hardening is
-- not rolled back. This migration is intentionally irreversible: the
-- admissionstore evidence wall requires every migration above the newest
-- reversible version to refuse Down with P0001.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00283 is irreversible: admission retry RLS hardening is durable evidence'; END $$;
-- +goose StatementEnd
