// Package auditpack produces a payroll reconciliation and auditor-evidence
// package from ledger state (owner: data plane; phase: gate C; DB-024).
//
// # What it binds
//
// A payroll run's release rests on four totals that a different process
// produced at a different time: the payroll register (gross pay computed by
// payroll), the bank file (net pay actually disbursed), the tax liability
// (what was withheld) and the filing acknowledgment (what a tax authority
// confirmed receiving). Nothing before this package tied those four numbers
// together, so a duplicate, missing, altered or misdirected transaction in
// any one of them could pass unreconciled.
//
// Every total this package produces is [ResolveFromContent] over an
// [github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence.Content]
// - the same signed-checkpoint-backed, hash-chained slice of the ledger that
// package exports - and never over a domain aggregate. A payroll or paygl
// value object can be wrong in memory; a ledger event a checkpoint has
// already attested to cannot be edited without the attestation failing.
//
// # The idempotency key
//
// [IdempotencyKey] is a pure function of (tenant, run id): the same run
// always resolves to the same key. [Bind] uses it as the ledger append's own
// idempotency key, so re-binding an already-bound run is an exact replay
// (internal/data/ledger's own rule), never a second, competing record. That
// is what "immutable" means here: the key is not stored anywhere as its own
// fact, it is recomputed identically every time and the ledger's replay path
// enforces that recomputation is the only way to reach the same event.
//
// # The variance check
//
// [Reconcile] checks two pairs a sound run must satisfy exactly, at the
// declared decimal scale, with no tolerance: the register total equals the
// bank file total plus the tax liability total (gross pay is disbursed cash
// plus withholding, with no third destination), and the tax liability total
// equals the filing acknowledgment total (what was withheld is what a filing
// authority confirmed). Either pair failing is an unexplained variance:
// [ErrVarianceUnexplained] names the pair and the exact difference, and
// [Bind] refuses to record a reconciliation over it. There is deliberately no
// override parameter - a run that needs an explained exception is a new
// business decision recorded as its own ledger event, not a flag passed to
// this package.
//
// # The auditor package
//
// [Build] wraps one
// github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence.Package
// (unmodified, under the "evidence/" path prefix) with a summary part - the
// four totals, their contributing event references and the variance decision
// - and an outer manifest whose digest folds the evidence package's own
// manifest digest and the summary's digest together. [Verify] checks all of
// it holding only the package bytes and the checkpoint epochs' public keys:
// it re-verifies the wrapped evidence package exactly as
// evidence.VerifyPackage would, and it independently re-derives all four
// totals and the variance decision from the wrapped package's own covered
// events - never from the summary's stated numbers - so a summary edited
// without touching the underlying ledger events is caught by the mismatch
// between what it claims and what the covered events actually fold to.
//
// # What this package does not do
//
// It does not compute payroll, does not decide what a register or bank file
// line's amount is, and does not talk to a tax authority. Those facts are
// assumed to already be recorded as ledger events; [AppendLine] is a thin,
// optional convenience for a caller that wants one call to record and
// hash-chain a contributing line, not the only way to produce one.
package auditpack
