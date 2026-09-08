package mobility

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// MaterialChange is the small, descriptive input to a mobility replan.  The
// package does not infer a country's law; callers supply the changed facts.
type MaterialChangeKind string

const (
	ChangeExtension MaterialChangeKind = "EXTENSION"
	ChangeCountry   MaterialChangeKind = "COUNTRY_CHANGE"
	ChangeReturn    MaterialChangeKind = "RETURN"
)

func (k MaterialChangeKind) Valid() bool {
	return k == ChangeExtension || k == ChangeCountry || k == ChangeReturn
}

var (
	ErrInvalidChange       = errors.New("mobility: invalid material change")
	ErrStaleVendor         = errors.New("mobility: vendor observation is not sufficient")
	ErrInvalidCorrection   = errors.New("mobility: invalid retro correction")
	ErrReturnAlreadyClosed = errors.New("mobility: host effect is already closed")
)

type MaterialChange struct {
	Kind             MaterialChangeKind
	HostAssignmentID string
	HostEffective    values.EffectiveInterval
	HostJurisdiction string
	EffectiveAt      time.Time
	Reason           string
	EvidenceRef      string
}

type ScopedReevaluation struct {
	JurisdictionChanged bool
	DatesChanged        bool
	Payroll             bool
	Tax                 bool
	Privacy             bool
	Immigration         bool
}

type ReplanResult struct {
	Successor         MobilityPlan
	Change            MaterialChangeKind
	ChangeEffectiveAt time.Time
	ChangeReason      string
	ChangeEvidenceRef string
	Reevaluation      ScopedReevaluation
}

// ReplanOnMaterialChange always creates a successor for an extension or
// country change.  Existing observations are retained as history, but the
// affected controls are deliberately downgraded until fresh evidence arrives.
func ReplanOnMaterialChange(plan MobilityPlan, change MaterialChange) (ReplanResult, error) {
	if err := plan.Validate(); err != nil {
		return ReplanResult{}, err
	}
	if (change.Kind != ChangeExtension && change.Kind != ChangeCountry) || change.EffectiveAt.IsZero() || strings.TrimSpace(change.Reason) == "" || strings.TrimSpace(change.EvidenceRef) == "" {
		return ReplanResult{}, fmt.Errorf("%w: replan kind, effective time, reason and evidence are required", ErrInvalidChange)
	}
	next := plan
	next.HostAssignments = append([]AssignmentRevision(nil), plan.HostAssignments...)
	next.Obligations = append([]MobilityObligation(nil), plan.Obligations...)
	hosts := plan.hosts()
	targetID := strings.TrimSpace(change.HostAssignmentID)
	if targetID == "" {
		if len(hosts) != 1 {
			return ReplanResult{}, fmt.Errorf("%w: host assignment id is required for multiple hosts", ErrInvalidChange)
		}
		targetID = hosts[0].id()
	}
	var current AssignmentRevision
	found := false
	for _, host := range hosts {
		if host.id() == targetID {
			if found {
				return ReplanResult{}, fmt.Errorf("%w: host assignment id is ambiguous", ErrInvalidChange)
			}
			current, found = host, true
		}
	}
	if !found {
		return ReplanResult{}, fmt.Errorf("%w: host assignment was not found", ErrInvalidChange)
	}
	re := ScopedReevaluation{Payroll: true, Tax: true, Privacy: true, Immigration: true}
	if change.Kind == ChangeCountry {
		if strings.TrimSpace(change.HostJurisdiction) == "" {
			return ReplanResult{}, fmt.Errorf("%w: host jurisdiction is required", ErrInvalidChange)
		}
		h := current
		if h.Jurisdiction == change.HostJurisdiction {
			return ReplanResult{}, fmt.Errorf("%w: host jurisdiction did not change", ErrInvalidChange)
		}
		h.Jurisdiction = change.HostJurisdiction
		var err error
		h, err = current.Successor(h)
		if err != nil {
			return ReplanResult{}, err
		}
		replaceHostAssignment(&next, targetID, h)
		re.JurisdictionChanged = true
	}
	if change.Kind == ChangeExtension {
		if err := change.HostEffective.Validate(); err != nil {
			return ReplanResult{}, fmt.Errorf("%w: host effective interval is required", ErrInvalidChange)
		}
		// An extension changes the effective interval, not the authority or
		// identity of the assignment. Treat the candidate as a dated fact only.
		h := current
		if !isStrictExtension(h.effective(), change.HostEffective) {
			return ReplanResult{}, fmt.Errorf("%w: extension must preserve the start and advance the end", ErrInvalidChange)
		}
		h.Effective = change.HostEffective
		h.EffectiveWindow = values.EffectiveInterval{}
		var err error
		h, err = current.Successor(h)
		if err != nil {
			return ReplanResult{}, err
		}
		replaceHostAssignment(&next, targetID, h)
		re.DatesChanged = true
	}
	for i := range next.Obligations {
		// A material change never carries forward a prior approval as current.
		next.Obligations[i].Status = ObligationUnknown
		// Evidence remains part of dated prior truth. Unknown prevents it from
		// being treated as a current approval after the material change.
	}
	result, err := plan.Successor(next)
	if err != nil {
		return ReplanResult{}, err
	}
	return ReplanResult{Successor: result, Change: change.Kind, ChangeEffectiveAt: change.EffectiveAt, ChangeReason: change.Reason, ChangeEvidenceRef: change.EvidenceRef, Reevaluation: re}, nil
}

func replaceHostAssignment(plan *MobilityPlan, id string, replacement AssignmentRevision) {
	for i := range plan.HostAssignments {
		if plan.HostAssignments[i].id() == id {
			plan.HostAssignments[i] = replacement
			return
		}
	}
	if plan.HostAssignment.id() == id {
		plan.HostAssignment = replacement
		return
	}
	plan.Host = replacement
}

func isStrictExtension(current, next values.EffectiveInterval) bool {
	if current.Validate() != nil || next.Validate() != nil || current.Kind() != next.Kind() || current.IsOpenEnded() || next.IsOpenEnded() {
		return false
	}
	if current.Kind() == values.IntervalKindLocalDate {
		currentStart, _ := current.StartDate()
		nextStart, _ := next.StartDate()
		currentEnd, _ := current.EndDate()
		nextEnd, _ := next.EndDate()
		return currentStart.Compare(nextStart) == 0 && currentEnd.Compare(nextEnd) < 0 && current.Calendar() == next.Calendar()
	}
	currentStart, _ := current.StartInstant()
	nextStart, _ := next.StartInstant()
	currentEnd, _ := current.EndInstant()
	nextEnd, _ := next.EndInstant()
	return currentStart.Compare(nextStart) == 0 && currentEnd.Compare(nextEnd) < 0
}

type VendorObservation struct {
	ObligationKind ObligationKind
	ObligationRef  string
	VendorRef      string
	ObservationRef string
	Status         ObligationStatus
	Accepted       bool
	ObservedAt     time.Time
}

type VendorReconciliationStatus string

const (
	VendorReview  VendorReconciliationStatus = "REVIEW_REQUIRED"
	VendorUnknown VendorReconciliationStatus = "UNKNOWN"
)

type VendorReconciliation struct {
	Status      VendorReconciliationStatus
	Obligation  MobilityObligation
	Observation VendorObservation
}

// ReconcileVendor maps an observation to exactly one obligation. Acceptance
// is evidence about the vendor only; it cannot certify payroll, tax, PE or
// privacy obligations by itself.
func ReconcileVendor(obligation MobilityObligation, observation VendorObservation) (VendorReconciliation, error) {
	if err := obligation.Validate(); err != nil {
		return VendorReconciliation{}, err
	}
	if !observation.ObligationKind.Valid() || observation.ObligationKind != obligation.Kind || observation.ObligationRef != obligation.Ref || !observation.Status.Valid() || strings.TrimSpace(observation.VendorRef) == "" || strings.TrimSpace(observation.ObservationRef) == "" || observation.ObservedAt.IsZero() {
		return VendorReconciliation{}, fmt.Errorf("%w: observation scope is incomplete", ErrStaleVendor)
	}
	if !observation.Accepted {
		return VendorReconciliation{Status: VendorReview, Obligation: obligation, Observation: observation}, nil
	}
	if observation.Status == ObligationUnknown {
		return VendorReconciliation{Status: VendorUnknown, Obligation: obligation, Observation: observation}, nil
	}
	return VendorReconciliation{Status: VendorReview, Obligation: obligation, Observation: observation}, nil
}

type HostEffectKind string

const (
	EffectPayroll HostEffectKind = "PAYROLL"
	EffectTax     HostEffectKind = "TAX"
	EffectPrivacy HostEffectKind = "PRIVACY"
	EffectPE      HostEffectKind = "PE"
)

type HostEffect struct {
	Kind   HostEffectKind
	Ref    string
	Active bool
}
type ReturnResult struct {
	Effects []HostEffect
	Closed  []string
}

func (e HostEffectKind) Valid() bool {
	return e == EffectPayroll || e == EffectTax || e == EffectPrivacy || e == EffectPE
}

// CloseHostEffects closes only the named effects and returns a detached value;
// unrelated effects remain active and the input is never rewritten.
func CloseHostEffects(effects []HostEffect, closeRefs []string) (ReturnResult, error) {
	if len(closeRefs) == 0 {
		return ReturnResult{}, fmt.Errorf("%w: at least one effect is required", ErrInvalidChange)
	}
	set := map[string]bool{}
	for _, ref := range closeRefs {
		if strings.TrimSpace(ref) == "" || set[ref] {
			return ReturnResult{}, fmt.Errorf("%w: effect reference is empty or duplicated", ErrInvalidChange)
		}
		set[ref] = true
	}
	out := make([]HostEffect, len(effects))
	copy(out, effects)
	closed := make([]string, 0, len(set))
	found := make(map[string]bool, len(set))
	known := make(map[string]bool, len(effects))
	for i := range out {
		if !out[i].Kind.Valid() || strings.TrimSpace(out[i].Ref) == "" || known[out[i].Ref] {
			return ReturnResult{}, fmt.Errorf("%w: effect is incomplete", ErrInvalidChange)
		}
		known[out[i].Ref] = true
		if set[out[i].Ref] {
			found[out[i].Ref] = true
			if !out[i].Active {
				return ReturnResult{}, fmt.Errorf("%w: %s", ErrReturnAlreadyClosed, out[i].Ref)
			}
			out[i].Active = false
			closed = append(closed, out[i].Ref)
		}
	}
	for ref := range set {
		if !found[ref] {
			return ReturnResult{}, fmt.Errorf("%w: effect %s was not found", ErrInvalidChange, ref)
		}
	}
	return ReturnResult{Effects: out, Closed: closed}, nil
}

type RetroCorrection struct {
	CorrectionID    string
	MobilityID      string
	EffectiveAt     time.Time
	PriorDigest     string
	CorrectedDigest string
	Reason          string
	EvidenceRef     string
}

func (c RetroCorrection) Validate() error {
	if strings.TrimSpace(c.CorrectionID) == "" || strings.TrimSpace(c.MobilityID) == "" || c.EffectiveAt.IsZero() || strings.TrimSpace(c.PriorDigest) == "" || strings.TrimSpace(c.CorrectedDigest) == "" || strings.TrimSpace(c.Reason) == "" || strings.TrimSpace(c.EvidenceRef) == "" {
		return ErrInvalidCorrection
	}
	if c.PriorDigest == c.CorrectedDigest {
		return fmt.Errorf("%w: correction has no delta", ErrInvalidCorrection)
	}
	return nil
}

func AppendRetroCorrection(history []RetroCorrection, correction RetroCorrection, currentDigest string) ([]RetroCorrection, error) {
	if err := correction.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(currentDigest) == "" || correction.PriorDigest != currentDigest {
		return nil, fmt.Errorf("%w: prior truth is not current", ErrInvalidCorrection)
	}
	for _, prior := range history {
		if prior.MobilityID != correction.MobilityID {
			return nil, fmt.Errorf("%w: correction scope does not match history", ErrInvalidCorrection)
		}
		if prior.CorrectionID == correction.CorrectionID {
			return nil, fmt.Errorf("%w: correction id already exists", ErrInvalidCorrection)
		}
	}
	if len(history) > 0 && history[len(history)-1].CorrectedDigest != currentDigest {
		return nil, fmt.Errorf("%w: history head is not current", ErrInvalidCorrection)
	}
	out := append([]RetroCorrection(nil), history...)
	out = append(out, correction)
	return out, nil
}
