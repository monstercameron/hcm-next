-- Owner: product experience. Phase: production frontend.
-- storage-disposition: organization appearance | shared branding | local PostgreSQL | tenant-local ACID/CAS | organization-scoped.

-- +goose Up

-- Migration 00132 initially keyed appearance only by tenant. Preserve any
-- legacy row under an intentionally unaddressable empty scope while making
-- every new/readable appearance record organization-specific. The transport
-- derives the non-empty scope from the authenticated principal.
ALTER TABLE tenant_appearance_preference
    ADD COLUMN IF NOT EXISTS organization_scope_id text NOT NULL DEFAULT '';

ALTER TABLE tenant_appearance_preference
    ALTER COLUMN organization_scope_id DROP DEFAULT;

ALTER TABLE tenant_appearance_preference
    DROP CONSTRAINT IF EXISTS tenant_appearance_preference_pkey;

ALTER TABLE tenant_appearance_preference
    ADD CONSTRAINT tenant_appearance_preference_pkey
    PRIMARY KEY (tenant_id, organization_scope_id);

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'tenant_appearance_preference'::regclass
          AND conname = 'tenant_appearance_organization_scope_nonempty'
    ) THEN
        ALTER TABLE tenant_appearance_preference
            ADD CONSTRAINT tenant_appearance_organization_scope_nonempty
            CHECK (organization_scope_id <> '') NOT VALID;
    END IF;
END $$;
-- +goose StatementEnd

COMMENT ON TABLE tenant_appearance_preference IS
    'Shared appearance keyed by tenant and authenticated organization scope; never by editing principal.';

COMMENT ON TABLE user_presentation_preference IS
    'Personal presentation settings keyed by tenant and authenticated principal.';

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT tenant_id
        FROM tenant_appearance_preference
        GROUP BY tenant_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot collapse organization appearance rows to tenant scope without data loss';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE tenant_appearance_preference
    DROP CONSTRAINT IF EXISTS tenant_appearance_organization_scope_nonempty;

ALTER TABLE tenant_appearance_preference
    DROP CONSTRAINT IF EXISTS tenant_appearance_preference_pkey;

ALTER TABLE tenant_appearance_preference
    DROP COLUMN organization_scope_id;

ALTER TABLE tenant_appearance_preference
    ADD CONSTRAINT tenant_appearance_preference_pkey PRIMARY KEY (tenant_id);
