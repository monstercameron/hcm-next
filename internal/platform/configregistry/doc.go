// Package configregistry implements the CP-001 immutable
// configuration-object registry (owner: DataOps/control-plane; phase: P1B
// minimal depth per planning/todos.md section 30's disposition note).
//
// # Boundary
//
// planning/specs/platform-foundation-gap-closure.md section 1 describes a
// much larger Control Plane: hermetic bundles, signatures, activation
// epochs, anti-rollback floors, offline bootstrap. Those are CP-002
// (bundles), CP-003 (signing/epochs) and CP-004 (distribution) — Gate A/Gate
// C work this package deliberately does not attempt. CP-001 is the narrower
// foundation those later todos build on: a single [ConfigurationObject] is
// content-addressed, immutable once published, and carries only the
// metadata needed to know what it is, who published it and where it
// applies. There is no signature, no bundle, no epoch here.
//
// [internal/platform/config] is a sibling package, not a duplicate: it owns
// diffing two configuration snapshots (CONFIG-001) and signing/manifesting a
// configuration object for distribution (CONFIG-002). This package owns the
// one thing that package does not: a governed *activation* step, recorded
// as its own evidence-bearing history, and a [Resolve] that answers "what
// is active right now" without ever guessing.
//
// # Shape
//
// The pattern mirrors [internal/workflow/version] and
// [internal/intent].Registry (over migration 00003's definition_version):
// content-addressed identity, an immutable revision row, and activation as a
// separate governed act rather than a field flipped in place.
//
//   - [Publish] mints an immutable [ConfigurationObject] keyed by
//     (scope, kind, id, revision). Re-publishing the exact same body under an
//     already-published key is idempotent; publishing a different body under
//     the same key is refused — a revision's content, once published, cannot
//     move.
//   - [Activate] appends an [ActivationRecord]: evidence of who activated
//     which published revision, and when. Only a revision [Publish] already
//     recorded may be activated. A later activation for the same
//     (scope, kind, id) does not delete or edit the earlier record — the
//     earlier activation is simply superseded by recency, and its evidence
//     stays in the store's history for [Store.ListActivations] to return.
//   - [Resolve] answers "what is active for (scope, kind, id) right now" by
//     reading the latest [ActivationRecord] and returning the
//     [ConfigurationObject] it names. It never falls back to "latest
//     published" or any other guess when nothing has been activated.
//
// [Store] is the persistence port; [Registry] is the in-memory adapter this
// package ships. internal/data/configregistry provides the PostgreSQL
// adapter over the same port, so this package never imports a database
// driver.
package configregistry
