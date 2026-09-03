package model

import (
	"errors"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Sentinel causes for governed reference-data releases (MODEL-018). Classify
// with [errors.Is]; never by matching strings.
var (
	// ErrInvalidReferenceRelease reports a ReferenceRelease that cannot be
	// published: no reference, no dataset, no digest, or an undeclared state.
	ErrInvalidReferenceRelease = errors.New("model: invalid reference-data release")

	// ErrUnknownReferenceReleaseTransition reports a release-state transition
	// the reference-data lifecycle profile does not declare.
	ErrUnknownReferenceReleaseTransition = errors.New("model: undeclared reference-data release transition")

	// ErrInvalidReferenceValue reports a ReferenceValue missing a release,
	// dataset, code, effective interval or status.
	ErrInvalidReferenceValue = errors.New("model: invalid reference-data value")

	// ErrUnknownReferenceValue reports a lookup naming a code the compiled
	// dataset does not publish at all (MODEL-019's UNKNOWN vocabulary applies
	// here too: this is not a guess, it is a typed refusal).
	ErrUnknownReferenceValue = errors.New("model: unknown reference-data value")

	// ErrReferenceValueRetired reports a lookup resolving to a code whose
	// status is DEPRECATED or RETIRED: simulation must reject a retired job,
	// grade, currency, jurisdiction or reason code rather than silently using
	// it.
	ErrReferenceValueRetired = errors.New("model: reference-data value is deprecated or retired")

	// ErrReferenceValueOutOfEffectiveRange reports a lookup asOf a date
	// outside every candidate value's effective window.
	ErrReferenceValueOutOfEffectiveRange = errors.New("model: reference-data value is outside its effective window")

	// ErrReferenceDatasetNotReleased reports a lookup against a dataset whose
	// governing release has never reached RELEASED: an unversioned global
	// dataset is exactly the MODEL-018 RED case, not a resolvable answer.
	ErrReferenceDatasetNotReleased = errors.New("model: reference-data dataset carries no RELEASED release")
)

// ReferenceDatasetKind names one governed code-set family: the shared,
// tenant-independent vocabularies a Position, Assignment or Compensation
// change references by code rather than owning as free text.
//
// Position itself is deliberately absent from this list (MODEL-018
// REFACTOR): a Position is capacity-bearing, aggregate-owned business state,
// not a governed code set. A position's job, grade, location and pay-band
// fields are references *into* these datasets; the Position record that
// carries them stays in the ordinary aggregate/entity model in catalog.go.
type ReferenceDatasetKind string

// Reference dataset kinds.
const (
	DatasetJobFamily    ReferenceDatasetKind = "JOB_FAMILY"
	DatasetGrade        ReferenceDatasetKind = "GRADE"
	DatasetCurrency     ReferenceDatasetKind = "CURRENCY"
	DatasetJurisdiction ReferenceDatasetKind = "JURISDICTION"
	DatasetLocation     ReferenceDatasetKind = "LOCATION"
	DatasetReasonCode   ReferenceDatasetKind = "REASON_CODE"
)

// Valid reports whether k is one of the six declared dataset kinds.
func (k ReferenceDatasetKind) Valid() bool {
	switch k {
	case DatasetJobFamily, DatasetGrade, DatasetCurrency, DatasetJurisdiction, DatasetLocation, DatasetReasonCode:
		return true
	default:
		return false
	}
}

// dimensionReferenceRelease is the [lifecycle.Dimension] this file registers
// its own profile under, following the exact pattern
// [dimensionSchemaRelease] in schemarelease.go establishes: [lifecycle.Profile]
// is dimension-generic, so the shared reachability/terminal/retention-class
// validation in internal/intent/lifecycle applies here without copying it.
const dimensionReferenceRelease lifecycle.Dimension = "ReferenceReleaseState"

// Reference-data release states.
const (
	RefReleaseDraft      lifecycle.StateID = "DRAFT"
	RefReleaseValidated  lifecycle.StateID = "VALIDATED"
	RefReleaseApproved   lifecycle.StateID = "APPROVED"
	RefReleaseReleased   lifecycle.StateID = "RELEASED"
	RefReleaseDeprecated lifecycle.StateID = "DEPRECATED"
)

const retentionReferenceDataLifecycle = "REFERENCE_DATA_LIFECYCLE"

// ReferenceReleaseLifecycleProfile is the compiled-in reference-data release
// lifecycle: draft -> validated -> approved -> released -> deprecated. It is
// the RELEASED/DEPRECATED analogue of [ReleaseLifecycleProfile], reusing the
// same shared dimension-generic [lifecycle.Profile] machinery.
func ReferenceReleaseLifecycleProfile() lifecycle.Profile {
	governed := map[[2]lifecycle.StateID]bool{
		{RefReleaseValidated, RefReleaseApproved}:  true,
		{RefReleaseApproved, RefReleaseReleased}:   true,
		{RefReleaseReleased, RefReleaseDeprecated}: true,
	}
	rule := func(from, to lifecycle.StateID) lifecycle.TransitionRule {
		return lifecycle.TransitionRule{
			From:               from,
			To:                 to,
			RequiresGovernance: governed[[2]lifecycle.StateID{from, to}],
			RetentionClass:     retentionReferenceDataLifecycle,
		}
	}
	return lifecycle.Profile{
		ID:        "hcmnext.model.reference_release/v1",
		Dimension: dimensionReferenceRelease,
		States: []lifecycle.StateID{
			RefReleaseDraft, RefReleaseValidated, RefReleaseApproved, RefReleaseReleased, RefReleaseDeprecated,
		},
		Initial:  RefReleaseDraft,
		Terminal: []lifecycle.StateID{RefReleaseDeprecated},
		Transitions: []lifecycle.TransitionRule{
			rule(RefReleaseDraft, RefReleaseValidated),
			rule(RefReleaseValidated, RefReleaseDraft),
			rule(RefReleaseValidated, RefReleaseApproved),
			rule(RefReleaseApproved, RefReleaseReleased),
			rule(RefReleaseReleased, RefReleaseDeprecated),
		},
	}
}

// ReferenceRelease is one governed dataset's release-lifecycle state
// (MODEL-018), the RELEASED/DEPRECATED analogue of [SchemaRelease].
type ReferenceRelease struct {
	ReleaseRef     string
	Dataset        ReferenceDatasetKind
	ArtifactDigest string
	State          lifecycle.StateID
}

// Validate rejects a reference release missing an identity, dataset, digest,
// or resting in an undeclared state.
func (r ReferenceRelease) Validate() error {
	if r.ReleaseRef == "" {
		return newError("ReferenceRelease.Validate", "release_ref", ErrInvalidReferenceRelease,
			"release carries no reference")
	}
	if !r.Dataset.Valid() {
		return newError("ReferenceRelease.Validate", "dataset", ErrInvalidReferenceRelease,
			"%s has dataset %q, outside the six declared kinds", r.ReleaseRef, r.Dataset)
	}
	if r.ArtifactDigest == "" {
		return newError("ReferenceRelease.Validate", "artifact_digest", ErrInvalidReferenceRelease,
			"%s carries no artifact digest", r.ReleaseRef)
	}
	profile := ReferenceReleaseLifecycleProfile()
	declared := map[lifecycle.StateID]bool{}
	for _, st := range profile.States {
		declared[st] = true
	}
	if !declared[r.State] {
		return newError("ReferenceRelease.Validate", "state", ErrInvalidReferenceRelease,
			"%s is in undeclared state %q", r.ReleaseRef, r.State)
	}
	return nil
}

// ReferenceReleaseTransition is the LifecycleTransitionRecord every reference
// release advance appends.
type ReferenceReleaseTransition struct {
	ReleaseRef            string
	From, To              lifecycle.StateID
	At                    values.Instant
	GovernanceDecisionRef string
	RetentionClass        string
}

// AdvanceReferenceRelease transitions rel to the target state.
//
// It rejects: a transition the compiled profile does not declare
// ([ErrUnknownReferenceReleaseTransition]); any RequiresGovernance transition
// (approval, publication, deprecation) with no governance decision reference;
// and a transition with no timestamp.
func AdvanceReferenceRelease(rel ReferenceRelease, to lifecycle.StateID, at values.Instant, governanceRef string) (ReferenceRelease, ReferenceReleaseTransition, error) {
	if err := rel.Validate(); err != nil {
		return ReferenceRelease{}, ReferenceReleaseTransition{}, err
	}
	profile := ReferenceReleaseLifecycleProfile()
	rule, ok := profile.Rule(rel.State, to)
	if !ok {
		return ReferenceRelease{}, ReferenceReleaseTransition{}, newError("AdvanceReferenceRelease", "state", ErrUnknownReferenceReleaseTransition,
			"%s declares no transition %s->%s", rel.ReleaseRef, rel.State, to)
	}
	if rule.RequiresGovernance && governanceRef == "" {
		return ReferenceRelease{}, ReferenceReleaseTransition{}, newError("AdvanceReferenceRelease", "governance_decision_ref", ErrInvalidReferenceRelease,
			"%s cannot move %s->%s without a governance decision", rel.ReleaseRef, rel.State, to)
	}
	if !at.IsSet() {
		return ReferenceRelease{}, ReferenceReleaseTransition{}, newError("AdvanceReferenceRelease", "at", ErrInvalidReferenceRelease,
			"%s transition carries no timestamp", rel.ReleaseRef)
	}
	next := rel
	next.State = to
	record := ReferenceReleaseTransition{
		ReleaseRef:            rel.ReleaseRef,
		From:                  rel.State,
		To:                    to,
		At:                    at,
		GovernanceDecisionRef: governanceRef,
		RetentionClass:        rule.RetentionClass,
	}
	return next, record, nil
}

// ReferenceValue is one governed code within a dataset, effective-dated and
// carrying its own [DefinitionStatus] independent of the release's own
// lifecycle state (MODEL-018), exactly as [EntityDefinition.Status] is
// independent of [ReleaseState].
type ReferenceValue struct {
	ReleaseRef    string
	Dataset       ReferenceDatasetKind
	Code          string
	Effective     values.EffectiveInterval
	Status        DefinitionStatus
	ProvenanceRef string
}

// Validate rejects a reference value missing a release, dataset, code,
// effective interval, status or provenance.
func (v ReferenceValue) Validate() error {
	if v.ReleaseRef == "" {
		return newError("ReferenceValue.Validate", "release_ref", ErrInvalidReferenceValue,
			"value carries no release reference")
	}
	if !v.Dataset.Valid() {
		return newError("ReferenceValue.Validate", "dataset", ErrInvalidReferenceValue,
			"%s has dataset %q, outside the six declared kinds", v.ReleaseRef, v.Dataset)
	}
	if v.Code == "" {
		return newError("ReferenceValue.Validate", "code", ErrInvalidReferenceValue,
			"%s declares no code", v.ReleaseRef)
	}
	if err := v.Effective.Validate(); err != nil {
		return newError("ReferenceValue.Validate", "effective", ErrInvalidReferenceValue,
			"%s/%s has no valid effective interval: %v", v.Dataset, v.Code, err)
	}
	if !v.Status.Valid() {
		return newError("ReferenceValue.Validate", "status", ErrInvalidReferenceValue,
			"%s/%s has status %q, outside the four declared statuses", v.Dataset, v.Code, v.Status)
	}
	if v.ProvenanceRef == "" {
		return newError("ReferenceValue.Validate", "provenance_ref", ErrInvalidReferenceValue,
			"%s/%s names no provenance edge", v.Dataset, v.Code)
	}
	return nil
}

// ReferenceResolution is the answer to "what does this code mean as of this
// date?": the resolved value plus the release and provenance identities that
// back it (MODEL-018 GREEN — "pilot references resolve by effective date and
// return release/provenance IDs").
type ReferenceResolution struct {
	Value         ReferenceValue
	ReleaseRef    string
	ProvenanceRef string
}

// ResolveReference resolves one code within a dataset at asOf against a
// compiled set of releases and values.
//
// It rejects: a code the dataset never published at all
// ([ErrUnknownReferenceValue]); a code resolving only to a DEPRECATED or
// RETIRED value ([ErrReferenceValueRetired]); a code whose every candidate
// value's effective window excludes asOf ([ErrReferenceValueOutOfEffectiveRange]);
// and a code whose governing release has never reached RELEASED — an
// unversioned global dataset ([ErrReferenceDatasetNotReleased]).
func ResolveReference(releases map[string]ReferenceRelease, vals []ReferenceValue, dataset ReferenceDatasetKind, code string, asOf values.Instant) (ReferenceResolution, error) {
	var candidates []ReferenceValue
	for _, v := range vals {
		if v.Dataset != dataset || v.Code != code {
			continue
		}
		candidates = append(candidates, v)
	}
	if len(candidates) == 0 {
		return ReferenceResolution{}, newError("ResolveReference", "code", ErrUnknownReferenceValue,
			"%s has no published code %q", dataset, code)
	}

	// Every candidate's governing release must have reached RELEASED: an
	// unversioned global dataset is a typed refusal, never a guess.
	for _, v := range candidates {
		rel, ok := releases[v.ReleaseRef]
		if !ok || rel.State != RefReleaseReleased {
			return ReferenceResolution{}, newError("ResolveReference", "release_ref", ErrReferenceDatasetNotReleased,
				"%s/%s references release %q which has never reached RELEASED", dataset, code, v.ReleaseRef)
		}
	}

	var inWindow []ReferenceValue
	for _, v := range candidates {
		if ok, err := v.Effective.ContainsInstant(asOf); err == nil && ok {
			inWindow = append(inWindow, v)
		}
	}
	if len(inWindow) == 0 {
		return ReferenceResolution{}, newError("ResolveReference", "effective", ErrReferenceValueOutOfEffectiveRange,
			"%s/%s has no value effective at %s", dataset, code, asOf)
	}

	for _, v := range inWindow {
		if v.Status == StatusDeprecated || v.Status == StatusRetired {
			return ReferenceResolution{}, newError("ResolveReference", "status", ErrReferenceValueRetired,
				"%s/%s is %s at %s", dataset, code, v.Status, asOf)
		}
	}

	winner := inWindow[0]
	return ReferenceResolution{
		Value:         winner,
		ReleaseRef:    winner.ReleaseRef,
		ProvenanceRef: winner.ProvenanceRef,
	}, nil
}
