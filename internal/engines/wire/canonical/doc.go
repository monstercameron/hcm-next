// Package canonical implements the canonical envelope normalization contract.
//
// Owner: wire engine. Phase: P1A (MODEL-006).
//
// # Boundary
//
// This wire engine is the only canonical encoder in the platform. Every approval binding,
// idempotency key, hash chain link, signed configuration bundle, and portable
// audit evidence record derives from bytes produced here. grpcbridge, GWC, and
// any browser or mobile client never recompute canonical bytes or digests; they
// submit values and receive an authoritative
// [github.com/monstercameron/hcm-next/internal/engines/wire/digest.Reference]
// computed on the server. A client-supplied digest is input to be verified, not
// an authority to be trusted.
//
// Ordinary Protobuf serialization is not canonical: field order, unknown-field
// retention, map iteration order, and Unicode form are all unconstrained. This
// package projects a domain message into an explicitly materialized canonical
// model — the [Profile]'s material path include list — and emits a
// self-describing byte stream in ascending field-tag order.
//
// # Determinism guarantees
//
// For a fixed [Profile], [Encode] returns identical bytes for identical
// material meaning across processes, architectures, Go versions, host locales,
// and host timezones. Nothing in the encoder consults the ambient locale, the
// ambient timezone, wall-clock time, Go map iteration order, or any source of
// randomness. Instants encode as UTC seconds and nanoseconds; strings normalize
// to NFC; sets sort by their elements' canonical bytes; maps sort by canonical
// key bytes.
//
// Conversely, every material change produces different bytes, and a change
// confined to fields outside the profile's material list produces byte-identical
// output. That asymmetry is the whole point: republishing a policy bundle must
// not invalidate a pending approval, while editing a planned domain write must.
//
// # Failing closed
//
// Invalid UTF-8, duplicate set members, unknown Protobuf fields on a material
// profile, unrepresentable decimals or instants, and profile/schema mismatches
// are errors, never silently normalized values. See [Error] and the sentinel
// errors it wraps.
//
// # Profile and schema binding
//
// Profile identity, profile version, schema identity, schema version, and the
// canonical model's Protobuf full name are bound into the byte stream header.
// Two schema versions therefore cannot share a digest meaning merely because
// their payload bytes happen to coincide, and substituting one profile for
// another changes the bytes rather than silently verifying.
package canonical
