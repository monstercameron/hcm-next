// Package issuerregistry implements AUTHN-001: the tenant federation issuer
// registry. It is the governed catalog of enterprise identity provider
// issuers a tenant is allowed to federate with -- the record that names,
// for exactly one (tenant, issuer) pair, which audience an assertion must
// carry, which signing algorithms are trusted, where the verification
// material is pinned, how claims map onto the platform principal shape, and
// the clock-skew and metadata-staleness bounds a verifier must enforce.
//
// # Shape: the configregistry pattern, reimplemented for this record
//
// internal/platform/configregistry (CP-001) is this package's structural
// model: an immutable, content-addressed revision ([Issuer]) minted by
// [Publish], and a separate governed lifecycle event ([StateEvent]) that
// [Activate], [Suspend] and [Retire] each append -- never editing or
// deleting an earlier event, so the full transition history is permanent
// evidence. "What is active right now" is answered by [Lookup] reading the
// latest event, exactly as CP-001's own Resolve reads the latest
// [platformconfig.ActivationRecord].
//
// This package does not reuse CP-001's own tables, and that is a deliberate
// disposition, not an oversight. [platformconfig.Kind] is a closed,
// nine-member vocabulary (WORKFLOW, POLICY, SCHEMA, RULE, CONNECTOR, AGENT,
// REFERENCE, MAPPING, CAPABILITY) enforced twice -- once in Go
// ([platformconfig.Kind.Valid]) and once in migrations/00027_config_object.sql's
// CHECK constraint -- and AUTHN-001's file roots do not include
// internal/platform/configregistry, internal/data/configregistry or
// migrations/00027_config_object.sql. Widening that closed vocabulary with
// an ISSUER member, or silently repurposing an existing member (CONNECTOR
// is the closest fit in spirit) to secretly mean "federation issuer",
// either edits a file this package's lane does not own or mislabels a row
// under a Kind that does not describe it -- exactly the kind of drift a
// closed, storage-enforced vocabulary exists to prevent. So this package
// takes the other branch AUTHN-001 offers: migrations/00038_issuer_registry.sql
// mints two dedicated tables (issuer_profile, issuer_state_event) that copy
// migration 00027's own append-only, row-level-security and least-privilege
// pattern verbatim. A follow-up todo that does own
// internal/platform/configregistry may still choose to widen Kind and
// migrate this data onto CP-001's tables later; nothing in this package's
// shape prevents that.
//
// # Distinct approver
//
// [Publish] always records the newly published revision's first lifecycle
// event as DRAFT, attributed to the publisher. [Activate] additionally
// refuses when the activating principal is the same principal who published
// the revision being activated ([ErrSameApprover]) -- publish-then-activate
// is a maker/checker control, not a formality, and this package enforces
// the separation itself rather than trusting a caller to remember it.
//
// # Verification material
//
// [Issuer.JWKS] is never a live discovery URL alone: [Publish] refuses an
// issuer whose material is not pinned, either as an explicit snapshot of
// signing keys copied from the issuer's JWKS at publish time
// ([JWKSSourcePinnedKeys]) or as a reference to a specific
// internal/trust/bundle.Bundle version ([JWKSSourcePinnedBundle]). [Resolver]
// turns an active issuer's pinned material into the
// internal/trust/federation.SigningKey values that package's [KeySource]
// port -- and therefore [federation.Validator] -- consumes; see resolver.go.
//
// # Persistence
//
// [Store] is the persistence port; [MemoryStore] is the in-memory adapter
// this package ships (used by [Resolver] tests and as a development
// double); [PGStore] is the PostgreSQL adapter over migration 00038's
// tables, following internal/data/configregistry's own withTenant /
// row-level-security pattern. [MemoryStore] additionally knows which
// tenants have ever registered a given issuer URL (an in-memory, single
// process, non-authoritative index) so [Lookup] can distinguish an unknown
// issuer from one registered for a different tenant in tests; [PGStore]
// deliberately cannot answer that question at all -- migration 00038's row
// level security scopes every read to exactly one tenant, so a
// least-privilege connection has no way to observe that another tenant's
// issuer even exists, and [Lookup] against [PGStore] reports
// [ErrUnknownIssuer] for both cases. That is not a missing feature: it is
// the same fail-closed, no-cross-tenant-enumeration posture every other
// row-level-security-backed store in this codebase holds.
package issuerregistry
