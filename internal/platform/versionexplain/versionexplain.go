// Package versionexplain resolves what each subject is running from one
// consistent rollout snapshot, the way ROLLOUT-007 requires: a target, a
// cohort, a stage, a bundle, an epoch, a receipt and the override and kill
// state, at the snapshot's known and effective time. A snapshot is only as
// good as its consistency - an override that points at a cohort the snapshot
// does not contain, a cohort epoch that does not match the snapshot head, a
// missing default cohort, or a live pause that also carries a scheduled
// advance is rejected as ROLLOUT_007_REJECTED with the offending field,
// state and version, and a rejected resolution records nothing. Nothing in a
// resolution exposes the membership of any cohort other than the resolved
// subject's own: the output carries one cohort id and nothing else.
package versionexplain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ErrRejected is the base token for every rejected resolution. A rejection
// carries the offending field, the state found and the version involved, so
// an operator can fix the snapshot instead of guessing.
var ErrRejected = errors.New("ROLLOUT_007_REJECTED")

// Rejection is a denied resolution with the offending field named.
type Rejection struct {
	Field   string
	State   string
	Version string
}

// Error renders the rejection with the stable ROLLOUT_007_REJECTED token
// first, so log prefixes and error matches stay exact.
func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", ErrRejected, r.Field, r.State, r.Version)
}

// Unwrap exposes the ROLLOUT_007 token, so errors.Is(err, ErrRejected)
// matches every rejection.
func (r *Rejection) Unwrap() error { return ErrRejected }

// IsRejected reports whether err is a ROLLOUT_007 rejection.
func IsRejected(err error) bool { return errors.Is(err, ErrRejected) }

// rejected builds a rejection error.
func rejected(field, state, version string) *Rejection {
	return &Rejection{Field: field, State: state, Version: version}
}

// Stage names a position in the rollout.
type Stage string

// Rollout stages, in advancement order.
const (
	StageHold   Stage = "hold"
	StagePilot  Stage = "pilot"
	StageCanary Stage = "canary"
	StageStaged Stage = "staged"
	StageAll    Stage = "general"
	StageKilled Stage = "killed"
)

var stageOrder = map[Stage]int{
	StageHold:   0,
	StagePilot:  1,
	StageCanary: 2,
	StageStaged: 3,
	StageAll:    4,
	StageKilled: 5,
}

// CohortState is one cohort row of the snapshot: where it is, what it runs,
// and under which receipt the state was recorded.
type CohortState struct {
	Stage   Stage
	Bundle  string
	Epoch   int64
	Receipt string
}

// Override pins one subject to one cohort regardless of membership.
type Override struct {
	CohortID string
}

// Snapshot is one consistent view of a target rollout at one instant.
type Snapshot struct {
	// TargetID identifies the target the snapshot describes.
	TargetID string
	// Epoch is the snapshot head epoch. Every cohort row must sit on it.
	Epoch int64
	// Cohorts holds every cohort row of the snapshot.
	Cohorts map[string]CohortState
	// Default is the cohort id subjects without membership fall into.
	Default string
	// Membership maps each known subject to its cohort id.
	Membership map[string]string
	// Overrides pins subjects to cohorts.
	Overrides map[string]Override
	// Pause freezes stage advancement for the target.
	Pause bool
	// KillSwitch stops the target; every subject resolves to the kill
	// state while it is set.
	KillSwitch bool
	// Advance, when set, schedules the next stage for the target. A live
	// pause and a live advance cannot coexist in one snapshot.
	Advance *Advance
	// KnownAt is when the snapshot was known.
	KnownAt values.Instant
	// EffectiveAt is when the snapshot started applying.
	EffectiveAt values.Instant
}

// Advance is a scheduled stage change inside the snapshot.
type Advance struct {
	Stage Stage
	At    values.Instant
}

// Explanation is what one subject is running, resolved from one snapshot at
// one instant. It deliberately exposes only the resolved subject's own
// cohort: no member lists, no counts of any other cohort.
type Explanation struct {
	Subject     string
	TargetID    string
	CohortID    string
	Stage       Stage
	Bundle      string
	Epoch       int64
	Receipt     string
	Override    bool
	Pause       bool
	Kill        bool
	KnownAt     values.Instant
	EffectiveAt values.Instant
}

// Recorder is the sink a resolution writes its record into. The engine
// records only accepted resolutions; a rejected one never touches it.
type Recorder interface {
	Record(expl Explanation)
}

// Explainer resolves subjects against snapshots.
type Explainer struct {
	recorder Recorder
}

// NewExplainer builds an explainer. A nil recorder records nothing, which is
// the zero-persistence behaviour a rejected resolution must show anyway.
func NewExplainer(recorder Recorder) *Explainer {
	return &Explainer{recorder: recorder}
}

// Explain resolves subject against snap at instant at. It returns a
// *Rejection (wrap-checkable against [ErrRejected]) when the snapshot is
// internally inconsistent or not yet in effect at at, and never records a
// rejected resolution.
func (e *Explainer) Explain(snap Snapshot, subject string, at values.Instant) (Explanation, error) {
	if snap.TargetID == "" {
		return Explanation{}, rejected("target", "absent", "")
	}
	if snap.Default == "" {
		return Explanation{}, rejected("default_cohort", "absent", "")
	}
	if _, ok := snap.Cohorts[snap.Default]; !ok {
		return Explanation{}, rejected("default_cohort", "not_in_cohorts", snap.Default)
	}
	if !snap.EffectiveAt.IsSet() || !snap.KnownAt.IsSet() || snap.EffectiveAt.Before(snap.KnownAt) {
		return Explanation{}, rejected("effective_time", "before_known_time", snap.TargetID)
	}
	if at.Before(snap.EffectiveAt) {
		return Explanation{}, rejected("resolve_time", "before_effective_time", snap.TargetID)
	}
	for cohortID, row := range snap.Cohorts {
		if row.Bundle == "" || row.Receipt == "" {
			return Explanation{}, rejected("cohort."+cohortID+".bundle", "absent", row.Bundle)
		}
		if _, known := stageOrder[row.Stage]; !known {
			return Explanation{}, rejected("cohort."+cohortID+".stage", string(row.Stage), row.Bundle)
		}
		if row.Epoch != snap.Epoch {
			return Explanation{}, rejected("cohort."+cohortID+".epoch", fmt.Sprintf("%d", row.Epoch), row.Bundle)
		}
	}
	for subjectID, ov := range snap.Overrides {
		if _, ok := snap.Cohorts[ov.CohortID]; !ok {
			return Explanation{}, rejected("override."+subjectID, "cohort_not_in_snapshot", ov.CohortID)
		}
	}
	if snap.Pause && snap.Advance != nil {
		return Explanation{}, rejected("pause", "advance_scheduled", string(snap.Advance.Stage))
	}

	cohortID := snap.Default
	ov, overridden := snap.Overrides[subject]
	if overridden {
		cohortID = ov.CohortID
	} else if member, ok := snap.Membership[subject]; ok {
		cohortID = member
	}
	// A membership that points outside the snapshot is a snapshot defect,
	// not a subject property: reject it instead of guessing.
	row, ok := snap.Cohorts[cohortID]
	if !ok {
		return Explanation{}, rejected("membership."+subject, "cohort_not_in_snapshot", cohortID)
	}

	expl := Explanation{
		Subject:     subject,
		TargetID:    snap.TargetID,
		CohortID:    cohortID,
		Stage:       row.Stage,
		Bundle:      row.Bundle,
		Epoch:       row.Epoch,
		Receipt:     row.Receipt,
		Override:    overridden,
		Pause:       snap.Pause,
		Kill:        false,
		KnownAt:     snap.KnownAt,
		EffectiveAt: snap.EffectiveAt,
	}
	if snap.KillSwitch {
		// The kill state is one state for every subject of the target: the
		// same stage, the subject's own bundle and receipt, kill and pause
		// both set. No subject of a killed target resolves to any running
		// version, so one snapshot can never mix running and killed.
		expl.Stage = StageKilled
		expl.Bundle = row.Bundle
		expl.Receipt = row.Receipt
		expl.Kill = true
		expl.Pause = true
	}

	if e.recorder != nil && !expl.Kill {
		e.recorder.Record(expl)
	}
	return expl, nil
}

// CohortIDs orders the cohort ids of a snapshot for stable reports and
// operator output.
func (snap Snapshot) CohortIDs() []string {
	ids := make([]string, 0, len(snap.Cohorts))
	for id := range snap.Cohorts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// StageRank exposes the ordering rank of a stage. Unknown stages rank below
// hold.
func StageRank(s Stage) int {
	if rank, ok := stageOrder[s]; ok {
		return rank
	}
	return -1
}
