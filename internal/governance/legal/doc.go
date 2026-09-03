// Package legal resolves an explicit, signed [LegalContext] and evaluates a
// versioned [RulePack] against a proposed change.
//
// Owner: governance-and-trust. Ticket: LEGAL-001. Phase: P1B, reduced depth
// per the 2026-09-02 disposition in planning/todos.md — one versioned rule
// pack of customer-configured thresholds and notice obligations, evaluated to
// a [LegalEvaluationStatus] and bound through an [ObligationBinding].
// Jurisdiction resolution across the full JurisdictionAssertion/
// JurisdictionResolution graph, calculation engines, and filings are Phase 2
// or later; this package intentionally does not build that machinery.
//
// # Boundary
//
// This package resolves a jurisdiction only from explicit, structured facts:
// the worker's work location, the employer's legal entity, and a declared
// remote-work rule. A locale hint (language/region for presentation) is never
// sufficient on its own and is never consulted for jurisdiction — see
// [LegalContextInput.Locale]. When those facts do not resolve to one
// jurisdiction with at least one applicable, registered [RulePack] release,
// [Resolve] refuses with [ErrLegalContextUnknown] rather than defaulting
// silently to any jurisdiction.
//
// A resolved [LegalContext] is immutable and carries a digest and an ed25519
// signature over its own canonical encoding, so that a downstream simulation
// can later prove exactly which law set (jurisdiction, legal entity, rule
// pack releases, and effective/known times) it evaluated a proposal under.
// The digest and signature are computed and verified inside this package;
// they do not depend on internal/kernel/digest's Protobuf-canonicalization
// registry, which requires a schema this package is not permitted to define.
//
// # Rule packs are fixtures, not legal advice
//
// [CaliforniaPromotionPack] and [NewYorkPromotionPack] are seed packs drawn
// from planning/research/state-employment-law/california.md and
// new-york.md. Every rule they carry records that source file, the statutory
// section, and [ReviewStatusUnreviewed]: an agent drafted the research, no
// counsel has approved it, and nothing in this package may be relied on as
// legal advice. They exist to prove the [RulePack] skeleton shape that later,
// counsel-reviewed state packs will populate.
package legal
