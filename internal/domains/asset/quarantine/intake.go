package quarantine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Algorithm is the only digest algorithm this package computes, the same
// identifier internal/data/artifacts.Algorithm uses for the governed
// artifact store it eventually promotes an admitted derivative into.
const Algorithm = "sha256"

// StorageBackstopBytes is the absolute ceiling on what [Upload] will ever
// attempt to persist through [Store.RecordQuarantined], independent of
// whatever a caller's [Policy] declares. It mirrors
// internal/data/artifacts.DefaultMaxContentBytes and
// migrations/00030_artifact_quarantine.sql's own
// artifact_quarantine_byte_size_bounded CHECK constraint: an upload larger
// than this cannot be written as quarantine evidence at all, so [Upload]
// refuses it directly with [ErrContentTooLarge] rather than attempting a
// write the storage layer's own backstop would reject anyway.
const StorageBackstopBytes int64 = 64 << 20

// UploadRequest is one artifact submitted for quarantine intake.
type UploadRequest struct {
	// Tenant is the owning tenant. It is opaque to this package -- never
	// parsed or validated as a UUID -- and passed through to [Store]
	// exactly as given.
	Tenant string
	// Content is the artifact's bytes, already fully read into memory.
	// Its sha256 digest becomes the content id every [Store] method and
	// [Result] name.
	Content []byte
	// DeclaredContentType is the caller's own claim about what Content
	// is. [Upload] never trusts it in place of [Sniff]'s own
	// determination; it exists to be checked against that determination.
	DeclaredContentType ContentType
	// CreatorPrincipalRef is the principal that submitted Content. It is
	// required: quarantine evidence is worthless without knowing whose
	// upload produced it.
	CreatorPrincipalRef string
	// EvidenceID names the intake event (an upload request, an inbound
	// email attachment, an SFTP receipt) that produced Content. It is
	// required, and is the evidence recorded on the initial [Quarantined]
	// fact; the verdict [Upload] reaches afterward carries its own,
	// separately derived evidence id (see [deriveEvidenceID]).
	EvidenceID string
}

// Result is what [Upload] returns: the content id it computed, the state it
// reached, and -- for [Rejected] -- why.
type Result struct {
	ContentID           string
	DigestAlgorithm     string
	ByteSize            int64
	DeclaredContentType ContentType
	SniffedCategory     Category
	State               State
	ScannerID           string
	ScannerVersion      string
	Reason              string
}

// Upload is DOC-MAL-001's whole intake path. It always computes the digest
// and records the [Quarantined] fact first (unless Content itself exceeds
// [StorageBackstopBytes], in which case nothing is persisted at all and
// [ErrContentTooLarge] is returned directly), then evaluates -- in order --
// the declared size allowlist, the declared content-type allowlist, the
// magic-byte sniff against the declared type, and finally the declared
// [Scanner]. The first check that fails ends the upload in [Rejected] with a
// specific reason; passing every check ends it in [Admitted]. A [Scanner]
// error is treated exactly like an explicit unsafe [Verdict]: it is
// [Rejected], never an admit and never silently ignored.
func Upload(ctx context.Context, store Store, scanner Scanner, policy Policy, req UploadRequest, now time.Time) (Result, error) {
	if err := policy.Validate(); err != nil {
		return Result{}, err
	}
	if req.Tenant == "" {
		return Result{}, ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if len(req.Content) == 0 {
		return Result{}, ErrRequestInvalid{Field: "Content", Reason: "is required"}
	}
	if !req.DeclaredContentType.Valid() {
		return Result{}, ErrRequestInvalid{Field: "DeclaredContentType", Reason: "must be one of the declared content types"}
	}
	if req.CreatorPrincipalRef == "" {
		return Result{}, ErrRequestInvalid{Field: "CreatorPrincipalRef", Reason: "is required"}
	}
	if req.EvidenceID == "" {
		return Result{}, ErrRequestInvalid{Field: "EvidenceID", Reason: "is required"}
	}

	size := int64(len(req.Content))
	if size > StorageBackstopBytes {
		return Result{}, ErrContentTooLarge{Limit: StorageBackstopBytes, Size: size}
	}

	digest := ComputeDigest(req.Content)
	sniffed := Sniff(req.Content)

	result := Result{
		ContentID:           digest,
		DigestAlgorithm:     Algorithm,
		ByteSize:            size,
		DeclaredContentType: req.DeclaredContentType,
		SniffedCategory:     sniffed,
		State:               Quarantined,
	}

	quarantinedEvidence := deriveEvidenceID(req.EvidenceID, "quarantined", digest, now)
	if err := store.RecordQuarantined(ctx, req.Tenant, QuarantinedRecord{
		ContentID:           digest,
		DigestAlgorithm:     Algorithm,
		ByteSize:            size,
		DeclaredContentType: req.DeclaredContentType,
		SniffedCategory:     sniffed,
		CreatorPrincipalRef: req.CreatorPrincipalRef,
		EvidenceID:          quarantinedEvidence,
		Content:             req.Content,
	}); err != nil {
		return Result{}, fmt.Errorf("quarantine: record quarantined %s: %w", digest, err)
	}

	reject := func(reason string) (Result, error) {
		result.State = Rejected
		result.Reason = reason
		result.ScannerID = policy.ScannerID
		result.ScannerVersion = policy.ScannerVersion
		evidence := deriveEvidenceID(req.EvidenceID, "rejected", digest, now)
		if err := store.RecordVerdict(ctx, req.Tenant, VerdictRecord{
			ContentID:      digest,
			State:          Rejected,
			ScannerID:      policy.ScannerID,
			ScannerVersion: policy.ScannerVersion,
			Reason:         reason,
			EvidenceID:     evidence,
		}); err != nil {
			return Result{}, fmt.Errorf("quarantine: record rejected %s: %w", digest, err)
		}
		return result, nil
	}

	if size > policy.MaxContentBytes {
		return reject(fmt.Sprintf("content is %d bytes, which exceeds the declared %d byte policy allowlist", size, policy.MaxContentBytes))
	}
	if !policy.allows(req.DeclaredContentType) {
		return reject(fmt.Sprintf("declared content type %q is not on the declared content-type allowlist", req.DeclaredContentType))
	}
	expected, _ := req.DeclaredContentType.ExpectedCategory()
	if sniffed != expected {
		return reject(fmt.Sprintf("declared content type %q expects a %s signature, but the content sniffed as %s (polyglot or mismatched file)", req.DeclaredContentType, expected, sniffed))
	}

	verdict, err := scanner.Scan(ctx, digest, bytes.NewReader(req.Content))
	if err != nil {
		return reject(fmt.Sprintf("scanner %s/%s failed to produce a verdict: %v", policy.ScannerID, policy.ScannerVersion, err))
	}
	if !verdict.Safe {
		reason := verdict.Reason
		if reason == "" {
			reason = "scanner reported unsafe content"
		}
		return reject(reason)
	}

	result.State = Admitted
	result.ScannerID = policy.ScannerID
	result.ScannerVersion = policy.ScannerVersion
	evidence := deriveEvidenceID(req.EvidenceID, "admitted", digest, now)
	if err := store.RecordVerdict(ctx, req.Tenant, VerdictRecord{
		ContentID:      digest,
		State:          Admitted,
		ScannerID:      policy.ScannerID,
		ScannerVersion: policy.ScannerVersion,
		EvidenceID:     evidence,
	}); err != nil {
		return Result{}, fmt.Errorf("quarantine: record admitted %s: %w", digest, err)
	}
	return result, nil
}

// ComputeDigest returns the lowercase hex sha256 digest of content -- the
// same content id [Upload] records and [Use] later looks up by.
func ComputeDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// deriveEvidenceID computes a deterministic evidence identifier for one
// phase of one upload's lifecycle, the same technique
// internal/data/artifacts.refusalEvidenceID uses: a stable prefix naming
// what produced it, plus a digest over exactly the inputs that phase's
// decision was made from. Replaying an identical upload at an identical
// instant always yields identical evidence ids for each phase, and the three
// phases (quarantined, admitted, rejected) never collide with each other
// even when they share a base evidence id and content id.
func deriveEvidenceID(base, phase, contentID string, at time.Time) string {
	h := sha256.New()
	writeField := func(s string) {
		fmt.Fprintf(h, "%d:%s\x00", len(s), s)
	}
	writeField(base)
	writeField(phase)
	writeField(contentID)
	writeField(at.UTC().Format(time.RFC3339Nano))
	return "ev:quarantine:" + phase + ":" + hex.EncodeToString(h.Sum(nil))[:32]
}
