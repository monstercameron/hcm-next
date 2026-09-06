// Package incidentstate owns the canonical, append-only incident lifecycle.
package incidentstate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

func Version() int { return contractVersion }

type State string

const (
	Detected      State = "DETECTED"
	Triaged       State = "TRIAGED"
	Declared      State = "DECLARED"
	Mitigating    State = "MITIGATING"
	Monitoring    State = "MONITORING"
	Resolved      State = "RESOLVED"
	Reviewed      State = "REVIEWED"
	Reopened      State = "REOPENED"
	FalsePositive State = "FALSE_POSITIVE"
	Merged        State = "MERGED"
	Split         State = "SPLIT"
	Duplicate     State = "DUPLICATE"
)

type AffectedFact struct {
	TenantID string
	Kind     string
	Value    string
}

type AffectedSet struct {
	Known    bool
	Verified bool
	Partial  bool
	Facts    []AffectedFact
}

type Evidence struct {
	ID   string
	Kind string
}

type TimelineEvent struct {
	ID          string
	From        State
	To          State
	Actor       string
	Reason      string
	EvidenceRef string
	At          time.Time
	Version     uint64
}

type Incident struct {
	ID              string
	TenantID        string
	Service         string
	Capability      string
	State           State
	Version         uint64
	Owner           string
	Mitigation      string
	RepairLink      string
	MonitoringUntil time.Time
	Review          string
	Affected        AffectedSet
	Evidence        []Evidence
	Timeline        []TimelineEvent
}

type Command struct {
	To                State
	Actor             string
	Reason            string
	EvidenceRef       string
	EventID           string
	Owner             string
	Mitigation        string
	RepairLink        string
	MonitoringUntil   time.Time
	Review            string
	Affected          AffectedSet
	RelatedIncidentID string
}

var (
	ErrInvalidIncident   = errors.New("incident: invalid incident")
	ErrTransitionInvalid = errors.New("INCIDENT_TRANSITION_INVALID")
	ErrClosureIncomplete = errors.New("incident: resolution prerequisites are incomplete")
)

var transitions = map[State]map[State]bool{
	Detected:   {Triaged: true, FalsePositive: true},
	Triaged:    {Declared: true, FalsePositive: true},
	Declared:   {Mitigating: true, Merged: true, Split: true, Duplicate: true},
	Mitigating: {Monitoring: true, Merged: true, Split: true, Duplicate: true},
	Monitoring: {Resolved: true, Merged: true, Split: true, Duplicate: true},
	Resolved:   {Reviewed: true, Reopened: true},
	Reviewed:   {Reopened: true},
	Reopened:   {Mitigating: true},
}

func New(id, tenantID string, at time.Time) (Incident, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(tenantID) == "" || at.IsZero() {
		return Incident{}, ErrInvalidIncident
	}
	return Incident{ID: id, TenantID: tenantID, State: Detected, Version: 1, Timeline: []TimelineEvent{{ID: id + ":detected", To: Detected, At: at, Version: 1}}}, nil
}

func clone(in Incident) Incident {
	in.Affected.Facts = append([]AffectedFact(nil), in.Affected.Facts...)
	in.Evidence = append([]Evidence(nil), in.Evidence...)
	in.Timeline = append([]TimelineEvent(nil), in.Timeline...)
	return in
}

func validCommand(in Incident, c Command, now time.Time) error {
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.TenantID) == "" || in.Version == 0 || in.State == "" {
		return ErrInvalidIncident
	}
	if !transitions[in.State][c.To] {
		return fmt.Errorf("%w: %s -> %s", ErrTransitionInvalid, in.State, c.To)
	}
	if strings.TrimSpace(c.Actor) == "" || strings.TrimSpace(c.Reason) == "" || strings.TrimSpace(c.EvidenceRef) == "" || now.IsZero() {
		return fmt.Errorf("%w: actor, reason, evidence, and time are required", ErrTransitionInvalid)
	}
	if !c.MonitoringUntil.IsZero() && c.MonitoringUntil.Before(now) && c.To == Monitoring {
		return fmt.Errorf("%w: monitoring window must be in the future", ErrTransitionInvalid)
	}
	if (c.To == Merged || c.To == Split || c.To == Duplicate) && strings.TrimSpace(c.RelatedIncidentID) == "" {
		return fmt.Errorf("%w: related incident is required", ErrTransitionInvalid)
	}
	return nil
}

func applyFields(next *Incident, c Command) {
	if c.Owner != "" {
		next.Owner = c.Owner
	}
	if c.Mitigation != "" {
		next.Mitigation = c.Mitigation
	}
	if c.RepairLink != "" {
		next.RepairLink = c.RepairLink
	}
	if !c.MonitoringUntil.IsZero() {
		next.MonitoringUntil = c.MonitoringUntil
	}
	if c.Review != "" {
		next.Review = c.Review
	}
	if c.Affected.Known || c.Affected.Verified || c.Affected.Partial || c.Affected.Facts != nil {
		next.Affected = c.Affected
		next.Affected.Facts = append([]AffectedFact(nil), c.Affected.Facts...)
	}
}

func closureReady(in Incident, now time.Time) bool {
	return in.Affected.Known && in.Affected.Verified && strings.TrimSpace(in.Owner) != "" && strings.TrimSpace(in.Mitigation) != "" && strings.TrimSpace(in.RepairLink) != "" && !in.MonitoringUntil.IsZero() && !now.Before(in.MonitoringUntil)
}

// Transition returns a new incident and appends exactly one evidence-bearing
// timeline event. The input incident is never mutated, including on failure.
func Transition(in Incident, c Command, now time.Time) (Incident, error) {
	if err := validCommand(in, c, now); err != nil {
		return Incident{}, err
	}
	next := clone(in)
	applyFields(&next, c)
	if c.To == Resolved && !closureReady(next, now) {
		return Incident{}, fmt.Errorf("%w: %v", ErrTransitionInvalid, ErrClosureIncomplete)
	}
	if c.To == Reviewed && strings.TrimSpace(next.Review) == "" {
		return Incident{}, fmt.Errorf("%w: %v", ErrTransitionInvalid, ErrClosureIncomplete)
	}
	next.Version++
	eventID := c.EventID
	if strings.TrimSpace(eventID) == "" {
		eventID = fmt.Sprintf("%s:%d", in.ID, next.Version)
	}
	next.Timeline = append(next.Timeline, TimelineEvent{ID: eventID, From: in.State, To: c.To, Actor: c.Actor, Reason: c.Reason, EvidenceRef: c.EvidenceRef, At: now, Version: next.Version})
	next.State = c.To
	return next, nil
}

// Timeline returns a stable copy suitable for evidence or customer filtering.
func Timeline(in Incident) []TimelineEvent {
	out := append([]TimelineEvent(nil), in.Timeline...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func Explain(in Incident) string {
	return fmt.Sprintf("incident %s tenant=%s state=%s version=%d affected_known=%t affected_partial=%t", in.ID, in.TenantID, in.State, in.Version, in.Affected.Known, in.Affected.Partial)
}
