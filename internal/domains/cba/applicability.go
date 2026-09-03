package cba

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

type Applicability string

const (
	Applicable    Applicability = "APPLICABLE"
	NotApplicable Applicability = "NOT_APPLICABLE"
	Conflict      Applicability = "CONFLICT"
	Unknown       Applicability = "UNKNOWN"
)

// ApplicabilityRequest is a complete, pinned input to one CBA evaluation.
type ApplicabilityRequest struct {
	WorkerID, AgreementID string
	EffectiveAt, KnownAt  time.Time
	Agreements            []AgreementRevision
	Units                 []BargainingUnitRevision
	Memberships           []MembershipRevision
}

// ApplicabilityResult carries the decision and all evidence needed to
// explain it. IDs are populated only from validated revisions.
type ApplicabilityResult struct {
	Outcome                          Applicability
	AgreementID, AgreementRevision   string
	UnitID, UnitRevision             string
	MembershipID, MembershipRevision string
	Representative, Source           string
	PrecedenceBasis                  string
	EffectiveFrom, EffectiveTo       time.Time
	KnownFrom, KnownTo               time.Time
}

func (r ApplicabilityResult) Validate() error {
	if r.Outcome != Applicable && r.Outcome != NotApplicable && r.Outcome != Conflict && r.Outcome != Unknown {
		return fmt.Errorf("cba: invalid applicability %q", r.Outcome)
	}
	return nil
}

// ResolveApplicability evaluates the exact agreement, unit and worker
// membership revisions at both effective and knowledge time. Ambiguity never
// silently resolves by slice order.
func ResolveApplicability(q ApplicabilityRequest) (ApplicabilityResult, error) {
	if q.WorkerID == "" || q.AgreementID == "" || q.EffectiveAt.IsZero() || q.KnownAt.IsZero() {
		return ApplicabilityResult{Outcome: Unknown}, errors.New("cba: worker, agreement, effective_at and known_at are required")
	}
	a := make([]AgreementRevision, 0)
	for _, x := range q.Agreements {
		if err := x.Validate(); err != nil {
			return ApplicabilityResult{Outcome: Unknown}, err
		}
		if x.AgreementID == q.AgreementID && x.active(q.EffectiveAt, q.KnownAt) {
			a = append(a, x)
		}
	}
	if len(a) == 0 {
		return ApplicabilityResult{Outcome: NotApplicable, PrecedenceBasis: "NO_EFFECTIVE_AGREEMENT"}, nil
	}
	sort.SliceStable(a, func(i, j int) bool { return a[i].Precedence > a[j].Precedence })
	if len(a) > 1 && a[0].Precedence == a[1].Precedence {
		return ApplicabilityResult{Outcome: Conflict, PrecedenceBasis: "OVERLAPPING_AGREEMENT_REVISIONS"}, nil
	}
	chosen := a[0]
	u := make([]BargainingUnitRevision, 0)
	for _, x := range q.Units {
		if err := x.Validate(); err != nil {
			return ApplicabilityResult{Outcome: Unknown}, err
		}
		if x.AgreementID == chosen.AgreementID && x.active(q.EffectiveAt, q.KnownAt) {
			u = append(u, x)
		}
	}
	if len(u) == 0 {
		return ApplicabilityResult{Outcome: NotApplicable, AgreementID: chosen.AgreementID, AgreementRevision: chosen.Revision, PrecedenceBasis: "NO_EFFECTIVE_UNIT"}, nil
	}
	if len(u) != 1 {
		return ApplicabilityResult{Outcome: Conflict, AgreementID: chosen.AgreementID, AgreementRevision: chosen.Revision, PrecedenceBasis: "OVERLAPPING_UNIT_REVISIONS"}, nil
	}
	chosenUnit := u[0]
	m := make([]MembershipRevision, 0)
	for _, x := range q.Memberships {
		if err := x.Validate(); err != nil {
			return ApplicabilityResult{Outcome: Unknown}, err
		}
		if x.WorkerID == q.WorkerID && x.UnitID == chosenUnit.UnitID && x.active(q.EffectiveAt, q.KnownAt) {
			m = append(m, x)
		}
	}
	if len(m) == 0 {
		return ApplicabilityResult{Outcome: NotApplicable, AgreementID: chosen.AgreementID, AgreementRevision: chosen.Revision, UnitID: chosenUnit.UnitID, UnitRevision: chosenUnit.Revision, Representative: chosen.Representative, Source: chosen.Source, PrecedenceBasis: "NO_EFFECTIVE_MEMBERSHIP"}, nil
	}
	if len(m) != 1 {
		return ApplicabilityResult{Outcome: Conflict, AgreementID: chosen.AgreementID, AgreementRevision: chosen.Revision, UnitID: chosenUnit.UnitID, UnitRevision: chosenUnit.Revision, PrecedenceBasis: "OVERLAPPING_MEMBERSHIP_REVISIONS"}, nil
	}
	return ApplicabilityResult{Outcome: Applicable, AgreementID: chosen.AgreementID, AgreementRevision: chosen.Revision, UnitID: chosenUnit.UnitID, UnitRevision: chosenUnit.Revision, MembershipID: m[0].MembershipID, MembershipRevision: m[0].Revision, Representative: chosenUnit.Representative, Source: m[0].Source, PrecedenceBasis: "AGREEMENT_PRECEDENCE_THEN_UNIT_MEMBERSHIP", EffectiveFrom: maxTime(chosen.EffectiveFrom, chosenUnit.EffectiveFrom, m[0].EffectiveFrom), KnownFrom: maxTime(chosen.KnownFrom, chosenUnit.KnownFrom, m[0].KnownFrom)}, nil
}

var Evaluate = ResolveApplicability

func maxTime(v ...time.Time) time.Time {
	out := v[0]
	for _, x := range v[1:] {
		if x.After(out) {
			out = x
		}
	}
	return out
}
