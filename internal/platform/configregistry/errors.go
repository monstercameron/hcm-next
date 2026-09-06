package configregistry

import (
	"errors"
	"fmt"
)

// ErrConfigRegistry is the sentinel every refusal from this package unwraps
// to. Classify with [errors.Is] and read [Error.Code] for the exact reason;
// never match message text.
var ErrConfigRegistry = errors.New("configregistry: rejected")

// Stable refusal codes. A transport, a runtime or a test matches these; they
// never change spelling once published.
const (
	// CodeInvalidKind reports a Kind outside the closed vocabulary
	// [Kind.Valid] declares.
	CodeInvalidKind = "INVALID_KIND"
	// CodeMissingID reports a ConfigurationObject with no ID.
	CodeMissingID = "MISSING_ID"
	// CodeInvalidRevision reports a revision of zero. Revisions start at 1,
	// matching migrations/00002_tenant_primitives.sql's cas_version domain.
	CodeInvalidRevision = "INVALID_REVISION"
	// CodeMissingScope reports a Scope with no TenantID.
	CodeMissingScope = "MISSING_SCOPE"
	// CodeMissingSchemaRef reports a ConfigurationObject with no SchemaRef.
	CodeMissingSchemaRef = "MISSING_SCHEMA_REF"
	// CodeMissingPublisher reports a ConfigurationObject with no
	// PublisherPrincipal.
	CodeMissingPublisher = "MISSING_PUBLISHER"
	// CodeMissingPublishedAt reports a ConfigurationObject with a zero
	// PublishedAt. There is no clock inside this package: the caller
	// supplies the moment of publication.
	CodeMissingPublishedAt = "MISSING_PUBLISHED_AT"
	// CodeEmptyBody reports a ConfigurationObject with no Body.
	CodeEmptyBody = "EMPTY_BODY"
	// CodeBodyDigestMismatch reports a caller-supplied CanonicalBodyDigest
	// that does not match recomputing it from Body — the caller asserted an
	// identity the content does not have.
	CodeBodyDigestMismatch = "BODY_DIGEST_MISMATCH"
	// CodeRevisionConflict reports a [Publish] call naming a (scope, kind,
	// id, revision) key that already carries a different body. A revision,
	// once published, is immutable; a caller that needs to change the
	// content must publish a new revision, not overwrite this one.
	CodeRevisionConflict = "REVISION_CONFLICT"

	// CodeUnauthorizedActivation reports an [Activate] call with no
	// ActivatedBy in its evidence — an unattributed activation.
	CodeUnauthorizedActivation = "UNAUTHORIZED_ACTIVATION"
	// CodeMissingActivationTime reports activation evidence with a zero
	// ActivatedAt.
	CodeMissingActivationTime = "MISSING_ACTIVATION_TIME"
	// CodeUnknownRevision reports an [Activate] or [Resolve] call naming a
	// (scope, kind, id, revision) the store has never seen. Only a revision
	// [Publish] already recorded may be activated.
	CodeUnknownRevision = "UNKNOWN_REVISION"
	// CodeNoActiveRevision reports a [Resolve] call for a (scope, kind, id)
	// that has never been activated. [Resolve] never falls back to "latest
	// published" — an unresolved kind/id is refused rather than guessed.
	CodeNoActiveRevision = "NO_ACTIVE_REVISION"

	// CodeRecordMutated reports a [ConfigurationObject] whose recomputed
	// digest no longer matches the digest it was minted with.
	CodeRecordMutated = "RECORD_MUTATED"
	// CodeNoStore reports a call made with a nil [Store].
	CodeNoStore = "NO_STORE"
)

// Error is one typed refusal: the code, the object id it concerns, and a
// detail written for a person. Code is for a program.
type Error struct {
	Code   string
	Ref    string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	loc := ""
	if e.Ref != "" {
		loc = " [" + e.Ref + "]"
	}
	msg := fmt.Sprintf("configregistry: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel, so
// errors.Is(err, ErrConfigRegistry) classifies every refusal this package
// produces.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrConfigRegistry, e.Err}
	}
	return []error{ErrConfigRegistry}
}

// refuse builds a typed refusal.
func refuse(code, ref, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...)}
}

// wrap builds a typed refusal around an underlying error, e.g. a [Store]
// failure.
func wrap(code, ref string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...), Err: err}
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
