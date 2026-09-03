// Package ledger is the ledger PORT
// (definitions/architecture/package-dependency-policy.yaml
// ports_and_adapters: "ledger"): the interfaces business packages depend on
// to append and read the authoritative transaction ledger, plus the wiring
// that connects internal/engines/wire/digest to internal/data/ledger's Digester
// hook.
//
// internal/data/ledger is the PostgreSQL adapter underneath this port: its
// *Appender and *Reader satisfy [Appender] and [Reader] structurally, and
// this package's [NewAppender] and [NewLedgerEventDigestRegistry] build one
// configured with a kernel-digest-backed [Digester] instead of the adapter's
// own ad hoc default.
//
// # Why a separate digester
//
// internal/data/ledger.SHA256Digester (LEDGER-002/DATA-003) hashes the
// schema reference and payload bytes directly; it is a legitimate, tested
// P1A digester with no dependency on the kernel canonicalization machinery.
// [KernelDigester] instead routes the same inputs through
// internal/engines/wire/digest under a registered, versioned canonicalization
// profile (LEDGER_EVENT), so a ledger event's digest is minted the same way
// every other canonical digest in the platform is: by a published profile
// version that never mutates once registered, verified by recomputation
// rather than trusted from the caller.
package ledger
