// Package mobility owns the pure vocabulary for cross-border mobility.
// Revisions are values: constructors validate and digest them, while
// successor operations leave the earlier revision untouched. Provider calls,
// persistence, payroll calculation, and immigration decisions remain outside
// this package.
package mobility

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's immutable vocabulary version.
func Version() int { return schemaVersion }

var (
	ErrInvalidAssignment         = errors.New("mobility: invalid assignment revision")
	ErrInvalidPlan               = errors.New("mobility: invalid mobility plan")
	ErrInvalidRelocation         = errors.New("mobility: invalid relocation package")
	ErrInvalidImmigration        = errors.New("mobility: invalid immigration milestone")
	ErrInvalidObligation         = errors.New("mobility: invalid mobility obligation")
	ErrInvalidLeg                = errors.New("mobility: invalid mobility leg")
	ErrOverlappingHostAssignment = errors.New("mobility: overlapping host assignments are refused")
	ErrPlanNotFound              = errors.New("mobility: plan not found")
)

// FieldError is a typed validation error. Field is intentionally stable so a
// caller can render a refusal without parsing prose.
type FieldError struct {
	Domain error
	Field  string
	Reason string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Domain, e.Field, e.Reason)
}
func (e *FieldError) Unwrap() error { return e.Domain }

func fieldError(domain error, field, reason string) error {
	return &FieldError{Domain: domain, Field: field, Reason: reason}
}

// AssignmentRole identifies the home and host sides of an assignment.
type AssignmentRole string

const (
	AssignmentHome AssignmentRole = "HOME"
	AssignmentHost AssignmentRole = "HOST"
	Home                          = AssignmentHome
	Host                          = AssignmentHost
)

func (r AssignmentRole) Valid() bool { return r == AssignmentHome || r == AssignmentHost }

// AssignmentType is the closed international-assignment vocabulary from the
// domain model.
type AssignmentType string

const (
	ShortTerm         AssignmentType = "SHORT_TERM"
	LongTerm          AssignmentType = "LONG_TERM"
	Permanent         AssignmentType = "PERMANENT"
	Commuter          AssignmentType = "COMMUTER"
	Rotational        AssignmentType = "ROTATIONAL"
	RemoteCrossBorder AssignmentType = "REMOTE_CROSS_BORDER"
	BusinessTravel    AssignmentType = "BUSINESS_TRAVEL"
)

func (t AssignmentType) Valid() bool {
	switch t {
	case ShortTerm, LongTerm, Permanent, Commuter, Rotational, RemoteCrossBorder, BusinessTravel:
		return true
	default:
		return false
	}
}

// PayrollModel is the closed payroll ownership vocabulary for a mobility
// assignment.
type PayrollModel string

const (
	PayrollHome   PayrollModel = "HOME"
	PayrollHost   PayrollModel = "HOST"
	PayrollSplit  PayrollModel = "SPLIT"
	PayrollShadow PayrollModel = "SHADOW"
	PayrollEOR    PayrollModel = "EOR"
)

func (m PayrollModel) Valid() bool {
	switch m {
	case PayrollHome, PayrollHost, PayrollSplit, PayrollShadow, PayrollEOR:
		return true
	default:
		return false
	}
}

// AssignmentRevision is one immutable home or host assignment snapshot.
// EntityRef, EmploymentRef, LocationRef, and PayrollRef are opaque references;
// their payloads are not interpreted or fetched by this package.
type AssignmentRevision struct {
	AssignmentID        string
	ID                  string
	Revision            uint64
	SupersedesRevision  uint64
	SupersedesDigest    string
	Role                AssignmentRole
	Type                AssignmentType
	EntityRef           string
	Entity              string
	EmploymentRef       string
	LocationRef         string
	Jurisdiction        string
	PayrollRef          string
	PayrollModel        PayrollModel
	Effective           values.EffectiveInterval
	EffectiveWindow     values.EffectiveInterval
	CostAllocationSplit values.Decimal
	AllocationSplit     values.Decimal
	CostAllocation      values.Decimal
	SourceAuthority     string
	KnownAt             values.KnownAt
	CanonicalDigest     string
}

func (a AssignmentRevision) id() string {
	if strings.TrimSpace(a.AssignmentID) != "" {
		return a.AssignmentID
	}
	return a.ID
}
func (a AssignmentRevision) entity() string {
	for _, value := range []string{a.EntityRef, a.Entity} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
func (a AssignmentRevision) effective() values.EffectiveInterval {
	if a.Effective.Validate() == nil {
		return a.Effective
	}
	return a.EffectiveWindow
}
func (a AssignmentRevision) split() values.Decimal {
	for _, value := range []values.Decimal{a.CostAllocationSplit, a.AllocationSplit, a.CostAllocation} {
		if value.Validate() == nil {
			return value
		}
	}
	return values.Decimal{}
}

func (a AssignmentRevision) Validate() error {
	if strings.TrimSpace(a.id()) == "" {
		return fieldError(ErrInvalidAssignment, "assignment_id", "is required")
	}
	if a.Revision == 0 {
		return fieldError(ErrInvalidAssignment, "revision", "must be positive")
	}
	if !a.Role.Valid() {
		return fieldError(ErrInvalidAssignment, "role", "must be HOME or HOST")
	}
	if a.Type != "" && !a.Type.Valid() {
		return fieldError(ErrInvalidAssignment, "type", "is not declared")
	}
	for field, value := range map[string]string{"entity_ref": a.entity(), "employment_ref": a.EmploymentRef, "location_ref": a.LocationRef, "jurisdiction": a.Jurisdiction, "payroll_ref": a.PayrollRef, "source_authority": a.SourceAuthority} {
		if strings.TrimSpace(value) == "" {
			return fieldError(ErrInvalidAssignment, field, "is required")
		}
	}
	if a.PayrollModel != "" && !a.PayrollModel.Valid() {
		return fieldError(ErrInvalidAssignment, "payroll_model", "is not declared")
	}
	if err := a.effective().Validate(); err != nil {
		return fieldError(ErrInvalidAssignment, "effective", err.Error())
	}
	split := a.split()
	if err := split.Validate(); err != nil {
		return fieldError(ErrInvalidAssignment, "cost_allocation_split", "must be an exact decimal")
	}
	if split.Rounding() != values.RoundingExactRequired {
		return fieldError(ErrInvalidAssignment, "cost_allocation_split", "rounding mode must be EXACT_REQUIRED")
	}
	zero := values.MustDecimal("0", split.Scale(), values.RoundingExactRequired)
	one := values.MustDecimal("1", split.Scale(), values.RoundingExactRequired)
	if split.Cmp(zero) < 0 || split.Cmp(one) > 0 {
		return fieldError(ErrInvalidAssignment, "cost_allocation_split", "must be between 0 and 1")
	}
	if a.KnownAt.Canonical() != nil { /* optional for planned assignments */
	}
	if a.SupersedesRevision >= a.Revision && a.SupersedesRevision != 0 {
		return fieldError(ErrInvalidAssignment, "supersedes_revision", "must precede revision")
	}
	if a.Revision > 1 && (a.SupersedesRevision == 0 || strings.TrimSpace(a.SupersedesDigest) == "") {
		return fieldError(ErrInvalidAssignment, "lineage", "successor requires parent revision and digest")
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != a.computedDigest() {
		return fieldError(ErrInvalidAssignment, "canonical_digest", "does not match assignment bytes")
	}
	return nil
}

func (a AssignmentRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.mobility.AssignmentRevision", schemaVersion).
		String("assignment_id", a.id()).Int("revision", int64(a.Revision)).Int("supersedes_revision", int64(a.SupersedesRevision)).
		String("supersedes_digest", a.SupersedesDigest).String("role", string(a.Role)).String("type", string(a.Type)).
		String("entity_ref", a.entity()).String("employment_ref", a.EmploymentRef).String("location_ref", a.LocationRef).
		String("jurisdiction", a.Jurisdiction).String("payroll_ref", a.PayrollRef).String("payroll_model", string(a.PayrollModel)).
		Value("effective", a.effective()).Value("cost_allocation_split", a.split()).String("source_authority", a.SourceAuthority)
	if a.KnownAt.Canonical() != nil {
		w.Value("known_at", a.KnownAt)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (a AssignmentRevision) computedDigest() string { return canonicalbytes.Digest(a.body()) }
func (a AssignmentRevision) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	return a.body()
}
func (a AssignmentRevision) Digest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a.computedDigest(), nil
}

// NewAssignmentRevision validates and computes the identity of a revision.
func NewAssignmentRevision(a AssignmentRevision) (AssignmentRevision, error) {
	a.CanonicalDigest = a.computedDigest()
	if err := a.Validate(); err != nil {
		return AssignmentRevision{}, err
	}
	return a, nil
}
func NewHomeAssignmentRevision(a AssignmentRevision) (AssignmentRevision, error) {
	a.Role = AssignmentHome
	return NewAssignmentRevision(a)
}
func NewHostAssignmentRevision(a AssignmentRevision) (AssignmentRevision, error) {
	a.Role = AssignmentHost
	return NewAssignmentRevision(a)
}

// Successor creates the next assignment revision without mutating the parent.
func (a AssignmentRevision) Successor(next AssignmentRevision) (AssignmentRevision, error) {
	if err := a.Validate(); err != nil {
		return AssignmentRevision{}, err
	}
	next.AssignmentID, next.ID = a.id(), a.id()
	next.Revision, next.SupersedesRevision, next.SupersedesDigest = a.Revision+1, a.Revision, a.CanonicalDigest
	if next.Role == "" {
		next.Role = a.Role
	}
	return NewAssignmentRevision(next)
}

// MobilityLeg records a dated origin/destination work or travel leg.
type MobilityLeg struct {
	LegID                   string
	OriginJurisdiction      string
	DestinationJurisdiction string
	Effective               values.EffectiveInterval
	Purpose                 string
	WorkPresence            bool
	SourceRef               string
}

func (l MobilityLeg) Validate() error {
	if strings.TrimSpace(l.LegID) == "" {
		return fieldError(ErrInvalidLeg, "leg_id", "is required")
	}
	for field, value := range map[string]string{"origin_jurisdiction": l.OriginJurisdiction, "destination_jurisdiction": l.DestinationJurisdiction, "purpose": l.Purpose, "source_ref": l.SourceRef} {
		if strings.TrimSpace(value) == "" {
			return fieldError(ErrInvalidLeg, field, "is required")
		}
	}
	if err := l.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidLeg, "effective", err.Error())
	}
	return nil
}
func (l MobilityLeg) Canonical() []byte {
	if l.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.mobility.MobilityLeg", schemaVersion).String("leg_id", l.LegID).String("origin", l.OriginJurisdiction).String("destination", l.DestinationJurisdiction).Value("effective", l.Effective).String("purpose", l.Purpose).Bool("work_presence", l.WorkPresence).String("source_ref", l.SourceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// RelocationMilestoneKind is a closed set; arbitrary provider states cannot
// become domain milestones.
type RelocationMilestoneKind string

const (
	RelocationDeparture  RelocationMilestoneKind = "DEPARTURE"
	RelocationTravel     RelocationMilestoneKind = "TRAVEL"
	RelocationArrival    RelocationMilestoneKind = "ARRIVAL"
	RelocationHousing    RelocationMilestoneKind = "HOUSING"
	RelocationShipment   RelocationMilestoneKind = "SHIPMENT"
	RelocationSettlingIn RelocationMilestoneKind = "SETTLING_IN"
	MilestoneDeparture                           = RelocationDeparture
	MilestoneTravel                              = RelocationTravel
	MilestoneArrival                             = RelocationArrival
	MilestoneHousing                             = RelocationHousing
	MilestoneShipment                            = RelocationShipment
	MilestoneSettlingIn                          = RelocationSettlingIn
)

func (k RelocationMilestoneKind) Valid() bool {
	switch k {
	case RelocationDeparture, RelocationTravel, RelocationArrival, RelocationHousing, RelocationShipment, RelocationSettlingIn:
		return true
	}
	return false
}

type RelocationMilestone struct {
	MilestoneID string
	Kind        RelocationMilestoneKind
	Effective   values.EffectiveInterval
	OwnerRef    string
	EvidenceRef string
}

func (m RelocationMilestone) Validate() error {
	if strings.TrimSpace(m.MilestoneID) == "" {
		return fieldError(ErrInvalidRelocation, "milestone_id", "is required")
	}
	if !m.Kind.Valid() {
		return fieldError(ErrInvalidRelocation, "kind", "is not declared")
	}
	if err := m.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidRelocation, "effective", err.Error())
	}
	if strings.TrimSpace(m.OwnerRef) == "" {
		return fieldError(ErrInvalidRelocation, "owner_ref", "is required")
	}
	if strings.TrimSpace(m.EvidenceRef) == "" {
		return fieldError(ErrInvalidRelocation, "evidence_ref", "is required")
	}
	return nil
}
func (m RelocationMilestone) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.mobility.RelocationMilestone", schemaVersion).String("milestone_id", m.MilestoneID).String("kind", string(m.Kind)).Value("effective", m.Effective).String("owner_ref", m.OwnerRef).String("evidence_ref", m.EvidenceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type RelocationPackage struct {
	PackageID       string
	MobilityID      string
	WorkerRef       string
	OwnerRef        string
	Milestones      []RelocationMilestone
	CanonicalDigest string
}

func (p RelocationPackage) Validate() error {
	if strings.TrimSpace(p.PackageID) == "" {
		return fieldError(ErrInvalidRelocation, "package_id", "is required")
	}
	for field, value := range map[string]string{"mobility_id": p.MobilityID, "worker_ref": p.WorkerRef, "owner_ref": p.OwnerRef} {
		if strings.TrimSpace(value) == "" {
			return fieldError(ErrInvalidRelocation, field, "is required")
		}
	}
	if len(p.Milestones) == 0 {
		return fieldError(ErrInvalidRelocation, "milestones", "at least one is required")
	}
	seen := map[string]bool{}
	for _, m := range p.Milestones {
		if err := m.Validate(); err != nil {
			return err
		}
		if seen[m.MilestoneID] {
			return fieldError(ErrInvalidRelocation, "milestone_id", "is duplicated")
		}
		seen[m.MilestoneID] = true
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fieldError(ErrInvalidRelocation, "canonical_digest", "does not match package bytes")
	}
	return nil
}
func (p RelocationPackage) body() []byte {
	ms := append([]RelocationMilestone(nil), p.Milestones...)
	sort.Slice(ms, func(i, j int) bool { return ms[i].MilestoneID < ms[j].MilestoneID })
	w := canonicalbytes.New("hcmnext.domains.mobility.RelocationPackage", schemaVersion).String("package_id", p.PackageID).String("mobility_id", p.MobilityID).String("worker_ref", p.WorkerRef).String("owner_ref", p.OwnerRef).Count("milestones", len(ms))
	for _, m := range ms {
		w.Value("milestone", m)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (p RelocationPackage) computedDigest() string { return canonicalbytes.Digest(p.body()) }
func (p RelocationPackage) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}
func NewRelocationPackage(p RelocationPackage) (RelocationPackage, error) {
	p.Milestones = append([]RelocationMilestone(nil), p.Milestones...)
	p.CanonicalDigest = p.computedDigest()
	if err := p.Validate(); err != nil {
		return RelocationPackage{}, err
	}
	return p, nil
}

// ImmigrationMilestoneKind is deliberately narrower than provider status.
type ImmigrationMilestoneKind string

const (
	ImmigrationPetition       ImmigrationMilestoneKind = "PETITION"
	ImmigrationApproval       ImmigrationMilestoneKind = "APPROVAL"
	ImmigrationExpiry         ImmigrationMilestoneKind = "EXPIRY"
	ImmigrationReverification ImmigrationMilestoneKind = "RE_VERIFICATION"
	Petition                                           = ImmigrationPetition
	Approval                                           = ImmigrationApproval
	Expiry                                             = ImmigrationExpiry
	ReVerification                                     = ImmigrationReverification
)

func (k ImmigrationMilestoneKind) Valid() bool {
	switch k {
	case ImmigrationPetition, ImmigrationApproval, ImmigrationExpiry, ImmigrationReverification:
		return true
	}
	return false
}

// DisclosureScope binds disclosure to the immigration jurisdiction; a broad
// read cannot turn a protected milestone into an ordinary employee field.
type DisclosureScope struct {
	Jurisdiction string
	Allowed      bool
	Reason       string
}

func (d DisclosureScope) Validate(forJurisdiction string) error {
	if strings.TrimSpace(d.Jurisdiction) == "" {
		return fieldError(ErrInvalidImmigration, "disclosure.jurisdiction", "is required")
	}
	if d.Jurisdiction != forJurisdiction {
		return fieldError(ErrInvalidImmigration, "disclosure.jurisdiction", "must equal milestone jurisdiction")
	}
	if !d.Allowed && strings.TrimSpace(d.Reason) == "" {
		return fieldError(ErrInvalidImmigration, "disclosure.reason", "is required when disclosure is refused")
	}
	return nil
}

type ImmigrationMilestone struct {
	MilestoneID     string
	ProcessRef      string
	Kind            ImmigrationMilestoneKind
	Jurisdiction    string
	At              values.LocalDate
	EffectiveOn     values.LocalDate
	SourceRef       string
	EvidenceRef     string
	ExpiresOn       values.LocalDate
	ExpiryDate      values.LocalDate
	Disclosure      DisclosureScope
	CanonicalDigest string
}

func (m ImmigrationMilestone) at() values.LocalDate {
	if m.At.IsSet() {
		return m.At
	}
	return m.EffectiveOn
}
func (m ImmigrationMilestone) expiry() values.LocalDate {
	if m.ExpiresOn.IsSet() {
		return m.ExpiresOn
	}
	return m.ExpiryDate
}
func (m ImmigrationMilestone) Validate() error {
	if strings.TrimSpace(m.MilestoneID) == "" {
		return fieldError(ErrInvalidImmigration, "milestone_id", "is required")
	}
	if strings.TrimSpace(m.ProcessRef) == "" {
		return fieldError(ErrInvalidImmigration, "process_ref", "is required")
	}
	if !m.Kind.Valid() {
		return fieldError(ErrInvalidImmigration, "kind", "is not declared")
	}
	if strings.TrimSpace(m.Jurisdiction) == "" {
		return fieldError(ErrInvalidImmigration, "jurisdiction", "is required")
	}
	if err := m.at().Validate(); err != nil {
		return fieldError(ErrInvalidImmigration, "at", err.Error())
	}
	if strings.TrimSpace(m.SourceRef) == "" && strings.TrimSpace(m.EvidenceRef) == "" {
		return fieldError(ErrInvalidImmigration, "source_ref", "source or evidence reference is required")
	}
	if err := m.Disclosure.Validate(m.Jurisdiction); err != nil {
		return err
	}
	if e := m.expiry(); e.IsSet() {
		if err := e.Validate(); err != nil {
			return fieldError(ErrInvalidImmigration, "expiry_date", err.Error())
		}
		if e.Compare(m.at()) < 0 {
			return fieldError(ErrInvalidImmigration, "expiry_date", "must not precede milestone date")
		}
	}
	if m.CanonicalDigest != "" && m.CanonicalDigest != m.computedDigest() {
		return fieldError(ErrInvalidImmigration, "canonical_digest", "does not match milestone bytes")
	}
	return nil
}
func (m ImmigrationMilestone) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.mobility.ImmigrationMilestone", schemaVersion).String("milestone_id", m.MilestoneID).String("process_ref", m.ProcessRef).String("kind", string(m.Kind)).String("jurisdiction", m.Jurisdiction).Value("at", m.at()).String("source_ref", m.SourceRef).String("evidence_ref", m.EvidenceRef).Bool("expiry_present", m.expiry().IsSet())
	if m.expiry().IsSet() {
		w.Value("expiry", m.expiry())
	}
	w.String("disclosure_jurisdiction", m.Disclosure.Jurisdiction).Bool("disclosure_allowed", m.Disclosure.Allowed).String("disclosure_reason", m.Disclosure.Reason)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (m ImmigrationMilestone) computedDigest() string { return canonicalbytes.Digest(m.body()) }
func (m ImmigrationMilestone) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	return m.body()
}
func NewImmigrationMilestone(m ImmigrationMilestone) (ImmigrationMilestone, error) {
	m.CanonicalDigest = m.computedDigest()
	if err := m.Validate(); err != nil {
		return ImmigrationMilestone{}, err
	}
	return m, nil
}

type ObligationKind string

const (
	ObligationPayroll ObligationKind = "PAYROLL"
	ObligationTax     ObligationKind = "TAX"
	ObligationPE      ObligationKind = "PERMANENT_ESTABLISHMENT"
	ObligationPrivacy ObligationKind = "PRIVACY"
)

func (k ObligationKind) Valid() bool {
	switch k {
	case ObligationPayroll, ObligationTax, ObligationPE, ObligationPrivacy:
		return true
	}
	return false
}

type ObligationStatus string

const (
	ObligationReady       ObligationStatus = "READY"
	ObligationConditional ObligationStatus = "CONDITIONAL"
	ObligationBlocked     ObligationStatus = "BLOCKED"
	ObligationUnknown     ObligationStatus = "UNKNOWN"
)

func (s ObligationStatus) Valid() bool {
	return s == ObligationReady || s == ObligationConditional || s == ObligationBlocked || s == ObligationUnknown
}

type MobilityObligation struct {
	Kind        ObligationKind
	Ref         string
	Status      ObligationStatus
	OwnerRef    string
	EvidenceRef string
}

func (o MobilityObligation) Validate() error {
	if !o.Kind.Valid() {
		return fieldError(ErrInvalidObligation, "kind", "is not declared")
	}
	if strings.TrimSpace(o.Ref) == "" {
		return fieldError(ErrInvalidObligation, "ref", "is required")
	}
	if !o.Status.Valid() {
		return fieldError(ErrInvalidObligation, "status", "is not declared")
	}
	if strings.TrimSpace(o.OwnerRef) == "" {
		return fieldError(ErrInvalidObligation, "owner_ref", "is required")
	}
	if o.Status == ObligationReady && strings.TrimSpace(o.EvidenceRef) == "" {
		return fieldError(ErrInvalidObligation, "evidence_ref", "is required for READY")
	}
	return nil
}
func (o MobilityObligation) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.mobility.MobilityObligation", schemaVersion).String("kind", string(o.Kind)).String("ref", o.Ref).String("status", string(o.Status)).String("owner_ref", o.OwnerRef).String("evidence_ref", o.EvidenceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type MobilityStatus string

const (
	StatusReady       MobilityStatus = "READY"
	StatusConditional MobilityStatus = "CONDITIONAL"
	StatusBlocked     MobilityStatus = "BLOCKED"
	StatusUnknown     MobilityStatus = "UNKNOWN"
	Ready                            = StatusReady
	Conditional                      = StatusConditional
	Blocked                          = StatusBlocked
	Unknown                          = StatusUnknown
)

func (s MobilityStatus) Valid() bool {
	return s == StatusReady || s == StatusConditional || s == StatusBlocked || s == StatusUnknown
}

// MobilityPlan is the immutable aggregate that binds all facts needed to
// assess a cross-border move. It is descriptive and never executes effects.
type MobilityPlan struct {
	PlanID          string
	MobilityID      string
	Revision        uint64
	ParentRevision  uint64
	ParentDigest    string
	WorkerRef       string
	Home            AssignmentRevision
	Host            AssignmentRevision
	HomeAssignment  AssignmentRevision
	HostAssignment  AssignmentRevision
	HostAssignments []AssignmentRevision
	Legs            []MobilityLeg
	Relocation      RelocationPackage
	Immigration     []ImmigrationMilestone
	Obligations     []MobilityObligation
	Status          MobilityStatus
	CanonicalDigest string
}

func (p MobilityPlan) id() string {
	if strings.TrimSpace(p.MobilityID) != "" {
		return p.MobilityID
	}
	return p.PlanID
}
func (p MobilityPlan) home() AssignmentRevision {
	if p.HomeAssignment.id() != "" {
		return p.HomeAssignment
	}
	return p.Home
}
func (p MobilityPlan) host() AssignmentRevision {
	if p.HostAssignment.id() != "" {
		return p.HostAssignment
	}
	return p.Host
}
func (p MobilityPlan) hosts() []AssignmentRevision {
	out := append([]AssignmentRevision(nil), p.HostAssignments...)
	if p.host().id() != "" {
		out = append(out, p.host())
	}
	return out
}
func (p MobilityPlan) Validate() error {
	if strings.TrimSpace(p.id()) == "" {
		return fieldError(ErrInvalidPlan, "mobility_id", "is required")
	}
	if p.Revision == 0 {
		return fieldError(ErrInvalidPlan, "revision", "must be positive")
	}
	if p.Revision > 1 && (p.ParentRevision == 0 || p.ParentRevision >= p.Revision || strings.TrimSpace(p.ParentDigest) == "") {
		return fieldError(ErrInvalidPlan, "lineage", "successor requires parent revision and digest")
	}
	if strings.TrimSpace(p.WorkerRef) == "" {
		return fieldError(ErrInvalidPlan, "worker_ref", "is required")
	}
	h := p.home()
	if err := h.Validate(); err != nil {
		return fieldError(ErrInvalidPlan, "home_assignment", err.Error())
	}
	if h.Role != AssignmentHome {
		return fieldError(ErrInvalidPlan, "home_assignment.role", "must be HOME")
	}
	hosts := p.hosts()
	if len(hosts) == 0 {
		return fieldError(ErrInvalidPlan, "host_assignments", "at least one is required")
	}
	for i, a := range hosts {
		if err := a.Validate(); err != nil {
			return fieldError(ErrInvalidPlan, fmt.Sprintf("host_assignments[%d]", i), err.Error())
		}
		if a.Role != AssignmentHost {
			return fieldError(ErrInvalidPlan, fmt.Sprintf("host_assignments[%d].role", i), "must be HOST")
		}
	}
	for i := 0; i < len(hosts); i++ {
		for j := i + 1; j < len(hosts); j++ {
			overlap, err := hosts[i].effective().Overlaps(hosts[j].effective())
			if err != nil {
				return fieldError(ErrInvalidPlan, "host_assignments", err.Error())
			}
			if overlap {
				return &FieldError{Domain: ErrOverlappingHostAssignment, Field: "host_assignments", Reason: hosts[i].id() + " overlaps " + hosts[j].id()}
			}
		}
	}
	if len(p.Legs) == 0 {
		return fieldError(ErrInvalidPlan, "legs", "at least one is required")
	}
	for i, l := range p.Legs {
		if err := l.Validate(); err != nil {
			return fieldError(ErrInvalidPlan, fmt.Sprintf("legs[%d]", i), err.Error())
		}
	}
	if err := p.Relocation.Validate(); err != nil {
		return fieldError(ErrInvalidPlan, "relocation", err.Error())
	}
	if p.Relocation.MobilityID != p.id() {
		return fieldError(ErrInvalidPlan, "relocation.mobility_id", "must bind the plan")
	}
	if p.Relocation.WorkerRef != p.WorkerRef {
		return fieldError(ErrInvalidPlan, "relocation.worker_ref", "must bind the plan")
	}
	if len(p.Immigration) < 4 {
		return fieldError(ErrInvalidPlan, "immigration", "petition, approval, expiry, and re-verification are required")
	}
	kinds := map[ImmigrationMilestoneKind]bool{}
	for i, m := range p.Immigration {
		if err := m.Validate(); err != nil {
			return fieldError(ErrInvalidPlan, fmt.Sprintf("immigration[%d]", i), err.Error())
		}
		kinds[m.Kind] = true
	}
	for _, k := range []ImmigrationMilestoneKind{ImmigrationPetition, ImmigrationApproval, ImmigrationExpiry, ImmigrationReverification} {
		if !kinds[k] {
			return fieldError(ErrInvalidPlan, "immigration", string(k)+" milestone is required")
		}
	}
	if len(p.Obligations) == 0 {
		return fieldError(ErrInvalidPlan, "obligations", "at least one is required")
	}
	seen := map[ObligationKind]bool{}
	for i, o := range p.Obligations {
		if err := o.Validate(); err != nil {
			return fieldError(ErrInvalidPlan, fmt.Sprintf("obligations[%d]", i), err.Error())
		}
		if seen[o.Kind] {
			return fieldError(ErrInvalidPlan, "obligations.kind", "each obligation kind may appear once")
		}
		seen[o.Kind] = true
	}
	for _, k := range []ObligationKind{ObligationPayroll, ObligationTax, ObligationPE, ObligationPrivacy} {
		if !seen[k] {
			return fieldError(ErrInvalidPlan, "obligations", string(k)+" is required")
		}
	}
	computed := p.assess()
	if p.Status != "" && !p.Status.Valid() {
		return fieldError(ErrInvalidPlan, "status", "is not declared")
	}
	if p.Status != "" && p.Status != computed {
		return fieldError(ErrInvalidPlan, "status", "does not match obligation assessment")
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fieldError(ErrInvalidPlan, "canonical_digest", "does not match plan bytes")
	}
	return nil
}
func (p MobilityPlan) assess() MobilityStatus {
	conditional := false
	unknown := false
	for _, o := range p.Obligations {
		switch o.Status {
		case ObligationBlocked:
			return StatusBlocked
		case ObligationUnknown:
			unknown = true
		case ObligationConditional:
			conditional = true
		}
	}
	if unknown {
		return StatusUnknown
	}
	if conditional {
		return StatusConditional
	}
	return StatusReady
}
func (p MobilityPlan) body() []byte {
	hosts := p.hosts()
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].id() < hosts[j].id() })
	legs := append([]MobilityLeg(nil), p.Legs...)
	sort.Slice(legs, func(i, j int) bool { return legs[i].LegID < legs[j].LegID })
	ims := append([]ImmigrationMilestone(nil), p.Immigration...)
	sort.Slice(ims, func(i, j int) bool { return ims[i].MilestoneID < ims[j].MilestoneID })
	obs := append([]MobilityObligation(nil), p.Obligations...)
	sort.Slice(obs, func(i, j int) bool { return obs[i].Kind < obs[j].Kind })
	w := canonicalbytes.New("hcmnext.domains.mobility.MobilityPlan", schemaVersion).String("mobility_id", p.id()).Int("revision", int64(p.Revision)).Int("parent_revision", int64(p.ParentRevision)).String("parent_digest", p.ParentDigest).String("worker_ref", p.WorkerRef).Value("home", p.home()).Count("hosts", len(hosts))
	for _, a := range hosts {
		w.Value("host", a)
	}
	w.Count("legs", len(legs))
	for _, l := range legs {
		w.Value("leg", l)
	}
	w.Value("relocation", p.Relocation).Count("immigration", len(ims))
	for _, m := range ims {
		w.Value("immigration", m)
	}
	w.Count("obligations", len(obs))
	for _, o := range obs {
		w.Value("obligation", o)
	}
	w.String("status", string(p.assess()))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (p MobilityPlan) computedDigest() string { return canonicalbytes.Digest(p.body()) }
func (p MobilityPlan) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}
func (p MobilityPlan) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}
func NewMobilityPlan(p MobilityPlan) (MobilityPlan, error) {
	p.HostAssignments = append([]AssignmentRevision(nil), p.HostAssignments...)
	p.Legs = append([]MobilityLeg(nil), p.Legs...)
	p.Immigration = append([]ImmigrationMilestone(nil), p.Immigration...)
	p.Obligations = append([]MobilityObligation(nil), p.Obligations...)
	p.Status = p.assess()
	p.CanonicalDigest = p.computedDigest()
	if err := p.Validate(); err != nil {
		return MobilityPlan{}, err
	}
	return p, nil
}
func NewPlan(p MobilityPlan) (MobilityPlan, error) { return NewMobilityPlan(p) }
func (p MobilityPlan) Assess() (MobilityStatus, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.assess(), nil
}
func (p MobilityPlan) StatusValue() (MobilityStatus, error) { return p.Assess() }
func (p MobilityPlan) Successor(next MobilityPlan) (MobilityPlan, error) {
	if err := p.Validate(); err != nil {
		return MobilityPlan{}, err
	}
	next.PlanID, next.MobilityID = p.PlanID, p.id()
	next.Revision, next.ParentRevision, next.ParentDigest = p.Revision+1, p.Revision, p.CanonicalDigest
	next.WorkerRef = p.WorkerRef
	return NewMobilityPlan(next)
}

// PlanExplanation contains counts and control state only. It deliberately
// excludes worker, source, evidence, assignment, and immigration payloads.
type PlanExplanation struct {
	Revision                  uint64
	Status                    MobilityStatus
	HostAssignmentCount       int
	LegCount                  int
	RelocationMilestoneCount  int
	ImmigrationMilestoneCount int
	ObligationCount           int
	Digest                    string
}

func (p MobilityPlan) Explain() (PlanExplanation, error) {
	if err := p.Validate(); err != nil {
		return PlanExplanation{}, err
	}
	return PlanExplanation{Revision: p.Revision, Status: p.assess(), HostAssignmentCount: len(p.hosts()), LegCount: len(p.Legs), RelocationMilestoneCount: len(p.Relocation.Milestones), ImmigrationMilestoneCount: len(p.Immigration), ObligationCount: len(p.Obligations), Digest: p.CanonicalDigest}, nil
}
func Explain(p MobilityPlan) (PlanExplanation, error) { return p.Explain() }

// PlanStore is an in-memory port for pure tests and adapters. It has no
// persistence semantics beyond the lifetime of the value.
type PlanStore interface {
	Put(context.Context, MobilityPlan) error
	Get(context.Context, string, uint64) (MobilityPlan, error)
}
type InMemoryPlanStore struct {
	mu    sync.RWMutex
	plans map[string]MobilityPlan
	// ext holds the immigration milestone state persistence.go keeps per
	// store; it lives on the store itself so no package-level registry
	// outlives its owner.
	ext     *tenantMemoryState
	extOnce sync.Once
}

func NewInMemoryPlanStore() *InMemoryPlanStore {
	return &InMemoryPlanStore{plans: make(map[string]MobilityPlan)}
}
func (s *InMemoryPlanStore) Put(ctx context.Context, p MobilityPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans == nil {
		s.plans = make(map[string]MobilityPlan)
	}
	s.plans[p.id()+fmt.Sprintf("\x00%d", p.Revision)] = p
	return nil
}
func (s *InMemoryPlanStore) Get(ctx context.Context, id string, revision uint64) (MobilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return MobilityPlan{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.plans[id+fmt.Sprintf("\x00%d", revision)]
	if !ok {
		return MobilityPlan{}, ErrPlanNotFound
	}
	p.HostAssignments = append([]AssignmentRevision(nil), p.HostAssignments...)
	p.Legs = append([]MobilityLeg(nil), p.Legs...)
	p.Immigration = append([]ImmigrationMilestone(nil), p.Immigration...)
	p.Obligations = append([]MobilityObligation(nil), p.Obligations...)
	return p, nil
}
