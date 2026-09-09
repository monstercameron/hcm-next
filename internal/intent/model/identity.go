package model

import (
	"errors"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes for exact identity linkage (MODEL-022). Classify with
// [errors.Is]; never by matching strings.
var (
	// ErrInvalidIdentityLink reports an IdentityLink that cannot be
	// published: no link reference, no external identity, no canonical
	// reference, no match key, less-than-exact confidence, no effective
	// interval, no evidence or no source authority.
	ErrInvalidIdentityLink = errors.New("model: invalid identity link")

	// ErrUnknownIdentityLink reports a lookup naming an external identity
	// with no declared link at all.
	ErrUnknownIdentityLink = errors.New("model: unknown identity link")

	// ErrIdentityAmbiguous reports two or more links for the same external
	// identity, effective at the same instant, that resolve to different
	// canonical references: a collision, never auto-resolved by guessing.
	ErrIdentityAmbiguous = errors.New("model: AMBIGUOUS identity resolution")

	// ErrIdentityOutOfEffectiveRange reports an external identity with
	// declared links, none of which is effective at the requested instant.
	ErrIdentityOutOfEffectiveRange = errors.New("model: identity link is outside its effective window")

	// ErrIdentityCrossTenantPolicy reports a resolution or merge whose
	// tenant or purpose scope does not match the link's declared scope: a
	// redirect must never cross tenant or purpose policy.
	ErrIdentityCrossTenantPolicy = errors.New("model: identity resolution crosses tenant or purpose policy")

	// ErrIdentityDoNotMerge reports a merge attempt between two canonical
	// references a [DoNotMergeConstraint] protects: a false auto-merge is
	// always refused, never overridden implicitly.
	ErrIdentityDoNotMerge = errors.New("model: identity merge blocked by a do-not-merge constraint")

	// ErrInvalidIdentityMerge reports a merge missing evidence, a
	// provenance reference, or attempting to merge a reference into itself.
	ErrInvalidIdentityMerge = errors.New("model: invalid identity merge")

	// ErrInvalidIdentitySeparation reports a separation with no prior merge
	// to reverse, or missing evidence/provenance.
	ErrInvalidIdentitySeparation = errors.New("model: invalid identity separation")
)

// exactConfidence is the only confidence value an exact identity link may
// carry. MODEL-022 is scoped to exact, declared match keys only (Gate A);
// fuzzy candidate scoring is out of scope, so confidence is not a sliding
// scale here — it is a validated invariant. Any other value is malformed
// input, not a lower-quality match.
const exactConfidence = 1.0

// IdentityLink asserts that one external identity is, by an exact declared
// match key, the same subject as one canonical values identity (MODEL-022).
// Every link carries its own evidence and source authority: no link is ever
// inferred without both.
type IdentityLink struct {
	LinkRef string

	ExternalSystem string
	ExternalID     string

	// CanonicalRef names the canonical `values` identity this external
	// identity resolves to, e.g. a Person reference.
	CanonicalRef string

	// MatchKey names the declared field or field-set the exact match was
	// made on, e.g. "EMPLOYEE_NUMBER" or "SSN_HASH+DOB". A link with no
	// declared match key is exactly the "no fuzzy matching" RED case: there
	// is nothing to audit.
	MatchKey string

	// Confidence must be exactly [exactConfidence]: an exact match is
	// binary, not scored.
	Confidence float64

	Effective values.EffectiveInterval

	// TenantRef and PurposeScope bound where this link may be used. A
	// resolution or redirect that crosses either is a policy violation, not
	// a data problem.
	TenantRef    string
	PurposeScope string

	EvidenceRef        string
	SourceAuthorityRef string
}

// Validate rejects an identity link missing an identity, canonical
// reference, match key, exact confidence, effective interval, tenant/purpose
// scope, evidence or source authority.
func (l IdentityLink) Validate() error {
	if l.LinkRef == "" {
		return newError("IdentityLink.Validate", "link_ref", ErrInvalidIdentityLink,
			"link carries no reference")
	}
	if l.ExternalSystem == "" || l.ExternalID == "" {
		return newError("IdentityLink.Validate", "external_identity", ErrInvalidIdentityLink,
			"%s declares an incomplete external identity (system=%q id=%q)",
			l.LinkRef, l.ExternalSystem, l.ExternalID)
	}
	if l.CanonicalRef == "" {
		return newError("IdentityLink.Validate", "canonical_ref", ErrInvalidIdentityLink,
			"%s names no canonical identity", l.LinkRef)
	}
	if l.MatchKey == "" {
		return newError("IdentityLink.Validate", "match_key", ErrInvalidIdentityLink,
			"%s declares no exact match key", l.LinkRef)
	}
	if l.Confidence != exactConfidence {
		return newError("IdentityLink.Validate", "confidence", ErrInvalidIdentityLink,
			"%s has confidence %v, not the exact-match value %v: fuzzy matching is out of scope",
			l.LinkRef, l.Confidence, exactConfidence)
	}
	if err := l.Effective.Validate(); err != nil {
		return newError("IdentityLink.Validate", "effective", ErrInvalidIdentityLink,
			"%s has no valid effective interval: %v", l.LinkRef, err)
	}
	if l.TenantRef == "" {
		return newError("IdentityLink.Validate", "tenant_ref", ErrInvalidIdentityLink,
			"%s names no tenant", l.LinkRef)
	}
	if l.PurposeScope == "" {
		return newError("IdentityLink.Validate", "purpose_scope", ErrInvalidIdentityLink,
			"%s names no purpose scope", l.LinkRef)
	}
	if l.EvidenceRef == "" {
		return newError("IdentityLink.Validate", "evidence_ref", ErrInvalidIdentityLink,
			"%s names no evidence", l.LinkRef)
	}
	if l.SourceAuthorityRef == "" {
		return newError("IdentityLink.Validate", "source_authority_ref", ErrInvalidIdentityLink,
			"%s names no source authority", l.LinkRef)
	}
	return nil
}

func externalIDKey(system, id string) string { return system + "\x00" + id }

// IdentityResolution is the answer to "which canonical identity does this
// external identity mean?" A person role such as Candidate, Worker or former
// employment is not part of this identity: MODEL-022's REFACTOR clause keeps
// those as concurrent roles/relationships a caller resolves separately,
// layered on top of the one canonical reference this returns.
type IdentityResolution struct {
	CanonicalRef string
	LinkRef      string
	Confidence   float64
	EvidenceRef  string
}

// ResolveIdentity resolves one external identity to exactly one canonical
// reference at asOf, scoped to tenantRef and purposeScope.
//
// It rejects: an external identity with no declared link at all
// ([ErrUnknownIdentityLink]); links all outside their effective window at
// asOf ([ErrIdentityOutOfEffectiveRange]); two or more simultaneously
// effective links resolving to different canonical references — a collision
// ([ErrIdentityAmbiguous]); and a link whose declared tenant or purpose scope
// does not match the caller's ([ErrIdentityCrossTenantPolicy]), which is
// enforced before ambiguity so a caller can never learn of an out-of-scope
// collision it has no business seeing.
func ResolveIdentity(links []IdentityLink, externalSystem, externalID string, asOf values.Instant, tenantRef, purposeScope string) (IdentityResolution, error) {
	key := externalIDKey(externalSystem, externalID)
	var any []IdentityLink
	for _, l := range links {
		if externalIDKey(l.ExternalSystem, l.ExternalID) == key {
			any = append(any, l)
		}
	}
	if len(any) == 0 {
		return IdentityResolution{}, newError("ResolveIdentity", "external_identity", ErrUnknownIdentityLink,
			"%s/%s has no declared identity link", externalSystem, externalID)
	}

	var inWindow []IdentityLink
	for _, l := range any {
		if ok, err := l.Effective.ContainsInstant(asOf); err == nil && ok {
			inWindow = append(inWindow, l)
		}
	}
	if len(inWindow) == 0 {
		return IdentityResolution{}, newError("ResolveIdentity", "effective", ErrIdentityOutOfEffectiveRange,
			"%s/%s has no link effective at %s", externalSystem, externalID, asOf)
	}

	for _, l := range inWindow {
		if l.TenantRef != tenantRef || l.PurposeScope != purposeScope {
			return IdentityResolution{}, newError("ResolveIdentity", "tenant_purpose", ErrIdentityCrossTenantPolicy,
				"%s resolves under tenant %q purpose %q, not the requested tenant %q purpose %q",
				l.LinkRef, l.TenantRef, l.PurposeScope, tenantRef, purposeScope)
		}
	}

	distinct := map[string]IdentityLink{}
	for _, l := range inWindow {
		if existing, ok := distinct[l.CanonicalRef]; !ok || l.LinkRef < existing.LinkRef {
			distinct[l.CanonicalRef] = l
		}
	}
	if len(distinct) > 1 {
		refs := make([]string, 0, len(distinct))
		for ref := range distinct {
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		return IdentityResolution{}, newError("ResolveIdentity", "canonical_ref", ErrIdentityAmbiguous,
			"%s/%s resolves to %d canonical identities: %v", externalSystem, externalID, len(distinct), refs)
	}

	var winner IdentityLink
	for _, l := range distinct {
		winner = l
	}
	return IdentityResolution{
		CanonicalRef: winner.CanonicalRef,
		LinkRef:      winner.LinkRef,
		Confidence:   winner.Confidence,
		EvidenceRef:  winner.EvidenceRef,
	}, nil
}

// DoNotMergeConstraint records a governance decision that two canonical
// references must never be merged even if a future collision suggests it.
type DoNotMergeConstraint struct {
	ConstraintRef string
	RefA, RefB    string
	Reason        string
	AuthorityRef  string
}

// Validate rejects a do-not-merge constraint missing an identity, both
// protected references, a reason or an authority.
func (c DoNotMergeConstraint) Validate() error {
	if c.ConstraintRef == "" {
		return newError("DoNotMergeConstraint.Validate", "constraint_ref", ErrInvalidIdentityLink,
			"constraint carries no reference")
	}
	if c.RefA == "" || c.RefB == "" {
		return newError("DoNotMergeConstraint.Validate", "refs", ErrInvalidIdentityLink,
			"%s does not name both protected references", c.ConstraintRef)
	}
	if c.Reason == "" {
		return newError("DoNotMergeConstraint.Validate", "reason", ErrInvalidIdentityLink,
			"%s names no reason", c.ConstraintRef)
	}
	if c.AuthorityRef == "" {
		return newError("DoNotMergeConstraint.Validate", "authority_ref", ErrInvalidIdentityLink,
			"%s names no authority", c.ConstraintRef)
	}
	return nil
}

// blocks reports whether c protects the unordered pair (a, b).
func (c DoNotMergeConstraint) blocks(a, b string) bool {
	return (c.RefA == a && c.RefB == b) || (c.RefA == b && c.RefB == a)
}

// FormerIdentifierRedirect is the append-only record left behind when a
// canonical reference is retired in favor of another: lookups against
// FormerRef must redirect to RedirectsTo without erasing the fact that
// FormerRef once existed independently.
type FormerIdentifierRedirect struct {
	FormerRef   string
	RedirectsTo string
	At          values.Instant
	EvidenceRef string
}

// IdentityMergeResult is the outcome of merging two canonical identities: the
// surviving reference, the redirect left for the retired one, and the
// provenance edge preserving how the merge was decided (MODEL-022 GREEN —
// "merge and separation preserve lineage").
type IdentityMergeResult struct {
	SurvivingRef  string
	RetiredRef    string
	Redirect      FormerIdentifierRedirect
	ProvenanceRef string
}

// MergeIdentities merges retiredRef into survivingRef.
//
// It refuses: merging a reference into itself; a merge protected by a
// [DoNotMergeConstraint] between the two references — a false auto-merge is
// always blocked, never silently permitted ([ErrIdentityDoNotMerge]); a merge
// whose two sides were resolved under different tenants or purpose scopes,
// which would let a redirect cross policy it has no authority to cross
// ([ErrIdentityCrossTenantPolicy]); and a merge with no evidence or
// provenance reference ([ErrInvalidIdentityMerge]).
func MergeIdentities(constraints []DoNotMergeConstraint, survivingRef, retiredRef, survivingTenant, retiredTenant, survivingPurpose, retiredPurpose string, at values.Instant, evidenceRef, provenanceRef string) (IdentityMergeResult, error) {
	if survivingRef == "" || retiredRef == "" {
		return IdentityMergeResult{}, newError("MergeIdentities", "refs", ErrInvalidIdentityMerge,
			"merge requires both a surviving and a retired reference")
	}
	if survivingRef == retiredRef {
		return IdentityMergeResult{}, newError("MergeIdentities", "refs", ErrInvalidIdentityMerge,
			"%s cannot be merged into itself", survivingRef)
	}
	for _, c := range constraints {
		if err := c.Validate(); err != nil {
			return IdentityMergeResult{}, err
		}
		if c.blocks(survivingRef, retiredRef) {
			return IdentityMergeResult{}, newError("MergeIdentities", "constraint", ErrIdentityDoNotMerge,
				"%s blocks merging %s and %s: %s", c.ConstraintRef, survivingRef, retiredRef, c.Reason)
		}
	}
	if survivingTenant != retiredTenant || survivingPurpose != retiredPurpose {
		return IdentityMergeResult{}, newError("MergeIdentities", "tenant_purpose", ErrIdentityCrossTenantPolicy,
			"merge of %s and %s crosses tenant (%q vs %q) or purpose (%q vs %q) policy",
			survivingRef, retiredRef, survivingTenant, retiredTenant, survivingPurpose, retiredPurpose)
	}
	if !at.IsSet() {
		return IdentityMergeResult{}, newError("MergeIdentities", "at", ErrInvalidIdentityMerge,
			"merge of %s and %s carries no timestamp", survivingRef, retiredRef)
	}
	if evidenceRef == "" {
		return IdentityMergeResult{}, newError("MergeIdentities", "evidence_ref", ErrInvalidIdentityMerge,
			"merge of %s and %s carries no evidence", survivingRef, retiredRef)
	}
	if provenanceRef == "" {
		return IdentityMergeResult{}, newError("MergeIdentities", "provenance_ref", ErrInvalidIdentityMerge,
			"merge of %s and %s carries no provenance edge", survivingRef, retiredRef)
	}
	return IdentityMergeResult{
		SurvivingRef: survivingRef,
		RetiredRef:   retiredRef,
		Redirect: FormerIdentifierRedirect{
			FormerRef:   retiredRef,
			RedirectsTo: survivingRef,
			At:          at,
			EvidenceRef: evidenceRef,
		},
		ProvenanceRef: provenanceRef,
	}, nil
}

// IdentitySeparationResult is the append-only reversal of a prior merge: the
// original [IdentityMergeResult] is never edited or erased, a new redirect
// record supersedes it by pointing the formerly-retired reference back to
// itself.
type IdentitySeparationResult struct {
	RestoredRef   string
	Redirect      FormerIdentifierRedirect
	ProvenanceRef string
}

// SeparateIdentities reverses a prior merge by appending a superseding
// redirect that restores RetiredRef as an independent canonical identity.
// The original merge record passed in is never mutated: unlink is append-only
// supersession, not deletion.
//
// It rejects a separation with no prior merge to reverse, or missing
// evidence or provenance.
func SeparateIdentities(original IdentityMergeResult, at values.Instant, evidenceRef, provenanceRef string) (IdentitySeparationResult, error) {
	if original.SurvivingRef == "" || original.RetiredRef == "" {
		return IdentitySeparationResult{}, newError("SeparateIdentities", "original", ErrInvalidIdentitySeparation,
			"separation names no prior merge to reverse")
	}
	if !at.IsSet() {
		return IdentitySeparationResult{}, newError("SeparateIdentities", "at", ErrInvalidIdentitySeparation,
			"separation of %s from %s carries no timestamp", original.RetiredRef, original.SurvivingRef)
	}
	if evidenceRef == "" {
		return IdentitySeparationResult{}, newError("SeparateIdentities", "evidence_ref", ErrInvalidIdentitySeparation,
			"separation of %s from %s carries no evidence", original.RetiredRef, original.SurvivingRef)
	}
	if provenanceRef == "" {
		return IdentitySeparationResult{}, newError("SeparateIdentities", "provenance_ref", ErrInvalidIdentitySeparation,
			"separation of %s from %s carries no provenance edge", original.RetiredRef, original.SurvivingRef)
	}
	return IdentitySeparationResult{
		RestoredRef: original.RetiredRef,
		Redirect: FormerIdentifierRedirect{
			FormerRef:   original.RetiredRef,
			RedirectsTo: original.RetiredRef,
			At:          at,
			EvidenceRef: evidenceRef,
		},
		ProvenanceRef: provenanceRef,
	}, nil
}
