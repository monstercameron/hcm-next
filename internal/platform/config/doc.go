// Package config implements the CONFIG-001 semantic configuration diff and
// the CONFIG-002 signed dependency manifest (planning/todos.md
// `CONFIG-001`, `CONFIG-002`; planning/specs/hris-admin-dataops.md,
// "Configuration Diff and Promotion"; planning/specs/platform-responsibility-boundaries.md,
// "Customer Configuration Package Manager"; the canonical-digest
// conventions of planning/specs/canonical-envelope-and-digest.md, followed
// here in this package's own smaller, non-Protobuf scope).
//
// # Scope
//
// This package computes a deterministic, typed diff between two
// configuration snapshots ([Snapshot], [Diff]) and builds, signs, and
// verifies an immutable, content-addressed dependency manifest for a
// configuration bundle ([Bundle], [SignBundle], [VerifyBundle]). It does
// not implement promotion, approval routing, or the storage/registry side
// of either concern — that is CONFIG-003 and the Configuration Package
// Manager. This package is the self-contained computation those own.
//
// # Determinism
//
// A [Snapshot]'s entries are indexed by key, never by construction order,
// so two snapshots built from the same entries supplied in different order
// diff and hash identically — [Diff] never reports a change for a snapshot
// that was merely reassembled in a different order. A [Bundle]'s dependency
// list is canonicalized into (kind, name, version) order before it
// contributes to the manifest digest, for the same reason: [BundleDigest]
// and a resulting signature are a function of content, not of the caller's
// slice order.
//
// # Secrets never travel by value
//
// A configuration entry that references a secret is constructed only
// through [NewSecretEntry], which accepts a fingerprint and structurally
// refuses a literal value; [Diff] compares such entries by fingerprint
// equality alone and never surfaces a value for them. A dependency
// manifest's CredentialRefs must be opaque references (see
// [CredentialRefPrefix]); [Bundle] validation rejects anything that is not,
// and never echoes the rejected value back in full.
//
// # Failing closed
//
// A floating dependency version, a missing pinned version or content
// digest, an unknown dependency kind, a duplicate dependency, raw secret
// material posing as a credential reference, and a signature that does not
// verify against the bundle's own recomputed digest are all rejected before
// a manifest is considered valid. [VerifyBundle] distinguishes a tampered
// bundle (content changed after signing) from an unverifiable signature
// (wrong key, corrupted signature) so a caller can tell which failure mode
// occurred.
package config
