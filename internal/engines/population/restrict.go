// POP-004: apply AuthZ, privacy, organization and purpose restrictions as
// already-decided input, never as a policy this package computes itself.
// Deciding who may see what is TRUST/PRIV territory; this package only
// applies a RestrictionDecision handed to it and evidences what it did.
package population

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Restriction errors. All are matchable with errors.Is.
var (
	// ErrRestrictionVersion is returned when a decision cites no policy version
	// for any of its four restriction sources.
	ErrRestrictionVersion = errors.New("population: restriction decision requires policy versions")
)

// RestrictionReason names why one subject or the aggregate count was
// restricted. It is evidence vocabulary: it appears in RestrictedResult's
// Evidence trail, never as a value substituted into the disclosed membership.
type RestrictionReason uint8

// Restriction reasons.
const (
	RestrictionUnspecified RestrictionReason = iota
	RestrictionUnauthorizedPerson
	RestrictionUnauthorizedField
	RestrictionCrossOrganization
	RestrictionCountSuppressed
)

var restrictionReasonWire = map[RestrictionReason]string{
	RestrictionUnauthorizedPerson: "UNAUTHORIZED_PERSON",
	RestrictionUnauthorizedField:  "UNAUTHORIZED_FIELD",
	RestrictionCrossOrganization:  "CROSS_ORGANIZATION",
	RestrictionCountSuppressed:    "COUNT_SUPPRESSED",
}

// String returns the wire token.
func (r RestrictionReason) String() string {
	if s, ok := restrictionReasonWire[r]; ok {
		return s
	}
	return "RESTRICTION_UNSPECIFIED"
}

// PolicyVersions cites the exact version of every restriction source a
// decision was made under. All four are required: a restriction decision that
// does not say which AuthZ, privacy, organization and purpose policy versions
// produced it cannot be replayed or audited.
type PolicyVersions struct {
	AuthZVersion        string
	PrivacyVersion      string
	OrganizationVersion string
	PurposeVersion      string
}

// Validate reports whether every version is cited.
func (p PolicyVersions) Validate() error {
	if p.AuthZVersion == "" || p.PrivacyVersion == "" || p.OrganizationVersion == "" || p.PurposeVersion == "" {
		return ErrRestrictionVersion
	}
	return nil
}

// RestrictionDecision is the already-decided input POP-004 applies. Every
// field is a decision this package receives, never one it computes: AuthZ,
// privacy, organization scope and purpose enforcement are owned elsewhere.
type RestrictionDecision struct {
	Versions PolicyVersions
	// DeniedSubjects lists subjects the caller is not authorized to see at
	// all, keyed by canonical entity-ref text, with the reason recorded for
	// evidence. A denied subject is removed from disclosed membership exactly
	// as if it were never a candidate; the reason is never returned to the
	// original caller, only into RestrictedResult.Evidence for an authorized
	// reviewer.
	DeniedSubjects map[string]RestrictionReason
	// CrossOrganizationSubjects lists subjects the definition's own criteria
	// selected but whose organization falls outside the decision's authorized
	// organization scope.
	CrossOrganizationSubjects map[string]bool
	// DiscloseMembership reports whether the caller may see the member list at
	// all. When false, Members is empty regardless of resolution size.
	DiscloseMembership bool
	// DiscloseCount reports whether the caller may see any count, exact or
	// banded, when membership itself is not disclosed.
	DiscloseCount bool
}

// Evidence records one restriction actually applied, for an authorized
// reviewer's audit trail. It is never serialized back to the original caller
// of RestrictedResult.Members.
type Evidence struct {
	Subject  values.EntityRef
	Reason   RestrictionReason
	Versions PolicyVersions
}

// RestrictedResult is a Result with restrictions applied. Members contains
// only subjects the decision authorizes to disclose; a denied or cross-org
// subject is simply absent, indistinguishable from a subject that was never a
// candidate, so a caller cannot run an inference oracle by comparing "denied"
// against "excluded".
type RestrictedResult struct {
	Members      []Member
	Completeness Completeness
	Count        values.Presence[int]
	Evidence     []Evidence
	Versions     PolicyVersions
}

// ApplyRestrictions filters result under decision. It never invents a
// restriction decision: an incomplete PolicyVersions fails closed.
func ApplyRestrictions(result Result, decision RestrictionDecision) (RestrictedResult, error) {
	if err := decision.Versions.Validate(); err != nil {
		return RestrictedResult{}, err
	}

	kept := make([]Member, 0, len(result.Members))
	var evidence []Evidence
	completeness := result.Completeness
	for _, m := range result.Members {
		key := m.Subject.String()
		if reason, denied := decision.DeniedSubjects[key]; denied {
			evidence = append(evidence, Evidence{Subject: m.Subject, Reason: reason, Versions: decision.Versions})
			continue
		}
		if decision.CrossOrganizationSubjects[key] {
			evidence = append(evidence, Evidence{Subject: m.Subject, Reason: RestrictionCrossOrganization, Versions: decision.Versions})
			continue
		}
		kept = append(kept, m)
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].Subject.String() < evidence[j].Subject.String() })

	restricted := RestrictedResult{Completeness: completeness, Evidence: evidence, Versions: decision.Versions}

	if decision.DiscloseMembership {
		restricted.Members = kept
	}

	switch {
	case decision.DiscloseMembership:
		restricted.Count = values.Value(len(kept))
	case decision.DiscloseCount:
		restricted.Count = values.Value(len(kept))
	default:
		restricted.Count = values.Redacted[int](RestrictionCountSuppressed.String())
	}

	return restricted, nil
}

// Canonical returns the canonical byte encoding of the restricted result's
// disclosed membership, or nil when Count carries no comparable state. It
// never includes Evidence: the digest describes what the caller can see, not
// the audit trail of what they cannot.
func (r RestrictedResult) Canonical() []byte {
	ids := make([]string, 0, len(r.Members))
	for _, m := range r.Members {
		if err := m.Subject.Validate(); err != nil {
			return nil
		}
		ids = append(ids, fmt.Sprintf("%s|%s", m.Subject.String(), m.Outcome.String()))
	}
	sort.Strings(ids)
	countText := r.Count.State().String()
	if v, ok := r.Count.Get(); ok {
		countText = fmt.Sprintf("VALUE:%d", v)
	}
	w := writerFor("hcmnext.engines.population.RestrictedResult").
		String("completeness", r.Completeness.String()).
		String("count", countText).
		Count("members", len(ids))
	for _, id := range ids {
		w.String("member", id)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
