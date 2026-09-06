package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// Review-record errors. All are matchable with errors.Is.
var (
	// ErrReviewRecordActor is returned when a review record's author and
	// reviewer are not both present and distinct. LEGAL-015's separation of
	// duties starts here: a review record that does not name two different
	// people cannot exist, let alone be signed.
	ErrReviewRecordActor = errors.New("legal: review record author and reviewer must be distinct")
	// ErrReviewFindingSeverity is returned when a typed finding carries no
	// severity.
	ErrReviewFindingSeverity = errors.New("legal: review finding severity is unspecified")
)

// FindingSeverity classifies one typed review finding. It exists so a
// review record is never a free-text approval: every observation the
// reviewer records carries a severity a downstream consumer can act on
// without re-reading prose.
type FindingSeverity uint8

// Finding severities.
const (
	// FindingSeverityUnspecified is the zero value and is never legal on a
	// recorded finding.
	FindingSeverityUnspecified FindingSeverity = iota
	// FindingSeverityInfo is a non-blocking observation, e.g. a citation the
	// reviewer independently confirmed.
	FindingSeverityInfo
	// FindingSeverityConcern flags something the reviewer wants tracked but
	// did not treat as disqualifying, e.g. a rule that reads narrower than
	// the source but is not wrong.
	FindingSeverityConcern
	// FindingSeverityBlocking means the reviewer will not raise the pack's
	// review status while this finding stands.
	FindingSeverityBlocking
)

var findingSeverityWire = map[FindingSeverity]string{
	FindingSeverityInfo:     "INFO",
	FindingSeverityConcern:  "CONCERN",
	FindingSeverityBlocking: "BLOCKING",
}

// String returns the stable wire token.
func (s FindingSeverity) String() string {
	if w, ok := findingSeverityWire[s]; ok {
		return w
	}
	return "FINDING_SEVERITY_UNSPECIFIED"
}

// ReviewFinding is one typed observation a reviewer records against a
// specific obligation (or the pack overall, when ObligationID is empty).
type ReviewFinding struct {
	// ObligationID names the obligation the finding is about. Empty means
	// the finding is about the pack as a whole.
	ObligationID string
	Severity     FindingSeverity
	// Note is the reviewer's own account of the finding. Unlike a
	// [Citation]'s Note, this is the review's substantive content, not a
	// paraphrase of an already-cited primary source, so it is part of the
	// review record's digest.
	Note string
}

func (f ReviewFinding) validate() error {
	if f.Severity == FindingSeverityUnspecified {
		return ErrReviewFindingSeverity
	}
	return nil
}

// ReviewRecord is the auditable outcome of one reviewer's pass over a pack
// candidate: who reviewed it, distinct from who authored it, what
// [ReviewStatus] they raised it to, and the typed findings that justify the
// raise. It is a digested artifact in its own right, signed by the reviewer
// over its own digest (see [ReviewRecord.ComputeDigest]) rather than folded
// silently into the pack's own release digest, so the review itself is
// independently verifiable evidence, not just a status field someone set.
type ReviewRecord struct {
	// PackDigest pins the exact candidate this review is about: the digest
	// [PackCandidate.Pack] would produce with Status already raised to the
	// value below (review_status is part of the release digest per the
	// contract's section 3.2), so a review record can never be replayed
	// against a pack it never actually examined.
	PackDigest string
	AuthorID   string
	ReviewerID string
	Status     ReviewStatus
	Findings   []ReviewFinding
}

// Validate reports whether the review record is well formed: it pins a pack
// digest, names two distinct, non-empty actors, declares a review status,
// and every finding carries a severity.
func (r ReviewRecord) Validate() error {
	if r.PackDigest == "" {
		return fmt.Errorf("legal: review record carries no pack digest")
	}
	if r.AuthorID == "" || r.ReviewerID == "" {
		return fmt.Errorf("%w: author=%q reviewer=%q", ErrReviewRecordActor, r.AuthorID, r.ReviewerID)
	}
	if r.AuthorID == r.ReviewerID {
		return fmt.Errorf("%w: both are %q", ErrReviewRecordActor, r.AuthorID)
	}
	if r.Status == ReviewStatusUnspecified {
		return ErrCitationStatus
	}
	for _, f := range r.Findings {
		if err := f.validate(); err != nil {
			return err
		}
	}
	return nil
}

// CanonicalBytes returns the deterministic encoding a review record's digest
// covers, using the same length-prefixed framing [RulePack.canonicalBytes]
// uses: the reviewed pack's digest, the two actor ids, the raised status,
// and every finding in declared order.
func (r ReviewRecord) CanonicalBytes() []byte {
	var b []byte
	b = append(b, 0x52, 0x56, 0x31) // "RV1" - ReviewRecord, encoding version 1.
	b = appendField(b, "pack_digest", r.PackDigest)
	b = appendField(b, "author_id", r.AuthorID)
	b = appendField(b, "reviewer_id", r.ReviewerID)
	b = appendField(b, "status", r.Status.String())
	b = appendUint32Field(b, "finding_count", uint32(len(r.Findings)))
	for _, f := range r.Findings {
		b = appendField(b, "finding.obligation_id", f.ObligationID)
		b = appendField(b, "finding.severity", f.Severity.String())
		b = appendField(b, "finding.note", f.Note)
	}
	return b
}

// ComputeDigest returns the lowercase hex sha256 over the review record's
// canonical encoding.
func (r ReviewRecord) ComputeDigest() string {
	sum := sha256.Sum256(r.CanonicalBytes())
	return hex.EncodeToString(sum[:])
}

// HasBlockingFinding reports whether any finding is [FindingSeverityBlocking].
func (r ReviewRecord) HasBlockingFinding() bool {
	for _, f := range r.Findings {
		if f.Severity == FindingSeverityBlocking {
			return true
		}
	}
	return false
}

// Explain renders a deterministic, human-readable one-line summary: who
// reviewed whose work, what status they raised it to, and a breakdown of
// findings by severity.
func (r ReviewRecord) Explain() string {
	var blocking, concern, info int
	for _, f := range r.Findings {
		switch f.Severity {
		case FindingSeverityBlocking:
			blocking++
		case FindingSeverityConcern:
			concern++
		case FindingSeverityInfo:
			info++
		}
	}
	return fmt.Sprintf(
		"review reviewer=%s author=%s raised=%s findings=%d(blocking=%d concern=%d info=%d) pack_digest=%s",
		r.ReviewerID, r.AuthorID, r.Status, len(r.Findings), blocking, concern, info, r.PackDigest)
}
