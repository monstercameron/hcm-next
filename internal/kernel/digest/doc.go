// Package digest implements versioned canonical digest profiles and the
// digest reference envelope that carries them.
//
// Owner: kernel. Phase: P1A (MODEL-007).
//
// # Boundary
//
// Go is the only authority that mints or verifies a canonical digest. A
// [Reference] is the durable envelope — profile identity and version, schema
// identity and version, algorithm, scope binding, canonical length, and the
// digest itself — and it round-trips losslessly to
// hcmnext.intents.v1.CanonicalDigestReference for transport and storage.
// grpcbridge and GWC never recompute approval or ledger digests in browser
// code; a client-supplied reference is untrusted input for [Registry.Verify],
// never a value to be believed.
//
// # Versioning is the point
//
// A profile change creates a new profile version; it never edits one in place.
// [Registry.Verify] recomputes using the profile version and algorithm named in
// the reference being verified, so a reference minted under PROPOSAL v1 keeps
// verifying byte-for-byte after PROPOSAL v2 is registered. Where a new digest
// is genuinely needed, [Registry.Migrate] records a [Migration] linking the old
// and new envelopes to the same authorized source object. The old digest is
// never overwritten, and both may be carried as dual digests through a declared
// algorithm transition — see [Migration.DualDigests] and [Registry.VerifyDual].
//
// # Failing closed
//
// Unknown algorithm, unknown profile or profile version, an empty digest, a
// scope binding that does not match the source object, and profile
// substitution all fail. Registration itself fails when a profile omits a path
// the platform requires that profile to bind, or claims a path the platform
// declares revalidated context rather than material — for PROPOSAL that means
// the proposal payload and the intent/revision identity are mandatory and
// control_snapshots are forbidden.
package digest
