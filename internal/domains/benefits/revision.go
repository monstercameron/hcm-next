package benefits

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PlanRevision is an immutable, citable snapshot of plan rules.  A revision
// never changes in place: corrections and recalculations create a successor.
type PlanRevision struct {
	RevisionID           values.EntityRef
	PlanID               values.EntityRef
	Revision             values.RevisionToken
	Supersedes           values.EntityRef
	PlanYear             PlanYear
	Name                 string
	Carrier              values.EntityRef
	Provider             values.EntityRef
	Sponsor              values.EntityRef
	Jurisdiction         string
	Currency             string
	CoverageTiers        []string
	Options              []string
	RateScheduleRef      values.EntityRef
	EligibilityRulesRef  values.EntityRef
	EnrollmentRulesRef   values.EntityRef
	ContributionRulesRef values.EntityRef
	Effective            values.EffectiveInterval
	Authority            values.EntityRef
	CanonicalDigest      string
}

func (r PlanRevision) Validate() error {
	if err := r.RevisionID.Validate(); err != nil {
		return fmt.Errorf("%w: revision id: %v", ErrInvalidRevision, err)
	}
	if err := r.PlanID.Validate(); err != nil {
		return fmt.Errorf("%w: plan id: %v", ErrInvalidRevision, err)
	}
	if !r.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision token is required", ErrInvalidRevision)
	}
	if err := r.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: revision token: %v", ErrInvalidRevision, err)
	}
	if r.Supersedes.Id != "" {
		if err := r.Supersedes.Validate(); err != nil {
			return fmt.Errorf("%w: supersedes: %v", ErrInvalidRevision, err)
		}
		if r.Supersedes.Tenant != r.PlanID.Tenant {
			return fmt.Errorf("%w: supersedes crosses tenant", ErrInvalidRevision)
		}
	}
	if r.PlanYear.PlanID.Id != "" && r.PlanYear.PlanID != r.PlanID {
		return fmt.Errorf("%w: plan year belongs to another plan", ErrInvalidRevision)
	}
	if r.Name == "" || r.Jurisdiction == "" || r.Currency == "" {
		return ErrInvalidRevision
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidRevision, err)
	}
	for _, x := range []struct {
		n string
		v values.EntityRef
	}{{"carrier", r.Carrier}, {"provider", r.Provider}, {"sponsor", r.Sponsor}, {"rate schedule", r.RateScheduleRef}, {"eligibility rules", r.EligibilityRulesRef}, {"enrollment rules", r.EnrollmentRulesRef}, {"contribution rules", r.ContributionRulesRef}, {"authority", r.Authority}} {
		if x.v.Id != "" {
			if err := x.v.Validate(); err != nil {
				return fmt.Errorf("%w: %s: %v", ErrInvalidRevision, x.n, err)
			}
			if x.v.Tenant != r.PlanID.Tenant {
				return fmt.Errorf("%w: %s crosses tenant", ErrInvalidRevision, x.n)
			}
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidRevision)
	}
	return nil
}

func (r PlanRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.benefits.PlanRevision", 1).Value("revision_id", r.RevisionID).Value("plan_id", r.PlanID).Value("revision", r.Revision).String("name", r.Name).String("jurisdiction", r.Jurisdiction).String("currency", r.Currency).SortedStrings("coverage_tiers", r.CoverageTiers).SortedStrings("options", r.Options)
	for _, x := range []struct {
		tag string
		ref values.EntityRef
	}{{"supersedes", r.Supersedes}, {"carrier", r.Carrier}, {"provider", r.Provider}, {"sponsor", r.Sponsor}, {"rate_schedule", r.RateScheduleRef}, {"eligibility_rules", r.EligibilityRulesRef}, {"enrollment_rules", r.EnrollmentRulesRef}, {"contribution_rules", r.ContributionRulesRef}, {"authority", r.Authority}} {
		w.Optional(x.tag, x.ref.Id != "", x.ref)
	}
	if r.PlanYear.PlanID.Id != "" {
		w.Value("plan_year", r.PlanYear)
	} else {
		w.Bool("plan_year?", false)
	}
	w.Value("effective", r.Effective)
	raw, e := w.Bytes()
	if e != nil {
		return nil
	}
	return raw
}
func (r PlanRevision) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func (r PlanRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r PlanRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// Successor returns a new revision whose identity and token are supplied by
// the caller. The receiver is never modified.
func (r PlanRevision) Successor(id values.EntityRef, token values.RevisionToken) (PlanRevision, error) {
	if err := r.Validate(); err != nil {
		return PlanRevision{}, err
	}
	if err := id.Validate(); err != nil {
		return PlanRevision{}, err
	}
	if !token.IsSpecified() {
		return PlanRevision{}, fmt.Errorf("%w: successor token is required", ErrInvalidRevision)
	}
	n := r
	n.RevisionID = id
	n.Revision = token
	n.Supersedes = r.RevisionID
	n.CanonicalDigest = ""
	n.CanonicalDigest = n.computedDigest()
	return n, nil
}
