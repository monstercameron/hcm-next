// Package quarantine implements DOC-MAL-001: every artifact uploaded into
// Human Capital Management Suite is untrusted until a declared [Scanner] and a declared allowlist
// have both cleared it, and nothing may read it for use before that happens.
//
// Semantic owner: domains (asset). Phase: P1B.
//
// # The state machine
//
// [Upload] is the only intake path this package implements. It always
// records an artifact as [Quarantined] first -- that is the durable fact
// that untrusted bytes were received, independent of whatever verdict comes
// next (planning/specs/platform-foundation-gap-closure.md §3: "the original
// remains restricted evidence"). It then applies the declared size and
// content-type allowlists, sniffs the bytes' own magic-byte signature and
// refuses a mismatch against the caller's declared type (a polyglot or a
// renamed file), and only when those syntactic checks pass does it call the
// declared [Scanner]. Every path that is not an unconditional admit ends in
// [Rejected] with a non-empty reason: a scanner error is a rejection, never
// a fallback admit, exactly like a scanner that runs and reports the content
// unsafe.
//
// # Why this package never touches a database
//
// This package is pure Go: it takes [Store] and [Scanner] as ports and knows
// nothing about PostgreSQL, pgx or SQL. internal/data/artifacts owns the
// actual persistence (byte storage and the append-only verdict log,
// migrations/00030_artifact_quarantine.sql) and implements [Store] against
// it; internal/domains/asset/quarantine owns only the policy decision of
// what to store and which verdict to reach. This mirrors the split that
// DOC-INTAKE-001's REFACTOR clause requires: malware scanning, byte storage,
// classification and domain evidence sufficiency stay separate owners.
//
// # The Use gate
//
// [Use] is the other half of the contract: it is the only sanctioned way for
// any other capability (preview, extraction, indexing, a workflow step, a
// provider call) to ask whether it may treat a content id as safe to read.
// It refuses by state -- anything other than [Admitted] is refused, whether
// that is a fresh [Quarantined] artifact still awaiting a verdict, a
// [Rejected] one, or a content id [Store] has never heard of at all.
package quarantine

import (
	"fmt"
)

// State is where one quarantined content id currently stands. It is never a
// mutable column: [Store] persists it as an append-only log, and the current
// state is whichever row was recorded most recently for that content id.
type State string

// The three declared states. There is no fourth: DOC-MAL-001's RED clause is
// exactly the claim that nothing may reach a fourth, implicit "safe by
// default" state.
const (
	// Quarantined is the state every uploaded artifact starts in, the
	// instant its bytes and declared type are recorded -- before any
	// syntactic check or scanner call has run.
	Quarantined State = "QUARANTINED"
	// Admitted is the terminal state reached only when every syntactic
	// check passed and the declared [Scanner] reported the content safe.
	Admitted State = "ADMITTED"
	// Rejected is the terminal state reached by any failure: an
	// oversized upload, a declared content type outside the allowlist, a
	// declared type that does not match the bytes' own magic-byte
	// signature, a scanner verdict of unsafe, or a scanner that failed to
	// run at all. Every Rejected state carries a non-empty [StateRecord]
	// Reason.
	Rejected State = "REJECTED"
)

// Valid reports whether s is one of the three declared states.
func (s State) Valid() bool {
	switch s {
	case Quarantined, Admitted, Rejected:
		return true
	default:
		return false
	}
}

// Category is the bucket [Sniff] assigns to a byte slice by inspecting its
// own magic bytes, independent of whatever type the uploader declared.
type Category string

// The declared sniff categories. CategoryUnknown covers any byte slice that
// matches none of the recognized signatures, including a signature this
// package does not implement at all.
const (
	CategoryPDF     Category = "PDF"
	CategoryPNG     Category = "PNG"
	CategoryJPEG    Category = "JPEG"
	CategoryZIP     Category = "ZIP"
	CategoryText    Category = "TEXT"
	CategoryUnknown Category = "UNKNOWN"
)

// ContentType is the declared allowlist vocabulary a caller states up front
// and [Policy] admits or refuses. It is deliberately a small, closed set:
// widening it means adding both a case here and a signature to [Sniff], not
// loosening a string comparison somewhere downstream.
type ContentType string

// The declared content types this package recognizes and can sniff for.
const (
	ContentPDF  ContentType = "pdf"
	ContentPNG  ContentType = "png"
	ContentJPEG ContentType = "jpeg"
	ContentDOCX ContentType = "docx"
	ContentCSV  ContentType = "csv"
	ContentTXT  ContentType = "txt"
)

// expectedCategory maps a declared [ContentType] to the [Category] its bytes
// must sniff as. csv and txt share [CategoryText]: a magic-byte sniff cannot
// distinguish a comma-separated file from any other plain text file, and
// this package does not pretend otherwise -- it only ever refuses a
// *binary* mismatch (a PDF, image or zip archive declared as text), which is
// exactly the polyglot/renamed-file attack DOC-MAL-001 names.
var expectedCategory = map[ContentType]Category{
	ContentPDF:  CategoryPDF,
	ContentPNG:  CategoryPNG,
	ContentJPEG: CategoryJPEG,
	ContentDOCX: CategoryZIP,
	ContentCSV:  CategoryText,
	ContentTXT:  CategoryText,
}

// Valid reports whether t is one of the declared content types.
func (t ContentType) Valid() bool {
	_, ok := expectedCategory[t]
	return ok
}

// ExpectedCategory returns the sniff category t's bytes must match, and
// false when t is not a declared content type at all.
func (t ContentType) ExpectedCategory() (Category, bool) {
	c, ok := expectedCategory[t]
	return c, ok
}

// Policy is the declared configuration [Upload] enforces: the size and
// content-type allowlists, and the scanner identity that names whichever
// verdict it reaches. Nothing in this package widens Policy on the fly --
// a caller that wants a larger cap or a wider allowlist constructs a new
// Policy value; there is no runtime override path.
type Policy struct {
	// MaxContentBytes is the declared per-upload size allowlist. An
	// upload strictly larger than this is rejected before the scanner
	// ever runs.
	MaxContentBytes int64
	// AllowedContentTypes is the declared content-type allowlist. An
	// upload whose caller-declared type is not in this set is rejected
	// before the scanner ever runs, even when its bytes would otherwise
	// sniff cleanly.
	AllowedContentTypes []ContentType
	// ScannerID and ScannerVersion identify the scanner this policy
	// declares. They are recorded as evidence on every verdict [Upload]
	// reaches through this policy, whether admitted or rejected,
	// including a scanner failure.
	ScannerID      string
	ScannerVersion string
}

// Validate reports whether p is well-formed: a positive size cap, at least
// one declared content type (each one recognized), and a non-empty declared
// scanner identity.
func (p Policy) Validate() error {
	if p.MaxContentBytes <= 0 {
		return ErrRequestInvalid{Field: "Policy.MaxContentBytes", Reason: "must be positive"}
	}
	if len(p.AllowedContentTypes) == 0 {
		return ErrRequestInvalid{Field: "Policy.AllowedContentTypes", Reason: "must declare at least one content type"}
	}
	for _, t := range p.AllowedContentTypes {
		if !t.Valid() {
			return ErrRequestInvalid{Field: "Policy.AllowedContentTypes", Reason: fmt.Sprintf("%q is not a recognized content type", t)}
		}
	}
	if p.ScannerID == "" {
		return ErrRequestInvalid{Field: "Policy.ScannerID", Reason: "is required"}
	}
	if p.ScannerVersion == "" {
		return ErrRequestInvalid{Field: "Policy.ScannerVersion", Reason: "is required"}
	}
	return nil
}

// allows reports whether t is on the declared content-type allowlist.
func (p Policy) allows(t ContentType) bool {
	for _, allowed := range p.AllowedContentTypes {
		if allowed == t {
			return true
		}
	}
	return false
}
