// Package benefits owns the read-only benefit-plan catalogue contract.
//
// Plans and plan years are logical identities; all business meaning is carried
// by append-only revisions.  This package deliberately has no persistence or
// transport dependency.
package benefits

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidPlan          = errors.New("benefits: invalid plan")
	ErrInvalidPlanYear      = errors.New("benefits: invalid plan year")
	ErrInvalidRevision      = errors.New("benefits: invalid immutable revision")
	ErrRevisionMutation     = errors.New("benefits: revision is immutable")
	ErrOverlappingPlanYears = errors.New("benefits: overlapping plan years")
)

// Plan is the stable logical identity of a benefit plan.  Do not edit its
// descriptive fields after publication; publish a successor PlanRevision.
type Plan struct {
	PlanID       values.EntityRef
	Name         string
	ProgramID    values.EntityRef
	Carrier      values.EntityRef
	Provider     values.EntityRef
	Sponsor      values.EntityRef
	LegalEntity  values.EntityRef
	Jurisdiction string
	Currency     string
	PlanYears    []PlanYear
}

func (p Plan) Validate() error {
	if err := p.PlanID.Validate(); err != nil {
		return fmt.Errorf("%w: plan id: %v", ErrInvalidPlan, err)
	}
	if p.Name == "" || p.Jurisdiction == "" || p.Currency == "" {
		return ErrInvalidPlan
	}
	for _, r := range []struct {
		name string
		ref  values.EntityRef
	}{
		{"program", p.ProgramID}, {"carrier", p.Carrier}, {"provider", p.Provider}, {"sponsor", p.Sponsor}, {"legal entity", p.LegalEntity},
	} {
		if r.ref.Id != "" {
			if err := r.ref.Validate(); err != nil {
				return fmt.Errorf("%w: %s: %v", ErrInvalidPlan, r.name, err)
			}
		}
	}
	for i, y := range p.PlanYears {
		if err := y.Validate(); err != nil {
			return err
		}
		if y.PlanID != p.PlanID {
			return fmt.Errorf("%w: year %d belongs to another plan", ErrInvalidPlanYear, y.Year)
		}
		for _, prior := range p.PlanYears[:i] {
			if intervalsOverlap(prior.Effective, y.Effective) {
				return ErrOverlappingPlanYears
			}
		}
	}
	return nil
}

func (p Plan) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.benefits.Plan", 1).Value("id", p.PlanID).String("name", p.Name).String("jurisdiction", p.Jurisdiction).String("currency", p.Currency)
	for _, item := range []struct {
		tag string
		ref values.EntityRef
	}{{"program", p.ProgramID}, {"carrier", p.Carrier}, {"provider", p.Provider}, {"sponsor", p.Sponsor}, {"legal_entity", p.LegalEntity}} {
		tag, ref := item.tag, item.ref
		w.Optional(tag, ref.Id != "", ref)
	}
	w.Value("plan_years", planYearsCanonical(p.PlanYears))
	raw, _ := w.Bytes()
	return raw
}

type planYearsEncoding struct{ Years []PlanYear }

func (x planYearsEncoding) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.benefits.PlanYears", 1).Count("year", len(x.Years))
	for _, y := range x.Years {
		w.Value("year", y)
	}
	b, _ := w.Bytes()
	return b
}
func planYearsCanonical(y []PlanYear) planYearsEncoding { return planYearsEncoding{Years: y} }

// PlanYear identifies one benefit plan year and its effective window.
type PlanYear struct {
	PlanID    values.EntityRef
	Year      int32
	Effective values.EffectiveInterval
}

func (y PlanYear) Validate() error {
	if err := y.PlanID.Validate(); err != nil {
		return fmt.Errorf("%w: plan: %v", ErrInvalidPlanYear, err)
	}
	if y.Year < 1 || y.Year > 9999 {
		return fmt.Errorf("%w: year must be between 1 and 9999", ErrInvalidPlanYear)
	}
	if err := y.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidPlanYear, err)
	}
	return nil
}

func (y PlanYear) Canonical() []byte {
	if y.Validate() != nil {
		return nil
	}
	w, e := canonicalbytes.New("hcmnext.domains.benefits.PlanYear", 1).Value("plan", y.PlanID).Int("year", int64(y.Year)).Value("effective", y.Effective).Bytes()
	if e != nil {
		return nil
	}
	return w
}

func intervalsOverlap(a, b values.EffectiveInterval) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	if a.Kind() == values.IntervalKindLocalDate {
		as, _ := a.StartDate()
		bs, _ := b.StartDate()
		ae, aok := a.EndDate()
		be, bok := b.EndDate()
		return (!aok || bs.Compare(ae) < 0) && (!bok || as.Compare(be) < 0)
	}
	return false
}
