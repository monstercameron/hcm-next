package provenance

import "fmt"

// Stable codes, mirroring internal/data/ledger's own convention of a Code()
// method plus a typed error struct per validation failure.
const (
	CodeMissingEvidence   = "PROVENANCE_MISSING_EVIDENCE"
	CodeMissingDigest     = "PROVENANCE_MISSING_DIGEST"
	CodeInvalidSourceKind = "PROVENANCE_INVALID_SOURCE_KIND"
	CodeRequestInvalid    = "PROVENANCE_PUBLISH_REQUEST_INVALID"
)

// ErrMissingEvidence reports a publish attempt with no evidence IDs -
// DATA-014's named RED clause: provenance is never published without its
// evidence id.
type ErrMissingEvidence struct {
	SourceKind SourceKind
	SourceRef  string
}

func (ErrMissingEvidence) Code() string { return CodeMissingEvidence }

func (e ErrMissingEvidence) Error() string {
	return fmt.Sprintf("%s: %s %s carries no evidence id", CodeMissingEvidence, e.SourceKind, e.SourceRef)
}

// ErrMissingDigest reports a publish attempt with no digest. A provenance
// record with nothing to check content against could never be verified.
type ErrMissingDigest struct {
	SourceKind SourceKind
	SourceRef  string
}

func (ErrMissingDigest) Code() string { return CodeMissingDigest }

func (e ErrMissingDigest) Error() string {
	return fmt.Sprintf("%s: %s %s carries no digest", CodeMissingDigest, e.SourceKind, e.SourceRef)
}

// ErrInvalidSourceKind reports a SourceKind outside the two declared values.
type ErrInvalidSourceKind struct {
	SourceKind SourceKind
}

func (ErrInvalidSourceKind) Code() string { return CodeInvalidSourceKind }

func (e ErrInvalidSourceKind) Error() string {
	return fmt.Sprintf("%s: %q is not LEDGER_EVENT or EXTERNAL_OBSERVATION", CodeInvalidSourceKind, e.SourceKind)
}

// ErrRequestInvalid reports a missing or malformed field on a publish
// request other than evidence, digest or source kind.
type ErrRequestInvalid struct {
	Field  string
	Reason string
}

func (ErrRequestInvalid) Code() string { return CodeRequestInvalid }

func (e ErrRequestInvalid) Error() string {
	return fmt.Sprintf("%s: %s %s", CodeRequestInvalid, e.Field, e.Reason)
}
