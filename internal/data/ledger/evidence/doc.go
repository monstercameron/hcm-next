// Package evidence exports and verifies auditor-verifiable ledger evidence
// packages (owner: data plane; phase: P1A; LEDGER-012).
//
// An evidence package is everything an independent party needs to satisfy
// itself that a stated slice of a tenant's ledger is the slice that was
// actually recorded - and nothing else. [Export] assembles one from a live
// ledger; [Verify] checks one with no database, no network and no
// credentials of any kind, holding only the package bytes and the public
// keys the checkpoint epochs were signed with.
//
// # What a package contains
//
// For one tenant and one half-open recorded-time window [From, To):
//
//   - header.json - the tenant, the window, the schema release the ledger
//     was read under, and the part index that says what every other path in
//     the package is.
//   - streams/NNNN/chain.json - for every stream holding a covered event,
//     the whole hash chain from genesis to the head as of To: every chain
//     link (internal/data/ledger/hashchain) and every event digest it folds.
//     The prefix before the window is carried because a chain that starts in
//     the middle proves nothing; carrying digests rather than payloads means
//     the prefix discloses no content.
//   - streams/NNNN/events.json - the full envelopes of the events actually
//     inside the window, including their correction targets.
//   - epochs/NNNN.json - every signed checkpoint epoch (LEDGER-010) whose
//     own window intersects the exported window.
//   - manifest.json - one [Part] row per path above with that part's byte
//     length and digest, plus a canonical digest over all of them.
//
// The layout is a path-to-bytes map ([Package.Files]), not an archive. There
// is no tar, zip or compression dependency anywhere in this package: an
// archive format is a transport decision, and binding evidence to one would
// make the digest a function of somebody's compression settings.
//
// # Why the digests nest the way they do
//
// Four independent bindings have to be broken at once to alter a covered
// event without [Verify] naming it:
//
//  1. the part digest in the manifest, which the altered bytes no longer
//     reproduce ([FindingTamperedPart]);
//  2. the manifest's own canonical digest over every part row
//     ([FindingManifestDigest]);
//  3. the event digest recorded in the stream's chain part, which the chain
//     folds and the altered envelope no longer matches
//     ([FindingTamperedEvent]) - and, if the chain part is edited to agree,
//     the fold itself stops reproducing ([FindingBrokenChain]);
//  4. the chain hash at the head, which a signed epoch attests to and which
//     cannot be re-signed without the private key ([FindingUnattestedHead],
//     [FindingSignatureMismatch]).
//
// The first two alone would be forgeable by anyone willing to rewrite the
// manifest. The third and fourth are what make the package evidence rather
// than a report: they terminate in an Ed25519 signature over a root digest
// this package never has the key for.
//
// # Fail closed on another tenant
//
// A tenant identifier appears inside every part, not only in the header.
// [Export] refuses to build a package if any row it read names a tenant
// other than the requested one ([ErrTenantLeak]) - which, under migration
// 00008's row level security, it never can - and [Verify] reports
// [FindingTenantLeak] for any part that names a tenant other than the
// manifest's. A package assembled by some future path that bypassed RLS is
// therefore still refused by the offline verifier.
//
// # Why nothing records when the export ran
//
// The package content is a pure function of (tenant, window, ledger state,
// schema release). No export instant, no exporter identity and no random
// identifier enters any part, so two exports of the same window - concurrent
// or a year apart - are byte-identical, and a difference between two copies
// of a package is always a difference in the evidence. When a checkpoint was
// taken is recorded where it belongs: inside the signed epochs.
//
// # Verifying the event digests themselves
//
// [Verify] does not recompute an event's payload digest by default. Which
// canonicalization profile minted it is a property of the cell that recorded
// it (internal/data/ledger.SHA256Digester and internal/ledger.KernelDigester
// both record the algorithm as "sha256" and produce different values), so
// guessing would produce false accusations of tampering. A verifier that
// knows the cell's profile passes it with [WithEventDigester] and gets that
// last check too.
package evidence
