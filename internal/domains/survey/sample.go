// SURVEY-002: freeze a campaign's population/sample from a bound
// population, under a declared sampling rule, into an immutable
// CampaignSample. A sample never carries a copy of engine-owned population
// membership by accident: it cites the exact frozen population snapshot it
// was drawn from (definition id, revision version and digest -- the same
// triple internal/engines/cycle's PopulationBinding cites, so this package
// never needs to import the population engine to prove which snapshot it
// means). Freezing is a one-way operation: two calls with byte-identical
// inputs produce byte-identical digests, and there is no exported way to
// change a CampaignSample's membership after it is built.
package survey

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidPopulationBindingRef = errors.New("survey: campaign sample requires a valid population binding reference")
	ErrInvalidSamplingRule         = errors.New("survey: invalid sampling rule")
	ErrInvalidCampaignSample       = errors.New("survey: invalid campaign sample")
	ErrSampleMembershipMismatch    = errors.New("survey: campaign sample membership does not match its protection flag")
)

// SamplingKind identifies how a campaign's respondent set is chosen from
// its bound population. The vocabulary is closed: WHOLE takes every member
// of the binding, STRATIFIED draws a declared quota from each named
// stratum, and RANDOM draws a declared count using a declared seed so the
// draw is reproducible from the rule alone.
type SamplingKind string

const (
	SamplingKindWhole      SamplingKind = "WHOLE"
	SamplingKindStratified SamplingKind = "STRATIFIED"
	SamplingKindRandom     SamplingKind = "RANDOM"
)

// Valid reports whether k is one of the declared sampling kinds.
func (k SamplingKind) Valid() bool {
	return k == SamplingKindWhole || k == SamplingKindStratified || k == SamplingKindRandom
}

// StratumQuota declares how many members a STRATIFIED sample draws from one
// named stratum.
type StratumQuota struct {
	Stratum string
	Size    int
}

// NonresponsePolicy is the declared, explicit handling of a sampled member
// who never responds. There is no implicit default: SamplingRule.Validate
// rejects an unrecognized or unset policy.
type NonresponsePolicy string

const (
	NonresponsePolicyExcludeFromAnalysis NonresponsePolicy = "EXCLUDE_FROM_ANALYSIS"
	NonresponsePolicyFollowUpReminder    NonresponsePolicy = "FOLLOW_UP_REMINDER"
	NonresponsePolicyEscalate            NonresponsePolicy = "ESCALATE"
)

// Valid reports whether p is one of the declared nonresponse policies.
func (p NonresponsePolicy) Valid() bool {
	switch p {
	case NonresponsePolicyExcludeFromAnalysis, NonresponsePolicyFollowUpReminder, NonresponsePolicyEscalate:
		return true
	default:
		return false
	}
}

// SamplingRule declares how a campaign's respondent set is chosen from a
// bound population, plus the explicit nonresponse handling for whoever is
// sampled but never answers.
type SamplingRule struct {
	Kind        SamplingKind
	Seed        uint64         // required for RANDOM; ignored otherwise
	SampleSize  int            // required for RANDOM; ignored otherwise
	Strata      []StratumQuota // required for STRATIFIED; ignored otherwise
	Nonresponse NonresponsePolicy
}

// Validate reports whether the rule is complete and internally consistent.
func (r SamplingRule) Validate() error {
	if !r.Kind.Valid() {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidSamplingRule, r.Kind)
	}
	if !r.Nonresponse.Valid() {
		return fmt.Errorf("%w: nonresponse policy is required", ErrInvalidSamplingRule)
	}
	switch r.Kind {
	case SamplingKindRandom:
		if r.SampleSize <= 0 {
			return fmt.Errorf("%w: RANDOM requires a positive sample size", ErrInvalidSamplingRule)
		}
	case SamplingKindStratified:
		if len(r.Strata) == 0 {
			return fmt.Errorf("%w: STRATIFIED requires at least one stratum quota", ErrInvalidSamplingRule)
		}
		seen := make(map[string]bool, len(r.Strata))
		for i, s := range r.Strata {
			if s.Stratum == "" {
				return fmt.Errorf("%w: stratum %d has no name", ErrInvalidSamplingRule, i)
			}
			if s.Size <= 0 {
				return fmt.Errorf("%w: stratum %q requires a positive size", ErrInvalidSamplingRule, s.Stratum)
			}
			if seen[s.Stratum] {
				return fmt.Errorf("%w: duplicate stratum %q", ErrInvalidSamplingRule, s.Stratum)
			}
			seen[s.Stratum] = true
		}
	}
	return nil
}

// Canonical returns the deterministic byte encoding of the rule, or nil
// when invalid.
func (r SamplingRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.survey.SamplingRule", 1).
		String("kind", string(r.Kind)).
		String("nonresponse", string(r.Nonresponse))

	switch r.Kind {
	case SamplingKindRandom:
		w.Int("seed", int64(r.Seed)).Int("sample_size", int64(r.SampleSize))
	case SamplingKindStratified:
		sorted := append([]StratumQuota(nil), r.Strata...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Stratum < sorted[j].Stratum })
		w.Count("strata", len(sorted))
		for _, s := range sorted {
			w.String("stratum", s.Stratum).Int("size", int64(s.Size))
		}
	}

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical encoding.
func (r SamplingRule) Digest() string {
	raw := r.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// PopulationBindingRef cites the exact frozen population snapshot a
// campaign draws its sample from -- by definition id, revision version and
// digest -- never by importing the population engine or copying its
// membership into this package.
type PopulationBindingRef struct {
	DefinitionID    string
	RevisionVersion string
	Digest          string
}

// Validate reports whether the reference names a complete snapshot.
func (r PopulationBindingRef) Validate() error {
	if r.DefinitionID == "" || r.RevisionVersion == "" || r.Digest == "" {
		return ErrInvalidPopulationBindingRef
	}
	return nil
}

// String returns "<definition id>@<revision version>#<digest>".
func (r PopulationBindingRef) String() string {
	return r.DefinitionID + "@" + r.RevisionVersion + "#" + r.Digest
}

// CampaignSample is the immutable, frozen result of applying a SamplingRule
// to a PopulationBindingRef for one campaign: exactly which members (or,
// when membership itself is protected, the fact that it is withheld) are in
// the campaign's respondent set, under one canonical digest. Membership
// cannot change after Freeze: a different member set, a different rule or a
// different binding produces a distinct CampaignSample with a distinct
// digest, never a mutation of an existing one.
type CampaignSample struct {
	CampaignID string
	Binding    PopulationBindingRef
	Rule       SamplingRule
	// MemberIDs is the exact sorted set of subject-ref text the sample
	// includes. It is empty when MembershipProtected is true: a protected
	// sample never carries a raw member list, even an empty-looking one a
	// caller could mistake for "zero members" instead of "not disclosed".
	MemberIDs           []string
	MembershipProtected bool
	Count               int
	FrozenAt            values.Instant
	Digest              string
}

// MemberIDList returns a defensive copy of the frozen member IDs.
func (s CampaignSample) MemberIDList() []string {
	cp := make([]string, len(s.MemberIDs))
	copy(cp, s.MemberIDs)
	return cp
}

// FreezeSample binds rule's outcome against binding into an immutable
// CampaignSample for campaignID. When membershipProtected is true,
// memberIDs must be empty and count is taken as given (the caller's own
// disclosure decision); when false, memberIDs must be a non-empty,
// duplicate-free set and count is always derived from it, never taken
// on faith. Two calls with byte-identical inputs produce byte-identical
// digests; any change to the campaign, the binding, the rule or the
// resolved membership changes the digest.
func FreezeSample(campaignID string, binding PopulationBindingRef, rule SamplingRule, memberIDs []string, membershipProtected bool, count int, frozenAt values.Instant) (CampaignSample, error) {
	if campaignID == "" {
		return CampaignSample{}, fmt.Errorf("%w: campaign id is required", ErrInvalidCampaignSample)
	}
	if err := binding.Validate(); err != nil {
		return CampaignSample{}, err
	}
	if err := rule.Validate(); err != nil {
		return CampaignSample{}, err
	}
	if err := frozenAt.Validate(); err != nil {
		return CampaignSample{}, fmt.Errorf("%w: frozen_at: %v", ErrInvalidCampaignSample, err)
	}
	if membershipProtected && len(memberIDs) != 0 {
		return CampaignSample{}, fmt.Errorf("%w: protected membership must not carry a raw member list", ErrSampleMembershipMismatch)
	}
	if !membershipProtected && len(memberIDs) == 0 {
		return CampaignSample{}, fmt.Errorf("%w: disclosed membership requires at least one member id", ErrSampleMembershipMismatch)
	}
	if membershipProtected && count < 0 {
		return CampaignSample{}, fmt.Errorf("%w: protected membership requires a non-negative count", ErrSampleMembershipMismatch)
	}

	var ids []string
	resolvedCount := count
	if !membershipProtected {
		ids = append([]string(nil), memberIDs...)
		sort.Strings(ids)
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if id == "" {
				return CampaignSample{}, fmt.Errorf("%w: empty member id", ErrInvalidCampaignSample)
			}
			if seen[id] {
				return CampaignSample{}, fmt.Errorf("%w: duplicate member id %q", ErrInvalidCampaignSample, id)
			}
			seen[id] = true
		}
		resolvedCount = len(ids)
	}

	sample := CampaignSample{
		CampaignID:          campaignID,
		Binding:             binding,
		Rule:                rule,
		MemberIDs:           ids,
		MembershipProtected: membershipProtected,
		Count:               resolvedCount,
		FrozenAt:            frozenAt,
	}

	ruleCanon := rule.Canonical()
	if ruleCanon == nil {
		return CampaignSample{}, ErrInvalidSamplingRule
	}

	w := canonicalbytes.New("hcmnext.domains.survey.CampaignSample", 1).
		String("campaign_id", sample.CampaignID).
		String("binding", sample.Binding.String()).
		Bool("membership_protected", sample.MembershipProtected).
		Int("count", int64(sample.Count)).
		Value("frozen_at", sample.FrozenAt).
		Field("rule", ruleCanon).
		SortedStrings("member", ids)

	raw, err := w.Bytes()
	if err != nil {
		return CampaignSample{}, fmt.Errorf("%w: digest: %v", ErrInvalidCampaignSample, err)
	}
	sample.Digest = canonicalbytes.Digest(raw)
	return sample, nil
}
