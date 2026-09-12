// Package evidence implements EP-EVID-001: the EvidenceService transport
// surface (GetExecutionReceipt, ExportIntentEvidence) over
// hcmnext.evidence.v1, composed from already-owned building blocks rather
// than reimplemented here:
//
//   - internal/evidence (EVIDENCE-001) supplies BusinessExecutionReceipt,
//     Assemble/Verify/Redacted — the immutable, 15-dimension audit receipt
//     for a closed intent. This package never assembles a receipt from
//     anything but the frozen dimension facts a [LineageSource] hands back;
//     it never re-reads current domain state.
//   - internal/transport/operations (EP-OPS-001) supplies the one
//     long-running Operation concept every governed export rides on. This
//     package never invents a second one: ExportIntentEvidence creates
//     exactly one operations.Record per logical export and the caller polls
//     it through the existing OperationsService.
//   - internal/operations/export (EXPORT-001) supplies the
//     formula-injection-safe HUMAN_SPREADSHEET renderer and the
//     byte-exact MACHINE_DATA renderer this package uses to produce the
//     export package's two artifacts.
//   - internal/cryptoagility supplies the Ed25519 signing/verification this
//     package uses to make the exported package tamper-evident; this
//     package adds AES-256-GCM sealing of the package bytes themselves.
//
// # GetExecutionReceipt and the frozen wire contract
//
// GetExecutionReceiptResponse.execution_receipt.receipt is wire-typed as
// hcmnext.evidence.v1.ZeroEffectReceipt (schema/proto/hcmnext/evidence/v1
// receipt.proto), which is internal/domains/evidence's proof that a
// PREFLIGHT/SIMULATE calculation changed nothing -- EffectCounters must be
// all zero, and ReceiptMode has no value other than PREFLIGHT/SIMULATE.
// That message cannot honestly carry EVIDENCE-001's BusinessExecutionReceipt
// (the 15-dimension, lineage-bearing evidence object for a CLOSED intent
// that DID have effects: transaction heads, domain revisions and effects
// are all legitimately PRESENT there) without either lying about the effect
// count or leaving the message's own declared invariant unchecked. This
// package does not do that. GetExecutionReceipt here serves genuine,
// already-immutable ZeroEffectReceipt-shaped receipts (addressed by
// receipt_id, purpose- and tenant-scoped, non-disclosing on refusal);
// BusinessExecutionReceipt content is surfaced only through
// ExportIntentEvidence's Operation result, whose TypedPayload/
// CanonicalDigestReference fields are a generic reference envelope built
// for exactly this. See this package's evidence001_test.go and the EP-EVID
// -001 report for the full reasoning; this is a reported finding, not a
// silent workaround.
package evidence
