// Package equity owns the pure, immutable vocabulary for equity plans, grants,
// vesting, and acceptance evidence. It never allocates shares, persists an
// event, or calls a broker, payroll system, or document provider.
package equity

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

var (
	ErrInvalidPlan       = errors.New("equity: invalid plan revision")
	ErrInvalidGrant      = errors.New("equity: invalid grant revision")
	ErrInvalidVesting    = errors.New("equity: invalid vesting schedule")
	ErrInvalidAcceptance = errors.New("equity: invalid acceptance event")
	ErrGrantTransition   = errors.New("equity: grant transition is not allowed")
)

// InstrumentKind is a closed equity instrument vocabulary.
type InstrumentKind string

const (
	InstrumentOption InstrumentKind = "OPTION"
	InstrumentRSU    InstrumentKind = "RSU"
	InstrumentRSA    InstrumentKind = "RSA"
	InstrumentPSU    InstrumentKind = "PSU"
	InstrumentSAR    InstrumentKind = "SAR"
	InstrumentShare  InstrumentKind = "SHARE"

	Option = InstrumentOption
	RSU    = InstrumentRSU
	RSA    = InstrumentRSA
	PSU    = InstrumentPSU
	SAR    = InstrumentSAR
	Share  = InstrumentShare
)

func (k InstrumentKind) Valid() bool {
	switch k {
	case InstrumentOption, InstrumentRSU, InstrumentRSA, InstrumentPSU, InstrumentSAR, InstrumentShare:
		return true
	default:
		return false
	}
}

// EquityPlanRevision pins the authorized pool and instrument vocabulary for a
// grant. AuthorizedQuantity and all grant prices remain exact decimals.
type EquityPlanRevision struct {
	PlanID             string
	Revision           uint64
	Name               string
	PoolRef            string
	AuthorizedQuantity values.Decimal
	Currency           string
	InstrumentKinds    []InstrumentKind
	Instruments        []InstrumentKind
	ApprovalRef        string
	ParentDigest       string
	SupersedesRevision uint64
	CanonicalDigest    string
	Digest             string
}

type EquityPlan = EquityPlanRevision

func (p EquityPlanRevision) normalizedInstruments() []InstrumentKind {
	if len(p.InstrumentKinds) > 0 {
		return append([]InstrumentKind(nil), p.InstrumentKinds...)
	}
	return append([]InstrumentKind(nil), p.Instruments...)
}

func (p EquityPlanRevision) Validate() error {
	for field, value := range map[string]string{"plan_id": p.PlanID, "name": p.Name, "pool_ref": p.PoolRef, "currency": p.Currency, "approval_ref": p.ApprovalRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPlan, field)
		}
	}
	if p.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidPlan)
	}
	if err := p.AuthorizedQuantity.Validate(); err != nil {
		return fmt.Errorf("%w: authorized_quantity: %v", ErrInvalidPlan, err)
	}
	if p.AuthorizedQuantity.Sign() <= 0 {
		return fmt.Errorf("%w: authorized_quantity must be positive", ErrInvalidPlan)
	}
	if p.Revision == 1 && (p.ParentDigest != "" || p.SupersedesRevision != 0) {
		return fmt.Errorf("%w: first revision cannot have lineage", ErrInvalidPlan)
	}
	if p.Revision > 1 && (strings.TrimSpace(p.ParentDigest) == "" || p.SupersedesRevision == 0 || p.SupersedesRevision >= p.Revision) {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidPlan)
	}
	instruments := p.normalizedInstruments()
	if len(instruments) == 0 {
		return fmt.Errorf("%w: at least one instrument kind is required", ErrInvalidPlan)
	}
	seen := make(map[InstrumentKind]struct{}, len(instruments))
	for _, kind := range instruments {
		if !kind.Valid() {
			return fmt.Errorf("%w: instrument kind %q is not declared", ErrInvalidPlan, kind)
		}
		if _, ok := seen[kind]; ok {
			return fmt.Errorf("%w: duplicate instrument kind %q", ErrInvalidPlan, kind)
		}
		seen[kind] = struct{}{}
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidPlan)
	}
	if p.Digest != "" && p.Digest != p.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidPlan)
	}
	return nil
}

func (p EquityPlanRevision) canonicalWithoutDigest() []byte {
	instruments := p.normalizedInstruments()
	sort.Slice(instruments, func(i, j int) bool { return instruments[i] < instruments[j] })
	w := canonicalbytes.New("hcmnext.domains.equity.EquityPlanRevision", schemaVersion).
		String("plan_id", p.PlanID).Int("revision", int64(p.Revision)).String("name", p.Name).
		String("pool_ref", p.PoolRef).Value("authorized_quantity", p.AuthorizedQuantity).
		String("currency", p.Currency).String("approval_ref", p.ApprovalRef).
		String("parent_digest", p.ParentDigest).Int("supersedes_revision", int64(p.SupersedesRevision)).
		Count("instrument_kinds", len(instruments))
	for _, kind := range instruments {
		w.String("instrument_kind", string(kind))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p EquityPlanRevision) computedDigest() string {
	return canonicalbytes.Digest(p.canonicalWithoutDigest())
}
func (p EquityPlanRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.canonicalWithoutDigest()
}
func (p EquityPlanRevision) DigestValue() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}

func NewEquityPlanRevision(p EquityPlanRevision) (EquityPlanRevision, error) {
	p.InstrumentKinds = p.normalizedInstruments()
	p.Instruments = append([]InstrumentKind(nil), p.InstrumentKinds...)
	p.CanonicalDigest, p.Digest = "", ""
	if err := p.Validate(); err != nil {
		return EquityPlanRevision{}, err
	}
	digest := p.computedDigest()
	p.CanonicalDigest, p.Digest = digest, digest
	return p, nil
}

func NewPlanRevision(p EquityPlanRevision) (EquityPlanRevision, error) {
	return NewEquityPlanRevision(p)
}

type PlanExplanation struct {
	PlanID          string
	Revision        uint64
	PoolRef         string
	InstrumentKinds []InstrumentKind
	Digest          string
}

func (p EquityPlanRevision) Explain() (PlanExplanation, error) {
	if err := p.Validate(); err != nil {
		return PlanExplanation{}, err
	}
	instruments := p.normalizedInstruments()
	sort.Slice(instruments, func(i, j int) bool { return instruments[i] < instruments[j] })
	return PlanExplanation{PlanID: p.PlanID, Revision: p.Revision, PoolRef: p.PoolRef, InstrumentKinds: instruments, Digest: p.computedDigest()}, nil
}

func ExplainPlan(p EquityPlanRevision) (PlanExplanation, error) { return p.Explain() }

func (p EquityPlanRevision) Successor(next EquityPlanRevision) (EquityPlanRevision, error) {
	if err := p.Validate(); err != nil {
		return EquityPlanRevision{}, err
	}
	next.PlanID, next.ParentDigest, next.SupersedesRevision = p.PlanID, p.computedDigest(), p.Revision
	if next.Revision == 0 {
		next.Revision = p.Revision + 1
	}
	return NewEquityPlanRevision(next)
}

// ValidateGrant binds a grant to the exact plan pool and instrument policy.
// It is separate from grant validation so a grant can still be parsed before
// a caller selects the authoritative plan revision to evaluate it against.
func (p EquityPlanRevision) ValidateGrant(g EquityGrant) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := g.Validate(); err != nil {
		return err
	}
	if g.PlanDigest != p.computedDigest() || g.PlanRevision != p.Revision {
		return fmt.Errorf("%w: plan digest or revision does not match grant", ErrInvalidGrant)
	}
	if g.PoolRef != p.PoolRef {
		return fmt.Errorf("%w: pool_ref does not match plan", ErrInvalidGrant)
	}
	allowed := false
	for _, kind := range p.normalizedInstruments() {
		if kind == g.Instrument {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: instrument %q is not authorized by plan", ErrInvalidGrant, g.Instrument)
	}
	if g.Quantity.Cmp(p.AuthorizedQuantity) > 0 {
		return fmt.Errorf("%w: quantity exceeds authorized pool", ErrInvalidGrant)
	}
	return nil
}

// CalendarRule makes vesting dates deterministic and explicit.
type CalendarRule string

const (
	CalendarGregorian CalendarRule = "GREGORIAN_ANNIVERSARY"
	CalendarFixed30   CalendarRule = "FIXED_30_DAY"

	GregorianAnniversary = CalendarGregorian
	FixedThirtyDay       = CalendarFixed30
)

func (r CalendarRule) Valid() bool { return r == CalendarGregorian || r == CalendarFixed30 }

// VestingSchedule describes a cliff followed by equally spaced periodic
// tranches. TrancheCount includes the cliff tranche.
type VestingSchedule struct {
	CalendarRule   CalendarRule
	Calendar       CalendarRule
	CliffMonths    int
	CliffPeriod    int
	PeriodicMonths int
	PeriodMonths   int
	TrancheCount   int
	TotalTranches  int
}

func (s VestingSchedule) normalized() VestingSchedule {
	if !s.CalendarRule.Valid() {
		s.CalendarRule = s.Calendar
	}
	if s.CliffMonths == 0 {
		s.CliffMonths = s.CliffPeriod
	}
	if s.PeriodicMonths == 0 {
		s.PeriodicMonths = s.PeriodMonths
	}
	if s.TrancheCount == 0 {
		s.TrancheCount = s.TotalTranches
	}
	return s
}

func (s VestingSchedule) Validate() error {
	s = s.normalized()
	if !s.CalendarRule.Valid() {
		return fmt.Errorf("%w: calendar_rule %q is not declared", ErrInvalidVesting, s.CalendarRule)
	}
	if s.CliffMonths < 0 || s.PeriodicMonths <= 0 || s.TrancheCount <= 0 {
		return fmt.Errorf("%w: cliff must be non-negative and periodic months and tranche count must be positive", ErrInvalidVesting)
	}
	return nil
}

func (s VestingSchedule) Canonical() []byte {
	s = s.normalized()
	if s.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.equity.VestingSchedule", schemaVersion).
		String("calendar_rule", string(s.CalendarRule)).Int("cliff_months", int64(s.CliffMonths)).
		Int("periodic_months", int64(s.PeriodicMonths)).Int("tranche_count", int64(s.TrancheCount)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// VestingTranche is a deterministic date and exact quantity.
type VestingTranche struct {
	Sequence        int
	VestsOn         values.LocalDate
	Quantity        values.Decimal
	Digest          string
	CanonicalDigest string
}

func (t VestingTranche) Validate() error {
	if t.Sequence <= 0 {
		return fmt.Errorf("%w: tranche sequence must be positive", ErrInvalidVesting)
	}
	if err := t.VestsOn.Validate(); err != nil {
		return fmt.Errorf("%w: vests_on: %v", ErrInvalidVesting, err)
	}
	if err := t.Quantity.Validate(); err != nil {
		return fmt.Errorf("%w: quantity: %v", ErrInvalidVesting, err)
	}
	if t.Quantity.Sign() <= 0 {
		return fmt.Errorf("%w: tranche quantity must be positive", ErrInvalidVesting)
	}
	return nil
}

func (t VestingTranche) Canonical() []byte {
	if t.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.equity.VestingTranche", schemaVersion).
		Int("sequence", int64(t.Sequence)).Value("vests_on", t.VestsOn).Value("quantity", t.Quantity).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func vestingDate(start values.LocalDate, months int, rule CalendarRule) (values.LocalDate, error) {
	if err := start.Validate(); err != nil {
		return values.LocalDate{}, err
	}
	if rule == CalendarFixed30 {
		return start.AddDays(months * 30), nil
	}
	if rule != CalendarGregorian {
		return values.LocalDate{}, fmt.Errorf("%w: calendar_rule %q is not declared", ErrInvalidVesting, rule)
	}
	probe := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	// time.Date normalizes an out-of-range day into the following month. Clamp
	// anniversary dates to the last day of the target month instead.
	if probe.Day() != int(start.Day()) {
		probe = time.Date(probe.Year(), probe.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	}
	return values.NewLocalDate(probe.Year(), probe.Month(), probe.Day())
}

// Compute deterministically derives all tranches from the grant date and
// returns detached values. The final tranche receives any exact-decimal
// remainder created by division, so the quantities always sum to the grant.
func (s VestingSchedule) Compute(grantDate values.LocalDate, quantity values.Decimal) ([]VestingTranche, error) {
	s = s.normalized()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if err := grantDate.Validate(); err != nil {
		return nil, fmt.Errorf("%w: grant_date: %v", ErrInvalidVesting, err)
	}
	if err := quantity.Validate(); err != nil || quantity.Sign() <= 0 {
		return nil, fmt.Errorf("%w: quantity must be positive: %v", ErrInvalidVesting, err)
	}
	count, err := values.NewDecimal(fmt.Sprintf("%d", s.TrancheCount), 0, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	per, err := quantity.Div(count, quantity.Scale(), values.RoundingTowardZero)
	if err != nil {
		return nil, fmt.Errorf("%w: equal tranche quantity: %v", ErrInvalidVesting, err)
	}
	remaining := quantity
	out := make([]VestingTranche, 0, s.TrancheCount)
	for i := 1; i <= s.TrancheCount; i++ {
		months := s.CliffMonths + (i-1)*s.PeriodicMonths
		date, err := vestingDate(grantDate, months, s.CalendarRule)
		if err != nil {
			return nil, err
		}
		trancheQuantity := per
		if i == s.TrancheCount {
			trancheQuantity = remaining
		} else {
			remaining, err = remaining.Sub(per)
			if err != nil {
				return nil, fmt.Errorf("%w: tranche remainder: %v", ErrInvalidVesting, err)
			}
		}
		tranche := VestingTranche{Sequence: i, VestsOn: date, Quantity: trancheQuantity}
		tranche.Digest = canonicalbytes.Digest(tranche.Canonical())
		tranche.CanonicalDigest = tranche.Digest
		out = append(out, tranche)
	}
	return out, nil
}

func ComputeVesting(s VestingSchedule, grantDate values.LocalDate, quantity values.Decimal) ([]VestingTranche, error) {
	return s.Compute(grantDate, quantity)
}

// GrantState is the closed grant lifecycle. Forfeiture is an explicit
// terminal revision rather than an overwrite of an accepted grant.
type GrantState string

const (
	GrantProposed  GrantState = "PROPOSED"
	GrantAccepted  GrantState = "ACCEPTED"
	GrantActive    GrantState = "ACTIVE"
	GrantCancelled GrantState = "CANCELLED"
	GrantForfeited GrantState = "FORFEITED"
	GrantSettled   GrantState = "SETTLED"

	Proposed  = GrantProposed
	Accepted  = GrantAccepted
	Active    = GrantActive
	Cancelled = GrantCancelled
	Forfeited = GrantForfeited
	Settled   = GrantSettled
)

func (s GrantState) Valid() bool {
	switch s {
	case GrantProposed, GrantAccepted, GrantActive, GrantCancelled, GrantForfeited, GrantSettled:
		return true
	default:
		return false
	}
}

// AcceptanceEvent is a separately digested event with explicit evidence.
type AcceptanceEvent struct {
	EventID         string
	GrantDigest     string
	GrantRevision   uint64
	AcceptedBy      string
	AcceptedAt      values.Instant
	EvidenceRef     string
	CanonicalDigest string
	Digest          string
}

func (e AcceptanceEvent) Validate() error {
	for field, value := range map[string]string{"event_id": e.EventID, "grant_digest": e.GrantDigest, "accepted_by": e.AcceptedBy, "evidence_ref": e.EvidenceRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidAcceptance, field)
		}
	}
	if e.GrantRevision == 0 {
		return fmt.Errorf("%w: grant_revision is required", ErrInvalidAcceptance)
	}
	if err := e.AcceptedAt.Validate(); err != nil {
		return fmt.Errorf("%w: accepted_at: %v", ErrInvalidAcceptance, err)
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidAcceptance)
	}
	if e.Digest != "" && e.Digest != e.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidAcceptance)
	}
	return nil
}

func (e AcceptanceEvent) canonicalWithoutDigest() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.equity.AcceptanceEvent", schemaVersion).
		String("event_id", e.EventID).String("grant_digest", e.GrantDigest).Int("grant_revision", int64(e.GrantRevision)).
		String("accepted_by", e.AcceptedBy).Value("accepted_at", e.AcceptedAt).String("evidence_ref", e.EvidenceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (e AcceptanceEvent) computedDigest() string {
	return canonicalbytes.Digest(e.canonicalWithoutDigest())
}
func (e AcceptanceEvent) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.canonicalWithoutDigest()
}

func NewAcceptanceEvent(e AcceptanceEvent) (AcceptanceEvent, error) {
	e.CanonicalDigest, e.Digest = "", ""
	if err := e.Validate(); err != nil {
		return AcceptanceEvent{}, err
	}
	digest := e.computedDigest()
	e.CanonicalDigest, e.Digest = digest, digest
	return e, nil
}

func NewAcceptance(e AcceptanceEvent) (AcceptanceEvent, error) { return NewAcceptanceEvent(e) }

// EquityGrant is an immutable grant revision. Price accepts either the
// descriptive Price field or StrikePrice; exactly one semantic price is
// retained by construction.
type EquityGrant struct {
	GrantID            string
	Revision           uint64
	PlanDigest         string
	PlanRevision       uint64
	PoolRef            string
	WorkerRef          string
	Instrument         InstrumentKind
	InstrumentKind     InstrumentKind
	Quantity           values.Decimal
	GrantDate          values.LocalDate
	Date               values.LocalDate
	StrikePrice        values.Decimal
	Price              values.Decimal
	Currency           string
	Vesting            VestingSchedule
	State              GrantState
	AcceptanceDigest   string
	ApprovalRef        string
	EvidenceRef        string
	ParentDigest       string
	SupersedesRevision uint64
	CanonicalDigest    string
	Digest             string
}

func (g EquityGrant) normalized() (EquityGrant, error) {
	if !g.Instrument.Valid() {
		g.Instrument = g.InstrumentKind
	}
	if !g.InstrumentKind.Valid() {
		g.InstrumentKind = g.Instrument
	}
	if g.GrantDate.Validate() != nil {
		g.GrantDate = g.Date
	}
	if g.Date.Validate() != nil {
		g.Date = g.GrantDate
	}
	if g.StrikePrice.Validate() != nil {
		g.StrikePrice = g.Price
	}
	if g.Price.Validate() != nil {
		g.Price = g.StrikePrice
	}
	return g, nil
}

func (g EquityGrant) Validate() error {
	n, err := g.normalized()
	if err != nil {
		return err
	}
	for field, value := range map[string]string{"grant_id": n.GrantID, "plan_digest": n.PlanDigest, "pool_ref": n.PoolRef, "worker_ref": n.WorkerRef, "currency": n.Currency} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidGrant, field)
		}
	}
	if n.Revision == 0 || n.PlanRevision == 0 {
		return fmt.Errorf("%w: revision and plan_revision are required", ErrInvalidGrant)
	}
	if !n.Instrument.Valid() {
		return fmt.Errorf("%w: instrument kind %q is not declared", ErrInvalidGrant, n.Instrument)
	}
	if err := n.Quantity.Validate(); err != nil || n.Quantity.Sign() <= 0 {
		return fmt.Errorf("%w: quantity must be positive: %v", ErrInvalidGrant, err)
	}
	if err := n.GrantDate.Validate(); err != nil {
		return fmt.Errorf("%w: grant_date: %v", ErrInvalidGrant, err)
	}
	if err := n.StrikePrice.Validate(); err != nil {
		return fmt.Errorf("%w: strike_price or price: %v", ErrInvalidGrant, err)
	}
	if n.StrikePrice.Sign() < 0 {
		return fmt.Errorf("%w: strike_price must not be negative", ErrInvalidGrant)
	}
	if !n.State.Valid() {
		return fmt.Errorf("%w: state %q is not declared", ErrInvalidGrant, n.State)
	}
	if err := n.Vesting.Validate(); err != nil {
		return fmt.Errorf("%w: vesting: %v", ErrInvalidGrant, err)
	}
	if n.Revision == 1 && (n.ParentDigest != "" || n.SupersedesRevision != 0) {
		return fmt.Errorf("%w: first revision cannot have lineage", ErrInvalidGrant)
	}
	if n.Revision > 1 && (strings.TrimSpace(n.ParentDigest) == "" || n.SupersedesRevision == 0 || n.SupersedesRevision >= n.Revision) {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidGrant)
	}
	if n.State == GrantAccepted || n.State == GrantActive || n.State == GrantSettled {
		if strings.TrimSpace(n.AcceptanceDigest) == "" {
			return fmt.Errorf("%w: acceptance_digest is required for accepted grant states", ErrInvalidGrant)
		}
	}
	if n.State == GrantCancelled || n.State == GrantForfeited {
		if strings.TrimSpace(n.EvidenceRef) == "" {
			return fmt.Errorf("%w: evidence_ref is required for cancellation or forfeiture", ErrInvalidGrant)
		}
	}
	if n.CanonicalDigest != "" && n.CanonicalDigest != n.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidGrant)
	}
	if n.Digest != "" && n.Digest != n.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidGrant)
	}
	return nil
}

func (g EquityGrant) canonicalWithoutDigest() []byte {
	n, err := g.normalized()
	if err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.equity.EquityGrant", schemaVersion).
		String("grant_id", n.GrantID).Int("revision", int64(n.Revision)).String("plan_digest", n.PlanDigest).
		Int("plan_revision", int64(n.PlanRevision)).String("pool_ref", n.PoolRef).String("worker_ref", n.WorkerRef).
		String("instrument", string(n.Instrument)).Value("quantity", n.Quantity).Value("grant_date", n.GrantDate).
		Value("strike_price", n.StrikePrice).String("currency", n.Currency).Value("vesting", n.Vesting).
		String("state", string(n.State)).String("acceptance_digest", n.AcceptanceDigest).String("approval_ref", n.ApprovalRef).
		String("evidence_ref", n.EvidenceRef).String("parent_digest", n.ParentDigest).
		Int("supersedes_revision", int64(n.SupersedesRevision))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (g EquityGrant) computedDigest() string {
	return canonicalbytes.Digest(g.canonicalWithoutDigest())
}
func (g EquityGrant) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}
	return g.canonicalWithoutDigest()
}

func NewEquityGrant(g EquityGrant) (EquityGrant, error) {
	n, err := g.normalized()
	if err != nil {
		return EquityGrant{}, err
	}
	n.InstrumentKind = n.Instrument
	n.Date = n.GrantDate
	n.Price = n.StrikePrice
	n.CanonicalDigest, n.Digest = "", ""
	if err := n.Validate(); err != nil {
		return EquityGrant{}, err
	}
	digest := n.computedDigest()
	n.CanonicalDigest, n.Digest = digest, digest
	return n, nil
}

func NewGrant(g EquityGrant) (EquityGrant, error) { return NewEquityGrant(g) }

func (g EquityGrant) transition(state GrantState, evidence string) (EquityGrant, error) {
	if err := g.Validate(); err != nil {
		return EquityGrant{}, err
	}
	if strings.TrimSpace(evidence) == "" {
		return EquityGrant{}, fmt.Errorf("%w: evidence_ref is required", ErrGrantTransition)
	}
	allowed := (g.State == GrantAccepted && state == GrantActive) ||
		(g.State == GrantActive && (state == GrantCancelled || state == GrantForfeited || state == GrantSettled)) ||
		(g.State == GrantAccepted && (state == GrantCancelled || state == GrantForfeited))
	if !allowed {
		return EquityGrant{}, fmt.Errorf("%w: %s -> %s", ErrGrantTransition, g.State, state)
	}
	n := g
	n.State, n.EvidenceRef = state, evidence
	n.ParentDigest, n.SupersedesRevision, n.Revision = g.computedDigest(), g.Revision, g.Revision+1
	n.CanonicalDigest, n.Digest = "", ""
	return NewEquityGrant(n)
}

// Accept binds an event to this exact grant revision and creates a successor.
func (g EquityGrant) Accept(event AcceptanceEvent) (EquityGrant, error) {
	if err := g.Validate(); err != nil {
		return EquityGrant{}, err
	}
	if g.State != GrantProposed {
		return EquityGrant{}, fmt.Errorf("%w: only proposed grants can be accepted", ErrGrantTransition)
	}
	if err := event.Validate(); err != nil {
		return EquityGrant{}, err
	}
	if event.GrantDigest != g.computedDigest() || event.GrantRevision != g.Revision {
		return EquityGrant{}, fmt.Errorf("%w: acceptance event is not bound to this grant revision", ErrGrantTransition)
	}
	n := g
	n.State, n.AcceptanceDigest, n.EvidenceRef = GrantAccepted, event.computedDigest(), event.EvidenceRef
	n.ParentDigest, n.SupersedesRevision, n.Revision = g.computedDigest(), g.Revision, g.Revision+1
	n.CanonicalDigest, n.Digest = "", ""
	return NewEquityGrant(n)
}

func (g EquityGrant) Activate(evidence string) (EquityGrant, error) {
	return g.transition(GrantActive, evidence)
}
func (g EquityGrant) Cancel(evidence string) (EquityGrant, error) {
	return g.transition(GrantCancelled, evidence)
}
func (g EquityGrant) Forfeit(evidence string) (EquityGrant, error) {
	return g.transition(GrantForfeited, evidence)
}
func (g EquityGrant) Settle(evidence string) (EquityGrant, error) {
	return g.transition(GrantSettled, evidence)
}

type GrantExplanation struct {
	GrantID          string
	PlanDigest       string
	PlanRevision     uint64
	Instrument       InstrumentKind
	GrantDate        values.LocalDate
	VestingDigest    string
	State            GrantState
	Revision         uint64
	AcceptanceDigest string
	Digest           string
}

// Explain deliberately excludes worker identity, quantity, price, and source
// evidence values; callers can use the returned digests to authorize a
// separate, policy-controlled disclosure.
func (g EquityGrant) Explain() (GrantExplanation, error) {
	n, err := g.normalized()
	if err != nil {
		return GrantExplanation{}, err
	}
	if err := n.Validate(); err != nil {
		return GrantExplanation{}, err
	}
	return GrantExplanation{GrantID: n.GrantID, PlanDigest: n.PlanDigest, PlanRevision: n.PlanRevision, Instrument: n.Instrument, GrantDate: n.GrantDate, VestingDigest: canonicalbytes.Digest(n.Vesting.Canonical()), State: n.State, Revision: n.Revision, AcceptanceDigest: n.AcceptanceDigest, Digest: n.computedDigest()}, nil
}

func Explain(g EquityGrant) (GrantExplanation, error) { return g.Explain() }
