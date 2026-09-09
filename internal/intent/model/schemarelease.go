package model

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// dimensionSchemaRelease is the [lifecycle.Dimension] this package registers
// its own profile under. [lifecycle.Profile] is dimension-generic, so the
// shared reachability/terminal/retention-class validation in
// internal/intent/lifecycle applies here without copying it.
const dimensionSchemaRelease lifecycle.Dimension = "SchemaReleaseState"

// Release states.
const (
	ReleaseDraft      lifecycle.StateID = "DRAFT"
	ReleaseValidated  lifecycle.StateID = "VALIDATED"
	ReleaseApproved   lifecycle.StateID = "APPROVED"
	ReleaseActive     lifecycle.StateID = "ACTIVE"
	ReleaseSuperseded lifecycle.StateID = "SUPERSEDED"
	ReleaseRetired    lifecycle.StateID = "RETIRED"
)

const retentionSchemaLifecycle = "SCHEMA_LIFECYCLE"

// ReleaseLifecycleProfile is the compiled-in schema-release lifecycle:
// draft -> validated -> approved -> active -> superseded/retired.
func ReleaseLifecycleProfile() lifecycle.Profile {
	governed := map[[2]lifecycle.StateID]bool{
		{ReleaseValidated, ReleaseApproved}: true,
		{ReleaseApproved, ReleaseActive}:    true,
		{ReleaseActive, ReleaseRetired}:     true,
		{ReleaseSuperseded, ReleaseRetired}: true,
	}
	rule := func(from, to lifecycle.StateID) lifecycle.TransitionRule {
		return lifecycle.TransitionRule{
			From:               from,
			To:                 to,
			RequiresGovernance: governed[[2]lifecycle.StateID{from, to}],
			RetentionClass:     retentionSchemaLifecycle,
		}
	}
	return lifecycle.Profile{
		ID:        "hcmnext.model.schema_release/v1",
		Dimension: dimensionSchemaRelease,
		States: []lifecycle.StateID{
			ReleaseDraft, ReleaseValidated, ReleaseApproved, ReleaseActive,
			ReleaseSuperseded, ReleaseRetired,
		},
		Initial:  ReleaseDraft,
		Terminal: []lifecycle.StateID{ReleaseRetired},
		Transitions: []lifecycle.TransitionRule{
			rule(ReleaseDraft, ReleaseValidated),
			rule(ReleaseValidated, ReleaseDraft),
			rule(ReleaseValidated, ReleaseApproved),
			rule(ReleaseApproved, ReleaseActive),
			rule(ReleaseActive, ReleaseSuperseded),
			rule(ReleaseActive, ReleaseRetired),
			rule(ReleaseSuperseded, ReleaseRetired),
		},
	}
}

// CompatibilityClass matches schema_release_compatibility_class_allowed in
// migrations/00001_platform_control.sql.
type CompatibilityClass string

// Compatibility classes.
const (
	CompatBackward CompatibilityClass = "BACKWARD_COMPATIBLE"
	CompatForward  CompatibilityClass = "FORWARD_COMPATIBLE"
	CompatFull     CompatibilityClass = "FULL"
	CompatBreaking CompatibilityClass = "BREAKING"
)

// Valid reports whether c is one of the four declared compatibility classes.
func (c CompatibilityClass) Valid() bool {
	switch c {
	case CompatBackward, CompatForward, CompatFull, CompatBreaking:
		return true
	default:
		return false
	}
}

// SchemaRelease is one governed schema's release-lifecycle state (MODEL-017).
type SchemaRelease struct {
	ReleaseRef          string
	ArtifactDigest      string
	CompatibilityClass  CompatibilityClass
	HasMigration        bool
	State               lifecycle.StateID
	UnresolvedConsumers []string
}

// Validate rejects a schema release missing an identity, digest or a
// recognized compatibility class, or resting in an undeclared state.
func (s SchemaRelease) Validate() error {
	if s.ReleaseRef == "" {
		return newError("SchemaRelease.Validate", "release_ref", ErrInvalidSchemaRelease,
			"release carries no reference")
	}
	if s.ArtifactDigest == "" {
		return newError("SchemaRelease.Validate", "artifact_digest", ErrInvalidSchemaRelease,
			"%s carries no artifact digest", s.ReleaseRef)
	}
	if !s.CompatibilityClass.Valid() {
		return newError("SchemaRelease.Validate", "compatibility_class", ErrInvalidSchemaRelease,
			"%s has compatibility class %q, outside the four declared classes",
			s.ReleaseRef, s.CompatibilityClass)
	}
	profile := ReleaseLifecycleProfile()
	declared := map[lifecycle.StateID]bool{}
	for _, st := range profile.States {
		declared[st] = true
	}
	if !declared[s.State] {
		return newError("SchemaRelease.Validate", "state", ErrInvalidSchemaRelease,
			"%s is in undeclared state %q", s.ReleaseRef, s.State)
	}
	return nil
}

// ReleaseTransition is the LifecycleTransitionRecord every release advance
// appends.
type ReleaseTransition struct {
	ReleaseRef            string
	From, To              lifecycle.StateID
	At                    values.Instant
	ActorRef              string
	GovernanceDecisionRef string
	RetentionClass        string
}

// AdvanceRelease transitions rel to the target state.
//
// It rejects: a transition the compiled profile does not declare
// ([ErrUnknownReleaseTransition]); publication (any transition the profile
// marks RequiresGovernance, including *->APPROVED and ACTIVE/SUPERSEDED->
// RETIRED) with no governance decision reference; an incompatible
// (BREAKING) change activated with no accompanying migration; and retirement
// while a hard consumer has not adopted the release.
func AdvanceRelease(rel SchemaRelease, to lifecycle.StateID, at values.Instant, governanceRef string) (SchemaRelease, ReleaseTransition, error) {
	if err := rel.Validate(); err != nil {
		return SchemaRelease{}, ReleaseTransition{}, err
	}
	profile := ReleaseLifecycleProfile()
	rule, ok := profile.Rule(rel.State, to)
	if !ok {
		return SchemaRelease{}, ReleaseTransition{}, newError("AdvanceRelease", "state", ErrUnknownReleaseTransition,
			"%s declares no transition %s->%s", rel.ReleaseRef, rel.State, to)
	}
	if rule.RequiresGovernance && governanceRef == "" {
		return SchemaRelease{}, ReleaseTransition{}, newError("AdvanceRelease", "governance_decision_ref", ErrInvalidSchemaRelease,
			"%s cannot move %s->%s without a governance decision", rel.ReleaseRef, rel.State, to)
	}
	if to == ReleaseActive && rel.CompatibilityClass == CompatBreaking && !rel.HasMigration {
		return SchemaRelease{}, ReleaseTransition{}, newError("AdvanceRelease", "has_migration", ErrInvalidSchemaRelease,
			"%s is a BREAKING change with no accompanying migration", rel.ReleaseRef)
	}
	if to == ReleaseRetired && len(rel.UnresolvedConsumers) > 0 {
		return SchemaRelease{}, ReleaseTransition{}, newError("AdvanceRelease", "unresolved_consumers", ErrInvalidSchemaRelease,
			"%s cannot retire with unresolved consumers %v", rel.ReleaseRef, rel.UnresolvedConsumers)
	}
	if !at.IsSet() {
		return SchemaRelease{}, ReleaseTransition{}, newError("AdvanceRelease", "at", ErrInvalidSchemaRelease,
			"%s transition carries no timestamp", rel.ReleaseRef)
	}
	next := rel
	next.State = to
	record := ReleaseTransition{
		ReleaseRef:            rel.ReleaseRef,
		From:                  rel.State,
		To:                    to,
		At:                    at,
		ActorRef:              governanceRef,
		GovernanceDecisionRef: governanceRef,
		RetentionClass:        rule.RetentionClass,
	}
	return next, record, nil
}
