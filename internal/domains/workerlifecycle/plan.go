// Package workerlifecycle owns the meaning and immutable shape of worker
// onboarding and offboarding plans. It only describes requirements; workflow
// and intent infrastructure is responsible for executing child work.
package workerlifecycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

var (
	ErrInvalidPlan        = errors.New("worker lifecycle: invalid plan")
	ErrRequirementInvalid = errors.New("worker lifecycle: invalid requirement")
	ErrChildGraph         = errors.New("worker lifecycle: invalid child graph")
	ErrPlanMutation       = errors.New("worker lifecycle: plan digest mismatch")
)

type EventKind string

const (
	EventStart EventKind = "START"
	EventEnd   EventKind = "END"
)

func (e EventKind) valid() bool { return e == EventStart || e == EventEnd }

type CompletionPolicy string

const (
	CompleteAllRequired  CompletionPolicy = "ALL_REQUIRED"
	CompleteBusinessOnly CompletionPolicy = "BUSINESS_ONLY"
)

func (p CompletionPolicy) valid() bool { return p == CompleteAllRequired || p == CompleteBusinessOnly }

// DueRule is explicit and calendar-bound. OffsetDays is relative to the
// event date; no wall clock or ambient tenant calendar is consulted.
type DueRule struct {
	Calendar   values.CalendarRef
	OffsetDays int
}

func (d DueRule) Validate() error {
	if err := d.Calendar.Validate(); err != nil {
		return fmt.Errorf("%w: calendar: %v", ErrRequirementInvalid, err)
	}
	return nil
}

type Requirement struct {
	ID       string
	Ordinal  int
	Owner    string
	Due      DueRule
	Evidence []values.EntityRef
	// VerificationPolicy identifies the governed policy that decides whether
	// referenced evidence satisfies this requirement. References alone never
	// assert truth or completion.
	VerificationPolicy values.EntityRef
	Completion         CompletionPolicy
	Required           bool
}

func (r Requirement) Validate() error {
	if strings.TrimSpace(r.ID) == "" || r.Ordinal <= 0 || strings.TrimSpace(r.Owner) == "" {
		return ErrRequirementInvalid
	}
	if err := r.Due.Validate(); err != nil {
		return err
	}
	if !r.Completion.valid() || len(r.Evidence) == 0 {
		return ErrRequirementInvalid
	}
	if err := r.VerificationPolicy.Validate(); err != nil || r.VerificationPolicy.Kind != values.Kind("evidence_policy") {
		return fmt.Errorf("%w: verification policy", ErrRequirementInvalid)
	}
	seen := map[string]struct{}{}
	for _, ref := range r.Evidence {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: evidence: %v", ErrRequirementInvalid, err)
		}
		if _, ok := seen[ref.String()]; ok {
			return fmt.Errorf("%w: duplicate evidence", ErrRequirementInvalid)
		}
		seen[ref.String()] = struct{}{}
	}
	return nil
}

// ChildTemplate is an ordered, bounded intent template. It contains no
// provider, task or external-effect instruction.
type ChildTemplate struct {
	ID            string
	Ordinal       int
	IntentType    string
	IntentVersion string
	DependsOn     []string
}

func (c ChildTemplate) Validate() error {
	if strings.TrimSpace(c.ID) == "" || c.Ordinal <= 0 || strings.TrimSpace(c.IntentType) == "" || strings.TrimSpace(c.IntentVersion) == "" {
		return ErrChildGraph
	}
	seen := map[string]struct{}{}
	for _, dep := range c.DependsOn {
		if strings.TrimSpace(dep) == "" || dep == c.ID {
			return ErrChildGraph
		}
		if _, ok := seen[dep]; ok {
			return ErrChildGraph
		}
		seen[dep] = struct{}{}
	}
	return nil
}

type WorkerLifecyclePlan struct {
	Worker       values.EntityRef
	Employment   values.EntityRef
	Proposal     values.EntityRef
	Event        EventKind
	EventDate    values.LocalDate
	Requirements []Requirement
	Children     []ChildTemplate
	Completion   CompletionPolicy
	// ApprovalDecision is an opaque reference to a separately governed
	// approval. Its presence records a binding; it does not grant authority.
	ApprovalDecision values.EntityRef
	CanonicalDigest  string
}

func (p WorkerLifecyclePlan) Validate() error {
	if err := p.Worker.Validate(); err != nil || p.Worker.Kind != values.Kind("worker") {
		return fmt.Errorf("%w: worker", ErrInvalidPlan)
	}
	if err := p.Employment.Validate(); err != nil || p.Employment.Kind != values.Kind("employment") || p.Employment.Tenant != p.Worker.Tenant {
		return fmt.Errorf("%w: employment", ErrInvalidPlan)
	}
	if err := p.Proposal.Validate(); err != nil || p.Proposal.Kind != values.Kind("proposal") || p.Proposal.Tenant != p.Worker.Tenant {
		return fmt.Errorf("%w: proposal", ErrInvalidPlan)
	}
	if !p.Event.valid() || p.EventDate.Validate() != nil || !p.Completion.valid() || len(p.Requirements) == 0 {
		return ErrInvalidPlan
	}
	if p.ApprovalDecision != (values.EntityRef{}) {
		if err := p.ApprovalDecision.Validate(); err != nil || p.ApprovalDecision.Kind != values.Kind("approval_decision") || p.ApprovalDecision.Tenant != p.Worker.Tenant {
			return fmt.Errorf("%w: approval decision", ErrInvalidPlan)
		}
	}
	seenReq := map[string]struct{}{}
	requirementOrdinals := map[int]struct{}{}
	for _, r := range p.Requirements {
		if err := r.Validate(); err != nil {
			return err
		}
		for _, evidence := range r.Evidence {
			if evidence.Tenant != p.Worker.Tenant {
				return fmt.Errorf("%w: requirement evidence tenant differs", ErrRequirementInvalid)
			}
		}
		if r.VerificationPolicy.Tenant != p.Worker.Tenant {
			return fmt.Errorf("%w: verification policy tenant differs", ErrRequirementInvalid)
		}
		if _, ok := seenReq[r.ID]; ok {
			return ErrRequirementInvalid
		}
		seenReq[r.ID] = struct{}{}
		if _, ok := requirementOrdinals[r.Ordinal]; ok {
			return ErrRequirementInvalid
		}
		requirementOrdinals[r.Ordinal] = struct{}{}
	}
	for ordinal := 1; ordinal <= len(p.Requirements); ordinal++ {
		if _, ok := requirementOrdinals[ordinal]; !ok {
			return ErrRequirementInvalid
		}
	}
	seenChild := map[string]ChildTemplate{}
	ordinals := map[int]struct{}{}
	for _, c := range p.Children {
		if err := c.Validate(); err != nil {
			return err
		}
		if _, ok := seenChild[c.ID]; ok {
			return ErrChildGraph
		}
		if _, ok := ordinals[c.Ordinal]; ok {
			return ErrChildGraph
		}
		seenChild[c.ID] = c
		ordinals[c.Ordinal] = struct{}{}
	}
	for _, c := range p.Children {
		for _, dep := range c.DependsOn {
			dependency, ok := seenChild[dep]
			if !ok || dependency.Ordinal >= c.Ordinal {
				return ErrChildGraph
			}
		}
	}
	for ordinal := 1; ordinal <= len(p.Children); ordinal++ {
		if _, ok := ordinals[ordinal]; !ok {
			return ErrChildGraph
		}
	}
	if hasCycle(seenChild) {
		return ErrChildGraph
	}
	if p.CanonicalDigest != "" {
		digest, err := p.computedDigest()
		if err != nil {
			return fmt.Errorf("%w: canonical encoding: %v", ErrInvalidPlan, err)
		}
		if p.CanonicalDigest != digest {
			return ErrPlanMutation
		}
	}
	return nil
}

func hasCycle(nodes map[string]ChildTemplate) bool {
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, dep := range nodes[id].DependsOn {
			if visit(dep) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range nodes {
		if visit(id) {
			return true
		}
	}
	return false
}

func (p WorkerLifecyclePlan) body() ([]byte, error) {
	r := append([]Requirement(nil), p.Requirements...)
	sort.Slice(r, func(i, j int) bool { return r[i].Ordinal < r[j].Ordinal })
	c := append([]ChildTemplate(nil), p.Children...)
	sort.Slice(c, func(i, j int) bool { return c[i].Ordinal < c[j].Ordinal })
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.WorkerLifecyclePlan", schemaVersion).Value("worker", p.Worker).Value("employment", p.Employment).Value("proposal", p.Proposal).String("event", string(p.Event)).Value("event_date", p.EventDate).String("completion", string(p.Completion)).String("approval_tenant", string(p.ApprovalDecision.Tenant)).String("approval_kind", string(p.ApprovalDecision.Kind)).String("approval_id", p.ApprovalDecision.Id).Count("requirements", len(r))
	for _, x := range r {
		w.String("requirement_id", x.ID).Int("requirement_ordinal", int64(x.Ordinal)).String("owner", x.Owner).String("due_calendar_ref", x.Due.Calendar.Ref).String("due_calendar_version", x.Due.Calendar.Version).Int("due_offset", int64(x.Due.OffsetDays)).Value("verification_policy", x.VerificationPolicy).String("requirement_completion", string(x.Completion)).Bool("required", x.Required).Count("evidence", len(x.Evidence))
		for _, e := range x.Evidence {
			w.Value("evidence", e)
		}
	}
	w.Count("children", len(c))
	for _, x := range c {
		w.String("child_id", x.ID).Int("ordinal", int64(x.Ordinal)).String("intent_type", x.IntentType).String("intent_version", x.IntentVersion)
		for _, d := range x.DependsOn {
			w.String("depends_on", d)
		}
	}
	return w.Bytes()
}
func (p WorkerLifecyclePlan) computedDigest() (string, error) {
	b, err := p.body()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}
func (p WorkerLifecyclePlan) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest()
}
func (p WorkerLifecyclePlan) Canonical() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p.body()
}

func NewPlan(p WorkerLifecyclePlan) (WorkerLifecyclePlan, error) {
	// A supplied digest is a seal from an earlier construction. Validate it
	// before copying or recomputing so reconstruction cannot bless mutation.
	if p.CanonicalDigest != "" {
		if err := p.Validate(); err != nil {
			return WorkerLifecyclePlan{}, err
		}
	}
	p.Requirements = append([]Requirement(nil), p.Requirements...)
	p.Children = append([]ChildTemplate(nil), p.Children...)
	for i := range p.Requirements {
		p.Requirements[i].Evidence = append([]values.EntityRef(nil), p.Requirements[i].Evidence...)
	}
	for i := range p.Children {
		p.Children[i].DependsOn = append([]string(nil), p.Children[i].DependsOn...)
	}
	digest, err := p.computedDigest()
	if err != nil {
		return WorkerLifecyclePlan{}, fmt.Errorf("%w: canonical encoding: %v", ErrInvalidPlan, err)
	}
	p.CanonicalDigest = digest
	if err := p.Validate(); err != nil {
		return WorkerLifecyclePlan{}, err
	}
	return p, nil
}
