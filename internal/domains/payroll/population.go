package payroll

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PopulationState is the lifecycle of a payroll population revision.
type PopulationState string

const (
	PopulationStateFrozen     PopulationState = "FROZEN"
	PopulationStateSuperseded PopulationState = "SUPERSEDED"

	Frozen     = PopulationStateFrozen
	Superseded = PopulationStateSuperseded
)

// LateEntryPolicy records what the run does with a worker discovered after
// the declared as-of instant. No policy is implied by an empty value.
type LateEntryPolicy string

const (
	LateEntryPolicyExplicitAmendment LateEntryPolicy = "EXPLICIT_AMENDMENT"
	LateEntryPolicyExclude           LateEntryPolicy = "EXCLUDE"
	LateEntryPolicyReview            LateEntryPolicy = "REVIEW"

	LateEntryExplicitAmendment = LateEntryPolicyExplicitAmendment
)

// PopulationAmendmentKind is the closed vocabulary for a membership change.
type PopulationAmendmentKind string

const (
	PopulationAmendmentLateEntry PopulationAmendmentKind = "LATE_ENTRY"
	PopulationAmendmentRemoval   PopulationAmendmentKind = "REMOVAL"

	LateEntry = PopulationAmendmentLateEntry
	Removal   = PopulationAmendmentRemoval
)

var (
	ErrInvalidFrozenPopulation   = errors.New("payroll: invalid frozen population")
	ErrPopulationUnfrozen        = errors.New("payroll: payroll population is not frozen")
	ErrPopulationSuperseded      = errors.New("payroll: payroll population revision is superseded")
	ErrPopulationBindingMismatch = errors.New("payroll: frozen population does not match the run binding")
	ErrPopulationAmbiguous       = errors.New("payroll: population has duplicate employment or pay-group membership")
	ErrPopulationAmendment       = errors.New("payroll: invalid population amendment")
)

// PopulationMember is one resolved employment in the run's pay group. The
// optional MemberRef spelling is retained for callers that use subject refs;
// WorkerRef is canonicalized when both are supplied and must agree.
type PopulationMember struct {
	WorkerRef     string
	MemberRef     string
	EmploymentRef string
	PayGroupRef   string
}

func (m PopulationMember) workerRef() string {
	if m.WorkerRef != "" {
		return m.WorkerRef
	}
	return m.MemberRef
}

func (m PopulationMember) Validate(expectedPayGroup string) error {
	worker := strings.TrimSpace(m.workerRef())
	if worker == "" || strings.TrimSpace(m.EmploymentRef) == "" || strings.TrimSpace(m.PayGroupRef) == "" {
		return fmt.Errorf("%w: worker, employment and pay group references are required", ErrInvalidFrozenPopulation)
	}
	if m.WorkerRef != "" && m.MemberRef != "" && m.WorkerRef != m.MemberRef {
		return fmt.Errorf("%w: worker and member references disagree", ErrInvalidFrozenPopulation)
	}
	if expectedPayGroup != "" && m.PayGroupRef != expectedPayGroup {
		return fmt.Errorf("%w: member pay group %q differs from run pay group %q", ErrPopulationAmbiguous, m.PayGroupRef, expectedPayGroup)
	}
	return nil
}

func (m PopulationMember) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.payroll.PopulationMember", 1).
		String("worker_ref", m.workerRef()).
		String("employment_ref", m.EmploymentRef).
		String("pay_group_ref", m.PayGroupRef)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// MemberList returns a defensive copy in canonical order.
func (p FrozenPopulation) MemberList() []PopulationMember {
	members := append([]PopulationMember(nil), p.Members...)
	sort.Slice(members, func(i, j int) bool { return memberKey(members[i]) < memberKey(members[j]) })
	return members
}

func memberKey(m PopulationMember) string {
	return m.workerRef() + "\x00" + m.EmploymentRef + "\x00" + m.PayGroupRef
}

// FrozenPopulation is an immutable, digested membership revision resolved
// from one payroll run's PopulationBindingRef at AsOf. An amendment returns a
// distinct value and records the prior digest in SupersedesDigest.
type FrozenPopulation struct {
	RunID            string
	RunRevision      uint64
	PayGroupRef      string
	Binding          PopulationBindingRef
	AsOf             values.Instant
	Members          []PopulationMember
	LateEntryPolicy  LateEntryPolicy
	Revision         uint64
	State            PopulationState
	SupersedesDigest string
	AmendmentDigest  string
	Digest           string
}

func (p FrozenPopulation) policyValid() bool {
	return p.LateEntryPolicy == LateEntryPolicyExplicitAmendment ||
		p.LateEntryPolicy == LateEntryPolicyExclude || p.LateEntryPolicy == LateEntryPolicyReview
}

func (p FrozenPopulation) validateMembers() error {
	seenEmployment := make(map[string]struct{}, len(p.Members))
	seenWorker := make(map[string]struct{}, len(p.Members))
	for _, member := range p.Members {
		if err := member.Validate(p.PayGroupRef); err != nil {
			return err
		}
		if _, ok := seenEmployment[member.EmploymentRef]; ok {
			return fmt.Errorf("%w: duplicate employment %q", ErrPopulationAmbiguous, member.EmploymentRef)
		}
		worker := member.workerRef()
		if _, ok := seenWorker[worker]; ok {
			return fmt.Errorf("%w: worker %q has ambiguous duplicate pay-group membership", ErrPopulationAmbiguous, worker)
		}
		seenEmployment[member.EmploymentRef] = struct{}{}
		seenWorker[worker] = struct{}{}
	}
	return nil
}

func (p FrozenPopulation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.payroll.FrozenPopulation", 1).
		String("run_id", p.RunID).
		Int("run_revision", int64(p.RunRevision)).
		String("pay_group_ref", p.PayGroupRef).
		Value("binding", p.Binding).
		Value("as_of", p.AsOf).
		String("late_entry_policy", string(p.LateEntryPolicy)).
		String("state", string(p.State)).
		Int("revision", int64(p.Revision)).
		String("supersedes_digest", p.SupersedesDigest).
		String("amendment_digest", p.AmendmentDigest).
		Count("members", len(p.Members))
	for _, member := range p.MemberList() {
		w.Field("member", member.canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (p FrozenPopulation) computedDigest() string { return canonicalbytes.Digest(p.body()) }

// Validate verifies the frozen snapshot and its content digest.
func (p FrozenPopulation) Validate() error {
	if strings.TrimSpace(p.RunID) == "" || p.RunRevision == 0 || strings.TrimSpace(p.PayGroupRef) == "" {
		return fmt.Errorf("%w: run and pay-group references are required", ErrInvalidFrozenPopulation)
	}
	if err := p.Binding.Validate(); err != nil {
		return fmt.Errorf("%w: binding: %v", ErrInvalidFrozenPopulation, err)
	}
	if err := p.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as_of: %v", ErrInvalidFrozenPopulation, err)
	}
	if !p.policyValid() {
		return fmt.Errorf("%w: late-entry policy is required", ErrInvalidFrozenPopulation)
	}
	if p.Revision == 0 || !p.State.Valid() {
		return fmt.Errorf("%w: frozen revision and state are required", ErrInvalidFrozenPopulation)
	}
	if err := p.validateMembers(); err != nil {
		return err
	}
	if p.Digest == "" || p.Digest != p.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidFrozenPopulation)
	}
	return nil
}

func (s PopulationState) Valid() bool {
	return s == PopulationStateFrozen || s == PopulationStateSuperseded
}

// FreezePopulation resolves and freezes members against the run's exact
// population binding and declared as-of instant. The input slice is copied.
func FreezePopulation(run PayrollRun, asOf values.Instant, members []PopulationMember, policy LateEntryPolicy) (FrozenPopulation, error) {
	if err := run.Validate(); err != nil {
		return FrozenPopulation{}, err
	}
	if err := asOf.Validate(); err != nil {
		return FrozenPopulation{}, fmt.Errorf("%w: as_of: %v", ErrInvalidFrozenPopulation, err)
	}
	p := FrozenPopulation{
		RunID: run.RunID, RunRevision: run.Revision, PayGroupRef: run.PayGroupRef,
		Binding: run.Population, AsOf: asOf, Members: append([]PopulationMember(nil), members...),
		LateEntryPolicy: policy, Revision: 1, State: PopulationStateFrozen,
	}
	if !p.policyValid() {
		return FrozenPopulation{}, fmt.Errorf("%w: late-entry policy is required", ErrInvalidFrozenPopulation)
	}
	if err := p.validateMembers(); err != nil {
		return FrozenPopulation{}, err
	}
	p.Digest = p.computedDigest()
	return p, nil
}

// FreezePayrollPopulation is the descriptive spelling of FreezePopulation.
func FreezePayrollPopulation(run PayrollRun, asOf values.Instant, members []PopulationMember, policy LateEntryPolicy) (FrozenPopulation, error) {
	return FreezePopulation(run, asOf, members, policy)
}

// PopulationAmendment is an explicit, digested late-entry or removal request.
type PopulationAmendment struct {
	Kind          PopulationAmendmentKind
	Member        PopulationMember
	Reason        string
	EffectiveAsOf values.Instant
	Digest        string
}

func (a PopulationAmendment) Validate(expectedPayGroup string) error {
	if a.Kind != PopulationAmendmentLateEntry && a.Kind != PopulationAmendmentRemoval {
		return fmt.Errorf("%w: unknown amendment kind %q", ErrPopulationAmendment, a.Kind)
	}
	if strings.TrimSpace(a.Reason) == "" {
		return fmt.Errorf("%w: reason is required", ErrPopulationAmendment)
	}
	if err := a.Member.Validate(expectedPayGroup); err != nil {
		return fmt.Errorf("%w: member: %v", ErrPopulationAmendment, err)
	}
	if err := a.EffectiveAsOf.Validate(); err != nil {
		return fmt.Errorf("%w: effective as-of: %v", ErrPopulationAmendment, err)
	}
	return nil
}

func (a PopulationAmendment) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.payroll.PopulationAmendment", 1).
		String("kind", string(a.Kind)).Field("member", a.Member.canonical()).
		String("reason", a.Reason).Value("effective_as_of", a.EffectiveAsOf)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// NewPopulationAmendment validates and digests an explicit membership change.
func NewPopulationAmendment(kind PopulationAmendmentKind, member PopulationMember, reason string, effectiveAsOf values.Instant) (PopulationAmendment, error) {
	a := PopulationAmendment{Kind: kind, Member: member, Reason: reason, EffectiveAsOf: effectiveAsOf}
	if err := a.Validate(member.PayGroupRef); err != nil {
		return PopulationAmendment{}, err
	}
	a.Digest = canonicalbytes.Digest(a.body())
	return a, nil
}

// Amend creates a new frozen revision. It never changes p; callers must retain
// the returned revision as the authoritative successor.
func (p FrozenPopulation) Amend(amendment PopulationAmendment) (FrozenPopulation, error) {
	if err := p.Validate(); err != nil {
		return FrozenPopulation{}, err
	}
	if p.State != PopulationStateFrozen {
		return FrozenPopulation{}, ErrPopulationSuperseded
	}
	if err := amendment.Validate(p.PayGroupRef); err != nil {
		return FrozenPopulation{}, err
	}
	if amendment.Digest == "" || amendment.Digest != canonicalbytes.Digest(amendment.body()) {
		return FrozenPopulation{}, fmt.Errorf("%w: amendment digest mismatch", ErrPopulationAmendment)
	}
	next := p
	next.Members = p.MemberList()
	key := memberKey(amendment.Member)
	index := -1
	for i, member := range next.Members {
		if memberKey(member) == key || member.workerRef() == amendment.Member.workerRef() {
			index = i
			break
		}
	}
	switch amendment.Kind {
	case PopulationAmendmentLateEntry:
		if index >= 0 {
			return FrozenPopulation{}, fmt.Errorf("%w: late-entry member already frozen", ErrPopulationAmendment)
		}
		next.Members = append(next.Members, amendment.Member)
	case PopulationAmendmentRemoval:
		if index < 0 {
			return FrozenPopulation{}, fmt.Errorf("%w: removal member is not frozen", ErrPopulationAmendment)
		}
		next.Members = append(next.Members[:index], next.Members[index+1:]...)
	}
	next.Revision++
	next.SupersedesDigest = p.Digest
	next.AmendmentDigest = amendment.Digest
	next.Digest = ""
	if err := next.validateMembers(); err != nil {
		return FrozenPopulation{}, err
	}
	next.Digest = next.computedDigest()
	return next, nil
}

// SupersededRevision returns a value representing p after a successor was
// recorded. The original p remains immutable and unchanged.
func (p FrozenPopulation) SupersededRevision(successor FrozenPopulation) (FrozenPopulation, error) {
	if err := p.Validate(); err != nil {
		return FrozenPopulation{}, err
	}
	if successor.SupersedesDigest != p.Digest {
		return FrozenPopulation{}, fmt.Errorf("%w: successor does not name this revision", ErrPopulationAmendment)
	}
	p.State = PopulationStateSuperseded
	p.Digest = p.computedDigest()
	return p, nil
}

// CalculateAgainstPopulation refuses a population that is not the current
// frozen revision for run and then advances the run immutably to CALCULATED.
func CalculateAgainstPopulation(run PayrollRun, population FrozenPopulation, calculationDigest string) (PayrollRun, error) {
	if err := run.Validate(); err != nil {
		return PayrollRun{}, err
	}
	if population.State == PopulationStateSuperseded {
		return PayrollRun{}, ErrPopulationSuperseded
	}
	if population.State != PopulationStateFrozen {
		return PayrollRun{}, ErrPopulationUnfrozen
	}
	if err := population.Validate(); err != nil {
		return PayrollRun{}, err
	}
	if run.State != PayrollRunStateDraft {
		return PayrollRun{}, fmt.Errorf("%w: run state %s", ErrPopulationUnfrozen, run.State)
	}
	if population.RunID != run.RunID || population.Binding != run.Population || population.PayGroupRef != run.PayGroupRef {
		return PayrollRun{}, ErrPopulationBindingMismatch
	}
	if strings.TrimSpace(calculationDigest) == "" {
		return PayrollRun{}, fmt.Errorf("%w: calculation digest is required", ErrInvalidFrozenPopulation)
	}
	return run.Calculate(calculationDigest)
}

// Explain is the package-level contract spelling for a frozen population.
func (p FrozenPopulation) Explain() (FrozenPopulationExplanation, error) {
	if err := p.Validate(); err != nil {
		return FrozenPopulationExplanation{}, err
	}
	return FrozenPopulationExplanation{RunID: p.RunID, Revision: p.Revision, State: p.State, AsOf: p.AsOf, MemberCount: len(p.Members), Binding: p.Binding, Digest: p.Digest, LateEntryPolicy: p.LateEntryPolicy}, nil
}

type FrozenPopulationExplanation struct {
	RunID           string
	Revision        uint64
	State           PopulationState
	AsOf            values.Instant
	MemberCount     int
	Binding         PopulationBindingRef
	LateEntryPolicy LateEntryPolicy
	Digest          string
}

func ExplainPopulation(p FrozenPopulation) (FrozenPopulationExplanation, error) { return p.Explain() }
