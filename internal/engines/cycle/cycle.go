// Package cycle owns the declarative business-cycle contract. It validates a
// complete cycle definition and publishes immutable, effective-dated
// revisions. The package is deliberately pure: it has no clock, storage, or
// policy lookup dependencies.
package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrType       = errors.New("cycle: type is required")
	ErrOwner      = errors.New("cycle: owner is required")
	ErrScope      = errors.New("cycle: scope is required")
	ErrTimePolicy = errors.New("cycle: timezone and calendar are required")
	ErrPeriods    = errors.New("cycle: at least one valid period is required")
	ErrPhases     = errors.New("cycle: at least one valid phase is required")
	ErrPolicies   = errors.New("cycle: cutoff, late, reopen, and restatement policies are required")
	ErrRevision   = errors.New("cycle: revision identity is required")
	ErrEffective  = errors.New("cycle: effective interval is invalid")
)

type CycleType string

const (
	CycleTypeUnspecified CycleType = ""
	CycleTypeRewards     CycleType = "REWARDS"
	CycleTypePayroll     CycleType = "PAYROLL"
	CycleTypeTalent      CycleType = "TALENT"
)

// Scope is the explicit ownership boundary for a cycle.
type Scope struct{ TenantID, OrganizationID, LegalEntityID, PopulationID string }

func (s Scope) valid() bool {
	return s.TenantID != "" && (s.OrganizationID != "" || s.LegalEntityID != "" || s.PopulationID != "")
}

type Period struct {
	ID         string
	Start, End time.Time
}
type Phase struct {
	ID, Name   string
	Start, End time.Time
}

// Policies are explicit even when a caller chooses a conservative value.
type Policies struct{ Cutoff, Late, Reopen, Restatement string }

func (p Policies) valid() bool {
	return p.Cutoff != "" && p.Late != "" && p.Reopen != "" && p.Restatement != ""
}

// BusinessCycle is the complete cycle definition. Slices are copied by
// NewRevision and are never exposed directly by a Revision.
type BusinessCycle struct {
	Type     CycleType
	Owner    string
	Scope    Scope
	Timezone string
	// TimeZone is accepted as the conventional spelling; when set it must
	// agree with Timezone.
	TimeZone    string
	TZDBVersion string
	Calendar    values.CalendarRef
	Periods     []Period
	Phases      []Phase
	Policies    Policies
}

// Definition is the conventional engine name for a BusinessCycle.
type Definition = BusinessCycle

func (c BusinessCycle) Validate() error {
	if c.Type == CycleTypeUnspecified {
		return ErrType
	}
	if strings.TrimSpace(c.Owner) == "" {
		return ErrOwner
	}
	if !c.Scope.valid() {
		return ErrScope
	}
	zone := c.Timezone
	if zone == "" {
		zone = c.TimeZone
	}
	if c.Timezone != "" && c.TimeZone != "" && c.Timezone != c.TimeZone {
		return ErrTimePolicy
	}
	if zone == "" || c.TZDBVersion == "" || c.Calendar.Validate() != nil {
		return ErrTimePolicy
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("%w: timezone %q: %v", ErrTimePolicy, zone, err)
	}
	if len(c.Periods) == 0 {
		return ErrPeriods
	}
	for _, p := range c.Periods {
		if p.ID == "" || p.Start.IsZero() || p.End.IsZero() || !p.Start.Before(p.End) {
			return ErrPeriods
		}
	}
	if len(c.Phases) == 0 {
		return ErrPhases
	}
	for _, p := range c.Phases {
		if p.ID == "" || p.Name == "" || p.Start.IsZero() || p.End.IsZero() || !p.Start.Before(p.End) {
			return ErrPhases
		}
	}
	if !c.Policies.valid() {
		return ErrPolicies
	}
	return nil
}

// Canonical returns deterministic JSON bytes for a valid definition.
func (c BusinessCycle) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	b, _ := json.Marshal(c)
	return b
}
func (c BusinessCycle) Digest() string {
	b := c.Canonical()
	if b == nil {
		return ""
	}
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// Revision is an immutable effective-dated cycle snapshot.
type Revision struct {
	Definition                 BusinessCycle
	ID                         string
	Version                    string
	EffectiveFrom, EffectiveTo time.Time
	Digest                     string
}

func NewRevision(c BusinessCycle, id, version string, from, to time.Time) (Revision, error) {
	if err := c.Validate(); err != nil {
		return Revision{}, err
	}
	if id == "" || version == "" {
		return Revision{}, ErrRevision
	}
	if from.IsZero() || (!to.IsZero() && !from.Before(to)) {
		return Revision{}, ErrEffective
	}
	c.Periods = append([]Period(nil), c.Periods...)
	c.Phases = append([]Phase(nil), c.Phases...)
	r := Revision{Definition: c, ID: id, Version: version, EffectiveFrom: from.UTC(), EffectiveTo: to.UTC()}
	r.Digest = r.computeDigest()
	return r, nil
}

func (r Revision) Cycle() BusinessCycle {
	c := r.Definition
	c.Periods = append([]Period(nil), c.Periods...)
	c.Phases = append([]Phase(nil), c.Phases...)
	return c
}
func (r Revision) ValidAt(t time.Time) bool {
	t = t.UTC()
	return !t.Before(r.EffectiveFrom) && (r.EffectiveTo.IsZero() || t.Before(r.EffectiveTo))
}
func (r Revision) CanonicalDigest() string { return r.Digest }
func (r Revision) computeDigest() string {
	b, _ := json.Marshal(struct {
		C        BusinessCycle
		ID       string
		Version  string
		From, To time.Time
	}{r.Definition, r.ID, r.Version, r.EffectiveFrom, r.EffectiveTo})
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}
