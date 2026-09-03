package privacy

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ErrNoticeInvalid is returned by [Notice.Validate] when a notice is missing
// evidence PRIV-002 requires before it can ever be presented or evaluated.
var ErrNoticeInvalid = errors.New("privacy: notice fails validation")

// Notice is one versioned, jurisdiction-scoped privacy notice: what purpose
// it covers, which data classes it discloses, which jurisdiction it was
// authored for, and (via [Notice.Digest]) the exact content this version
// presents.
//
// A Notice never authorizes anything by itself. [EvaluateAuthority] requires
// a [Presentation] proving this exact version was actually shown to a
// principal, and, because Mandatory is false here, an [OptionalProcessing]
// consent proving they agreed to the declared purpose.
//
// Mandatory marks a notice as covering a mandatory legal basis (statutory
// recordkeeping, tax reporting, and similar obligations governed by
// internal/governance/legal and PRIV-001) rather than an optional,
// consent-governed purpose. [EvaluateAuthority] refuses unconditionally for
// a Mandatory notice: this package's consent machinery is never a lawful
// substitute for a mandatory basis, so a caller cannot use it to authorize
// one even by supplying otherwise-complete Presentation/OptionalProcessing
// evidence. That is PRIV-002's REFACTOR clause ("mandatory legal basis and
// optional consent are never conflated"), enforced in code rather than left
// to caller discipline.
type Notice struct {
	ID            string
	Version       string
	Purpose       string
	DataClasses   []string
	Jurisdiction  string
	Locale        string
	Mandatory     bool
	EffectiveFrom values.Instant
	// EffectiveTo is the exclusive end of the notice's effective interval.
	// The zero (unset) value means open-ended.
	EffectiveTo values.Instant
}

// Validate reports whether the notice carries every field PRIV-002 evidence
// requires: an identity, a version, a declared purpose, at least one
// disclosed data class, a jurisdiction, a locale, and a well-formed
// effective interval.
func (n Notice) Validate() error {
	if n.ID == "" {
		return fmt.Errorf("%w: no id", ErrNoticeInvalid)
	}
	if n.Version == "" {
		return fmt.Errorf("%w: notice %q has no version", ErrNoticeInvalid, n.ID)
	}
	if n.Purpose == "" {
		return fmt.Errorf("%w: notice %q/%s has no purpose", ErrNoticeInvalid, n.ID, n.Version)
	}
	if len(n.DataClasses) == 0 {
		return fmt.Errorf("%w: notice %q/%s declares no data classes", ErrNoticeInvalid, n.ID, n.Version)
	}
	for i, dc := range n.DataClasses {
		if dc == "" {
			return fmt.Errorf("%w: notice %q/%s has an empty data class at index %d", ErrNoticeInvalid, n.ID, n.Version, i)
		}
	}
	if n.Jurisdiction == "" {
		return fmt.Errorf("%w: notice %q/%s has no jurisdiction", ErrNoticeInvalid, n.ID, n.Version)
	}
	if n.Locale == "" {
		return fmt.Errorf("%w: notice %q/%s has no locale", ErrNoticeInvalid, n.ID, n.Version)
	}
	if !n.EffectiveFrom.IsSet() {
		return fmt.Errorf("%w: notice %q/%s has no effective_from", ErrNoticeInvalid, n.ID, n.Version)
	}
	if n.EffectiveTo.IsSet() && !n.EffectiveFrom.Before(n.EffectiveTo) {
		return fmt.Errorf("%w: notice %q/%s effective_to does not follow effective_from", ErrNoticeInvalid, n.ID, n.Version)
	}
	return nil
}

// ActiveAt reports whether the notice is in force at asOf: on or after
// EffectiveFrom and, when EffectiveTo is set, strictly before it. An invalid
// notice or an unset asOf is never active.
func (n Notice) ActiveAt(asOf values.Instant) bool {
	if n.Validate() != nil || !asOf.IsSet() {
		return false
	}
	if asOf.Before(n.EffectiveFrom) {
		return false
	}
	if n.EffectiveTo.IsSet() && !asOf.Before(n.EffectiveTo) {
		return false
	}
	return true
}

// Digest is the canonical content digest of this exact notice version: the
// same id/version/purpose/data-classes/jurisdiction/locale/mandatory/
// effective-interval always hashes identically, and any difference --
// including a data-class reordering, since a notice's disclosed-category
// list is a published, ordered disclosure -- produces a different digest.
// [Presentation] binds this digest, not just the version string, precisely
// so a notice silently republished under an unchanged version number cannot
// stand in for what a principal actually saw.
func (n Notice) Digest() string {
	dst := appendFields(nil,
		"id", n.ID,
		"version", n.Version,
		"purpose", n.Purpose,
		"jurisdiction", n.Jurisdiction,
		"locale", n.Locale,
		"mandatory", strconv.FormatBool(n.Mandatory),
		"effective_from", n.EffectiveFrom.String(),
		"effective_to", n.EffectiveTo.String(),
	)
	dst = appendStringSlice(dst, "data_class", n.DataClasses)
	return digestHex(dst)
}
