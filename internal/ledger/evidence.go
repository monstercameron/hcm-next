package ledger

import (
	"context"

	evidenceadapter "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
)

// Evidence value types, re-exported so a business package needs only this
// port import to export or verify an auditor-verifiable evidence package.
// They are plain data - a request, a manifest, a bytes layout, a finding -
// with no behaviour that depends on PostgreSQL, so aliasing rather than
// redeclaring them keeps one definition (the same reasoning as the
// append-path aliases in port.go).
type (
	EvidenceRequest  = evidenceadapter.Request
	EvidencePackage  = evidenceadapter.Package
	EvidenceManifest = evidenceadapter.Manifest
	EvidenceContent  = evidenceadapter.Content
	EvidencePart     = evidenceadapter.Part
	EvidencePartKind = evidenceadapter.PartKind
	EvidenceStream   = evidenceadapter.Stream
	EvidenceEvent    = evidenceadapter.Event
	EvidenceFinding  = evidenceadapter.Finding
	EvidenceKind     = evidenceadapter.FindingKind
	EvidenceReport   = evidenceadapter.Report
	EvidenceOption   = evidenceadapter.VerifyOption
)

// The finding kinds an offline verification can report, re-exported so a
// caller can branch on one without importing the adapter.
const (
	EvidenceMissingPart          = evidenceadapter.FindingMissingPart
	EvidenceUnlistedPart         = evidenceadapter.FindingUnlistedPart
	EvidenceTamperedPart         = evidenceadapter.FindingTamperedPart
	EvidenceManifestDigest       = evidenceadapter.FindingManifestDigest
	EvidenceMalformedPart        = evidenceadapter.FindingMalformedPart
	EvidenceTamperedEvent        = evidenceadapter.FindingTamperedEvent
	EvidenceBrokenChain          = evidenceadapter.FindingBrokenChain
	EvidenceUnattestedHead       = evidenceadapter.FindingUnattestedHead
	EvidenceMissingEpochCoverage = evidenceadapter.FindingMissingEpochCoverage
	EvidenceSignatureMismatch    = evidenceadapter.FindingSignatureMismatch
	EvidenceTenantLeak           = evidenceadapter.FindingTenantLeak
)

// EvidenceExporter assembles an auditor-verifiable evidence package over a
// tenant's ledger for one recorded-time window.
//
// Unlike [Appender] and [Checkpointer] it takes a [Querier] rather than a
// transaction: an export writes nothing at all, so it neither needs nor
// should hold a write transaction open while it reads a window that may be
// large. internal/data/ledger/evidence.Exporter implements it.
type EvidenceExporter interface {
	// Export reads the covered slice and returns the package. It refuses -
	// producing no package at all - when a stream head is unchained, when no
	// signed checkpoint epoch covers the window, or when any row names
	// another tenant.
	Export(ctx context.Context, q Querier, req EvidenceRequest) (EvidencePackage, error)
	// Read assembles what would be exported without encoding it, for a
	// caller that wants to size or inspect an export first.
	Read(ctx context.Context, q Querier, req EvidenceRequest) (EvidenceContent, error)
}

// NewEvidenceExporter returns the default [EvidenceExporter].
func NewEvidenceExporter() EvidenceExporter { return evidenceadapter.NewExporter() }

// VerifyEvidence checks an evidence package offline: it needs the package
// bytes and the epoch signing keys, and touches no database, no network and
// no credential. It reports one typed finding per failure rather than
// stopping at the first, so one pass tells an auditor everything that is
// wrong with a package.
func VerifyEvidence(files map[string][]byte, dir CheckpointKeyDirectory, opts ...EvidenceOption) EvidenceReport {
	return evidenceadapter.Verify(files, dir, opts...)
}

// VerifyEvidencePackage is [VerifyEvidence] over an assembled package. It
// verifies the serialized bytes a recipient would get, never the in-memory
// value that produced them.
func VerifyEvidencePackage(pkg EvidencePackage, dir CheckpointKeyDirectory, opts ...EvidenceOption) EvidenceReport {
	return evidenceadapter.VerifyPackage(pkg, dir, opts...)
}

// WithEvidenceEventDigester makes verification recompute every covered
// event's payload digest with d. It is opt-in because which canonicalization
// profile minted a digest is a property of the cell that recorded it: a cell
// wired with [NewAppender] passes [NewKernelDigester], and one on
// internal/data/ledger's own default passes that.
func WithEvidenceEventDigester(d Digester) EvidenceOption {
	return evidenceadapter.WithEventDigester(d)
}

// BuildEvidence turns already-assembled content into a package. It is pure -
// no clock, no database, no identifier source - so the same content always
// produces the same bytes.
func BuildEvidence(c EvidenceContent) (EvidencePackage, error) {
	return evidenceadapter.Build(c)
}
