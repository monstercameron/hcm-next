-- +goose Up
-- FX-002: persist the complete conversion policy. Legacy rows remain
-- explicitly incomplete; the adapter must refuse to reconstruct them.
ALTER TABLE fx_conversion_profile_revision
    ADD COLUMN IF NOT EXISTS parent_digest content_digest,
    ADD COLUMN IF NOT EXISTS rounding_scale integer,
    ADD COLUMN IF NOT EXISTS rounding_mode text,
    ADD COLUMN IF NOT EXISTS tolerance_nanoseconds bigint,
    ADD COLUMN IF NOT EXISTS fallback_source_order text[],
    ADD COLUMN IF NOT EXISTS triangulation_currencies text[],
    ADD COLUMN IF NOT EXISTS effective_from timestamptz,
    ADD COLUMN IF NOT EXISTS effective_to timestamptz,
    ADD COLUMN IF NOT EXISTS effective_from_submicrosecond smallint,
    ADD COLUMN IF NOT EXISTS effective_to_submicrosecond smallint;

ALTER TABLE fx_conversion_profile_revision
    ADD CONSTRAINT fx_conversion_profile_revision_parent_digest_pair
        CHECK ((parent_revision IS NULL) = (parent_digest IS NULL)) NOT VALID,
    ADD CONSTRAINT fx_conversion_profile_revision_policy_scale
        CHECK (rounding_scale IS NULL OR rounding_scale BETWEEN 0 AND 18),
    ADD CONSTRAINT fx_conversion_profile_revision_policy_tolerance
        CHECK (tolerance_nanoseconds IS NULL OR tolerance_nanoseconds > 0),
    ADD CONSTRAINT fx_conversion_profile_revision_policy_effective
        CHECK (effective_to IS NULL OR effective_from IS NULL
            OR effective_to > effective_from
            OR (effective_to = effective_from
                AND COALESCE(effective_to_submicrosecond, 0) > COALESCE(effective_from_submicrosecond, 0))),
    ADD CONSTRAINT fx_conversion_profile_revision_policy_submicrosecond
        CHECK ((effective_from_submicrosecond IS NULL OR effective_from_submicrosecond BETWEEN 0 AND 999)
           AND (effective_to_submicrosecond IS NULL OR effective_to_submicrosecond BETWEEN 0 AND 999));

-- +goose Down
ALTER TABLE fx_conversion_profile_revision
    DROP CONSTRAINT IF EXISTS fx_conversion_profile_revision_policy_effective,
    DROP CONSTRAINT IF EXISTS fx_conversion_profile_revision_policy_submicrosecond,
    DROP CONSTRAINT IF EXISTS fx_conversion_profile_revision_policy_tolerance,
    DROP CONSTRAINT IF EXISTS fx_conversion_profile_revision_policy_scale,
    DROP CONSTRAINT IF EXISTS fx_conversion_profile_revision_parent_digest_pair,
    DROP COLUMN IF EXISTS effective_to,
    DROP COLUMN IF EXISTS effective_from,
    DROP COLUMN IF EXISTS effective_to_submicrosecond,
    DROP COLUMN IF EXISTS effective_from_submicrosecond,
    DROP COLUMN IF EXISTS triangulation_currencies,
    DROP COLUMN IF EXISTS fallback_source_order,
    DROP COLUMN IF EXISTS tolerance_nanoseconds,
    DROP COLUMN IF EXISTS rounding_mode,
    DROP COLUMN IF EXISTS rounding_scale,
    DROP COLUMN IF EXISTS parent_digest;
