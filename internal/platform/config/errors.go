package config

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrEmptyKey reports an entry, dependency, or bundle whose identifying
	// key/name/id is empty.
	ErrEmptyKey = errors.New("config: key is empty")

	// ErrInvalidKind reports an unspecified or unknown ValueKind.
	ErrInvalidKind = errors.New("config: value kind is unspecified or unknown")

	// ErrDuplicateKey reports two entries in one Snapshot sharing a key.
	ErrDuplicateKey = errors.New("config: duplicate key in snapshot")

	// ErrMissingFingerprint reports a secret-kind entry with no fingerprint.
	ErrMissingFingerprint = errors.New("config: secret entry requires a fingerprint")

	// ErrSecretValueLeak reports a secret-kind entry that also carries a
	// literal value. A secret is compared and diffed by fingerprint only;
	// an entry that tries to carry both is rejected rather than silently
	// preferring one field over the other.
	ErrSecretValueLeak = errors.New("config: secret entry must not carry a literal value")

	// ErrUnknownDependencyKind reports an unspecified or unknown
	// DependencyKind.
	ErrUnknownDependencyKind = errors.New("config: dependency kind is unspecified or unknown")

	// ErrMissingVersion reports a dependency with no pinned version.
	ErrMissingVersion = errors.New("config: dependency has no pinned version")

	// ErrFloatingDependency reports a dependency version that names a range
	// or a moving target instead of one immutable release.
	ErrFloatingDependency = errors.New("config: dependency version is floating, not pinned")

	// ErrMissingDigest reports a dependency with no content digest, or one
	// that is not well-formed lowercase sha256 hex.
	ErrMissingDigest = errors.New("config: dependency has no content digest")

	// ErrDuplicateDependency reports two dependencies in one bundle sharing
	// a (kind, name).
	ErrDuplicateDependency = errors.New("config: duplicate dependency in bundle")

	// ErrRawCredential reports a credential reference that is not an opaque
	// reference (see CredentialRefPrefix): a bundle may name where a
	// credential lives, never the credential material itself.
	ErrRawCredential = errors.New("config: credential reference is not an opaque reference")

	// ErrEmptyBundleID reports a bundle with no id.
	ErrEmptyBundleID = errors.New("config: bundle id is empty")

	// ErrEmptyCompatibilityRange reports a bundle with no declared
	// compatibility range.
	ErrEmptyCompatibilityRange = errors.New("config: bundle compatibility range is empty")

	// ErrEmptySigner reports a bundle or signing call with no signer key id.
	ErrEmptySigner = errors.New("config: signer key id is empty")

	// ErrTamperedManifest reports a signed bundle whose recorded digest does
	// not match the digest recomputed from its own bundle content.
	ErrTamperedManifest = errors.New("config: manifest content does not match its recorded digest")

	// ErrInvalidSignature reports a signature that does not verify, is
	// malformed, or was produced/checked with a key of the wrong size.
	ErrInvalidSignature = errors.New("config: signature does not verify")

	// Registry validation causes.
	ErrInvalidObject            = errors.New("config: object kind is unspecified or unknown")
	ErrMissingOwner             = errors.New("config: object owner is missing")
	ErrMissingPhase             = errors.New("config: object phase is missing")
	ErrMissingScope             = errors.New("config: object scope is missing")
	ErrMissingEffectiveTime     = errors.New("config: object effective time is missing")
	ErrInvalidEffectiveInterval = errors.New("config: object effective interval is invalid")
	ErrAlreadyRegistered        = errors.New("config: object version is already registered")
	ErrUnresolvedDependency     = errors.New("config: dependency cannot be resolved")
	ErrDependencyDigestMismatch = errors.New("config: dependency digest does not match registry")
	ErrDependencyCycle          = errors.New("config: dependency closure contains a cycle")
	ErrScopeMismatch            = errors.New("config: dependency scope does not match bundle scope")
	ErrMissingTargetScope       = errors.New("config: bundle target scope is missing")
	ErrMissingRuntimeVersion    = errors.New("config: bundle minimum runtime version is missing")
)

// Error is the single error type this package returns. Unwrap exposes the
// sentinel cause for [errors.Is]; Op and Detail exist for diagnostics only
// and are never load-bearing for control flow.
type Error struct {
	Op     string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Detail != "" {
		msg = msg + ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}
