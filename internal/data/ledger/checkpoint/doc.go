// Package checkpoint creates and verifies signed ledger checkpoints and
// integrity epochs (owner: data plane; phase: P1A; LEDGER-010).
//
// A checkpoint is a signed statement, made at one instant, about where every
// stream in a tenant's ledger stood and what its hash chain proved at that
// point. An epoch is the numbered, append-only sequence those checkpoints
// form. Together they turn internal/data/ledger/hashchain's tamper-evidence
// from something only the database can attest to into something an
// independent party can check offline, holding nothing but a manifest and a
// public key.
//
// # What a manifest binds
//
// A [Manifest] is refused by [Manifest.Validate] unless it carries every one
// of the following, because a checkpoint missing any of them proves less
// than it appears to:
//
//   - The stream range: every stream in the tenant that holds events, each
//     with its head sequence, its chain hash and the algorithm that produced
//     it ([StreamHead]). A checkpoint over a subset is not a checkpoint of
//     the ledger; [Service.Create] refuses one with [ErrIncompleteCoverage]
//     naming the exact streams it could not cover.
//   - The root digest over those heads, recomputed and compared rather than
//     accepted from the caller ([ComputeRootDigest]).
//   - The schema release the ledger was read under - a migration version and
//     the digest of the migration tree - so a manifest cannot be replayed
//     against a schema that means something different.
//   - The key: the signing key's identifier and public key, recorded inside
//     the signed bytes.
//   - The time: when the checkpoint was created, and the half-open recorded
//     -time window [CoversFrom, CoversTo) it makes its claim over.
//
// # Why a key is a port
//
// Nothing in this package holds key material. [Signer] is an interface, and
// [Ed25519Signer] is a thin binding for a crypto/ed25519 private key the
// caller already holds - from a KMS, an HSM, or a development file. The
// repository therefore contains exactly one private key, the loudly-labelled
// test fixture under testdata/, and it signs nothing but this package's own
// tests.
//
// [KeyDirectory] is the second half of that seam: it states, per key, the
// half-open validity window and whether the key has been revoked.
// [Service.Create] consults it before signing and refuses to sign at all
// with a key that is expired or revoked at the checkpoint's own instant
// ([ErrKeyNotUsable]), and [Verify] consults it again to refuse a signature
// that was made outside its key's window. A signature that could only have
// been produced by a key that was not permitted to produce it is not
// evidence, however well it verifies arithmetically.
//
// # Why late discovery makes a new epoch
//
// An epoch's signature is never rewritten. If a checkpoint is later found to
// have been made over a ledger that was already damaged, the correction is a
// new, higher-numbered epoch that names the one it corrects and says why
// ([Supersede]), signed on its own terms. The superseded epoch stays exactly
// as it was signed - the storage table is append-only and refuses UPDATE and
// DELETE outright - because the fact that a false attestation was made at a
// particular time is itself part of the record an auditor needs.
//
// Epochs therefore also chain: each carries the previous epoch's manifest
// digest, so a removed epoch leaves a hole that [VerifyChain] reports rather
// than a gap that closes silently.
//
// # External anchoring
//
// Publishing a checkpoint to a write-once store, a transparency log or a
// counterparty is deliberately outside the signing path: [Anchor] is a port
// with a no-op default, so which provider anchors a checkpoint - or whether
// one does at all - never changes what the checkpoint says or how it
// verifies.
package checkpoint
