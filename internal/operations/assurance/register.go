// Package assurance governs independent security, privacy, and control-assurance evidence.
//
// Sensitive source reports remain outside this package. The register stores only the
// metadata and findings needed to make a bounded, reproducible gate decision.
package assurance

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidAssessment = errors.New("assurance: invalid assessment")
	ErrGateBlocked       = errors.New("assurance: gate blocked")
)

type Kind string

const (
	KindPenetration Kind = "penetration"
	KindPrivacy     Kind = "privacy"
	KindControl     Kind = "control"
)

type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type FindingStatus string

const (
	FindingOpen         FindingStatus = "open"
	FindingRemediated   FindingStatus = "remediated"
	FindingRetested     FindingStatus = "retested"
	FindingAcceptedRisk FindingStatus = "accepted_risk"
)

// Finding is a scoped observation. Critical and high findings require an
// independent retest before the release gate can pass.
type Finding struct {
	ID       string
	Severity Severity
	Title    string
	Owner    string
	Due      time.Time
	Status   FindingStatus
	Retest   *Retest
}

type Retest struct {
	Assessor    string
	Independent bool
	Date        time.Time
	Result      string // pass or fail
	EvidenceRef string
}

// Assessment is the non-sensitive evidence envelope. ReportRef points to a
// separately access-controlled artifact; report bytes are deliberately absent.
type Assessment struct {
	ID                  string
	Kind                Kind
	Assessor            string
	AssessorIndependent bool
	Scope               []string
	ExcludedSurfaces    []string
	ReleaseVersion      string
	TopologyVersion     string
	ThreatVersion       string
	ControlVersion      string
	Environment         string
	Method              string
	Date                time.Time
	Expires             time.Time
	ReportRef           string
	Findings            []Finding
}

func (a Assessment) Validate(now time.Time) error {
	missing := func(name string, value bool) error {
		if !value {
			return fmt.Errorf("%w: %s is required", ErrInvalidAssessment, name)
		}
		return nil
	}
	for _, x := range []struct{ name, value string }{
		{"id", a.ID}, {"assessor", a.Assessor}, {"release_version", a.ReleaseVersion},
		{"topology_version", a.TopologyVersion}, {"threat_version", a.ThreatVersion},
		{"control_version", a.ControlVersion}, {"environment", a.Environment}, {"method", a.Method},
	} {
		if err := missing(x.name, strings.TrimSpace(x.value) != ""); err != nil {
			return err
		}
	}
	if err := missing("independent_assessor", a.AssessorIndependent); err != nil {
		return err
	}
	if len(a.Scope) == 0 {
		return fmt.Errorf("%w: scope is required", ErrInvalidAssessment)
	}
	if a.Date.IsZero() || a.Expires.IsZero() || !a.Expires.After(a.Date) {
		return fmt.Errorf("%w: valid date and expiry are required", ErrInvalidAssessment)
	}
	if !now.Before(a.Expires) {
		return fmt.Errorf("%w: evidence is expired", ErrInvalidAssessment)
	}
	seen := map[string]bool{}
	for _, f := range a.Findings {
		if f.ID == "" || f.Owner == "" || f.Due.IsZero() || f.Severity == "" {
			return fmt.Errorf("%w: finding %q lacks severity, owner, or deadline", ErrInvalidAssessment, f.ID)
		}
		if seen[f.ID] {
			return fmt.Errorf("%w: duplicate finding %q", ErrInvalidAssessment, f.ID)
		}
		seen[f.ID] = true
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			if f.Status != FindingRetested || f.Retest == nil || !f.Retest.Independent || !strings.EqualFold(f.Retest.Result, "pass") || f.Retest.Assessor == "" || f.Retest.Date.IsZero() {
				return fmt.Errorf("%w: finding %q lacks passing independent retest", ErrInvalidAssessment, f.ID)
			}
		}
	}
	return nil
}

type Register struct{ assessments map[string]Assessment }

func NewRegister() *Register { return &Register{assessments: make(map[string]Assessment)} }

func (r *Register) Add(a Assessment, now time.Time) error {
	if r == nil {
		return ErrInvalidAssessment
	}
	if err := a.Validate(now); err != nil {
		return err
	}
	if _, ok := r.assessments[a.ID]; ok {
		return fmt.Errorf("%w: duplicate assessment %q", ErrInvalidAssessment, a.ID)
	}
	a.Scope, a.ExcludedSurfaces, a.Findings = append([]string(nil), a.Scope...), append([]string(nil), a.ExcludedSurfaces...), append([]Finding(nil), a.Findings...)
	r.assessments[a.ID] = a
	return nil
}

type Decision struct {
	Allowed       bool
	Reasons       []string
	AssessmentIDs []string
}

func (r *Register) Gate(now time.Time) Decision {
	d := Decision{Allowed: true}
	if r == nil || len(r.assessments) == 0 {
		return Decision{Reasons: []string{"no assurance evidence"}}
	}
	ids := make([]string, 0, len(r.assessments))
	for id := range r.assessments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := r.assessments[id]
		d.AssessmentIDs = append(d.AssessmentIDs, id)
		if err := a.Validate(now); err != nil {
			d.Allowed = false
			d.Reasons = append(d.Reasons, id+": "+err.Error())
		}
	}
	return d
}

// Claim is deliberately an exact-scope statement, never a certification claim.
type Claim struct {
	AssessmentID     string
	Kind             Kind
	Scope            []string
	ExcludedSurfaces []string
	Statement        string
}

func (r *Register) Claim(id string, now time.Time) (Claim, error) {
	if r == nil {
		return Claim{}, fmt.Errorf("%w: nil register", ErrInvalidAssessment)
	}
	a, ok := r.assessments[id]
	if !ok {
		return Claim{}, fmt.Errorf("%w: unknown assessment %q", ErrInvalidAssessment, id)
	}
	if err := a.Validate(now); err != nil {
		return Claim{}, err
	}
	scope, excluded := append([]string(nil), a.Scope...), append([]string(nil), a.ExcludedSurfaces...)
	return Claim{AssessmentID: a.ID, Kind: a.Kind, Scope: scope, ExcludedSurfaces: excluded, Statement: fmt.Sprintf("Independent %s assessment completed for scope %s; excluded surfaces: %s. This statement is not a certification.", a.Kind, strings.Join(scope, ", "), strings.Join(excluded, ", "))}, nil
}
