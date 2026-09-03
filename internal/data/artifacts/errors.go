package artifacts

import (
	"fmt"
)

// Stable conflict and validation codes. They are matched by callers that map
// a typed error to a transport response without reading the message text,
// the same convention internal/data/ledger uses.
const (
	CodeDigestMismatch    = "ARTIFACT_DIGEST_MISMATCH"
	CodeImmutableConflict = "ARTIFACT_IMMUTABLE_CONFLICT"
	CodeContentTooLarge   = "ARTIFACT_CONTENT_TOO_LARGE"
	CodeNotFound          = "ARTIFACT_NOT_FOUND"
	CodeRetrievalDenied   = "ARTIFACT_RETRIEVAL_DENIED"
	CodeReferenceNotFound = "ARTIFACT_REFERENCE_NOT_FOUND"
	CodeRequestInvalid    = "ARTIFACT_REQUEST_INVALID"
)

// ErrDigestMismatch reports that a caller-claimed content id does not match
// the sha256 actually computed over the bytes -- on Put, before a single byte
// is written; on Retrieve, as a defense-in-depth check of what came back from
// storage.
type ErrDigestMismatch struct {
	Claimed string
	Actual  string
}

// Code returns the stable conflict identifier.
func (ErrDigestMismatch) Code() string { return CodeDigestMismatch }

func (e ErrDigestMismatch) Error() string {
	return fmt.Sprintf("%s: claimed content id %s does not match the computed digest %s",
		CodeDigestMismatch, e.Claimed, e.Actual)
}

// ErrImmutableConflict reports a second Put for a content id already on file
// whose identity metadata (media type, size, classification, retention class
// or creator principal) disagrees with what was recorded the first time. An
// artifact's bytes are content-addressed and therefore never collide with
// different bytes under the same id; this is the corresponding guarantee for
// the metadata that rides alongside those bytes.
type ErrImmutableConflict struct {
	ContentID string
	Field     string
}

// Code returns the stable conflict identifier.
func (ErrImmutableConflict) Code() string { return CodeImmutableConflict }

func (e ErrImmutableConflict) Error() string {
	return fmt.Sprintf("%s: artifact %s is already recorded with a different %s; identity never mutates",
		CodeImmutableConflict, e.ContentID, e.Field)
}

// ErrContentTooLarge reports that a PutStream reader produced more than the
// caller's declared cap before content id or storage were ever touched.
type ErrContentTooLarge struct {
	Limit int64
}

// Code returns the stable validation identifier.
func (ErrContentTooLarge) Code() string { return CodeContentTooLarge }

func (e ErrContentTooLarge) Error() string {
	return fmt.Sprintf("%s: content exceeds the %d byte streaming cap", CodeContentTooLarge, e.Limit)
}

// ErrNotFound reports that no artifact exists for the given tenant and
// content id -- including a syntactically well-formed but never-written
// digest, i.e. a guessed reference.
type ErrNotFound struct {
	ContentID string
}

// Code returns the stable validation identifier.
func (ErrNotFound) Code() string { return CodeNotFound }

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("%s: no artifact %s in this tenant", CodeNotFound, e.ContentID)
}

// ErrRetrievalDenied reports that [Retrieve] refused to return bytes. The
// refusal is also recorded as durable evidence (DATA-016); this error is the
// caller-facing half of that same decision.
type ErrRetrievalDenied struct {
	ContentID string
	Reason    string
}

// Code returns the stable conflict identifier.
func (ErrRetrievalDenied) Code() string { return CodeRetrievalDenied }

func (e ErrRetrievalDenied) Error() string {
	return fmt.Sprintf("%s: retrieval of %s denied: %s", CodeRetrievalDenied, e.ContentID, e.Reason)
}

// ErrReferenceNotFound reports that [RemoveReference] was asked to drop a
// reference an owner does not currently hold.
type ErrReferenceNotFound struct {
	ContentID string
	OwnerKind OwnerKind
	OwnerID   string
}

// Code returns the stable validation identifier.
func (ErrReferenceNotFound) Code() string { return CodeReferenceNotFound }

func (e ErrReferenceNotFound) Error() string {
	return fmt.Sprintf("%s: %s %s holds no active reference to %s",
		CodeReferenceNotFound, e.OwnerKind, e.OwnerID, e.ContentID)
}

// ErrRequestInvalid reports a missing or malformed field on a request.
type ErrRequestInvalid struct {
	Field  string
	Reason string
}

// Code returns the stable validation identifier.
func (ErrRequestInvalid) Code() string { return CodeRequestInvalid }

func (e ErrRequestInvalid) Error() string {
	return fmt.Sprintf("%s: %s %s", CodeRequestInvalid, e.Field, e.Reason)
}
