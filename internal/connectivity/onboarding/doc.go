// Package onboarding is the P1A tenant-onboarding control surface: it turns a
// signed intent to observe one tenant's incumbent HRIS into a resumable,
// budget-bounded, identity-adjudicated extraction of that source, without
// ever writing to the incumbent or to an authoritative table.
//
// Semantic owner: connectivity. Phase: P1A.
//
// # What this package owns
//
//   - [OnboardingManifest] and [SignedManifest]: the immutable, ed25519-signed
//     statement of what an onboarding job may read, from whom, under which
//     connector version, field allow-list, schema-version pins, reference and
//     crosswalk snapshot pins, and resource budget (ONBOARD-001).
//   - [Preflight]: the gate that checks a signed manifest against a live
//     connection and connector - state, capability, schema version, budget,
//     signature and authority - and refuses to let extraction start on any
//     mismatch (ONBOARD-001).
//   - [Extractor]: a manifest-scoped, resumable snapshot extraction built on
//     top of [observe.Runner]'s checkpointed page loop, adding a proactive
//     source-consistency check so a source that changed underneath a stored
//     checkpoint is refused before a wasted read (ONBOARD-002).
//   - [Adjudicator]: identity resolution of external ids to canonical ones
//     through a pinned crosswalk snapshot, with four honest outcomes - EXACT,
//     CANDIDATE, UNMATCHED (operator language: UNRESOLVED) and CONFLICT
//     (operator language: AMBIGUOUS) - and never a guess (ONBOARD-003).
//   - [BudgetGuard] and [FieldFilterConnector]: [connectivity.Connector]
//     decorators that enforce a whole-job resource budget (pages, records,
//     bytes, wall time from an injected clock) and isolate malformed records
//     into a quarantine log rather than failing the page they arrived in
//     (ONBOARD-004).
//
// # Read-only by construction
//
// Every type here composes on top of [connectivity.Connector], which has no
// method that could mutate an external system. This package adds no new
// external surface of its own: it only decorates, filters, checkpoints and
// adjudicates what the connectivity plane already observes. Nothing here
// imports internal/data, internal/intent/app or internal/operations, so
// nothing here can persist an authoritative row, publish a business event or
// enqueue a provider request - nor, per P1A's remit, is that its job. Staging
// and cutover are ONBOARD-005 and ONBOARD-006, a later gate.
package onboarding
