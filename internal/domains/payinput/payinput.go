// Package payinput owns the pure payroll-input vocabulary for earning and
// deduction definitions and their worker assignments. Definitions and
// assignments are immutable, effective-dated records; persistence and payroll
// calculation remain outside this package.
package payinput

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidDefinition       = errors.New("payinput: invalid definition")
	ErrInvalidAssignment       = errors.New("payinput: invalid assignment")
	ErrUnknownTaxability       = errors.New("payinput: unknown taxability jurisdiction class")
	ErrUnknownRecurrence       = errors.New("payinput: unknown recurrence")
	ErrDefinitionRetired       = errors.New("payinput: assignment cannot use a retired definition")
	ErrOverlappingAssignments  = errors.New("payinput: worker assignments overlap")
	ErrDefinitionLineage       = errors.New("payinput: invalid definition lineage")
	ErrAssignmentDefinitionRef = errors.New("payinput: assignment definition reference mismatch")
)

// DefinitionKind is the closed earning/deduction vocabulary.
type DefinitionKind string

const (
	KindEarning   DefinitionKind = "EARNING"
	KindDeduction DefinitionKind = "DEDUCTION"
	Earning                      = KindEarning
	Deduction                    = KindDeduction
)

func (k DefinitionKind) Valid() bool { return k == KindEarning || k == KindDeduction }

// JurisdictionClass is the closed vocabulary for taxability flags.
type JurisdictionClass string

const (
	JurisdictionFederal  JurisdictionClass = "FEDERAL"
	JurisdictionState    JurisdictionClass = "STATE"
	JurisdictionLocal    JurisdictionClass = "LOCAL"
	JurisdictionCountry  JurisdictionClass = "COUNTRY"
	JurisdictionProvince JurisdictionClass = "PROVINCE"
	Federal                                = JurisdictionFederal
	State                                  = JurisdictionState
	Local                                  = JurisdictionLocal
	Country                                = JurisdictionCountry
	Province                               = JurisdictionProvince
)

func (j JurisdictionClass) Valid() bool {
	switch j {
	case JurisdictionFederal, JurisdictionState, JurisdictionLocal, JurisdictionCountry, JurisdictionProvince:
		return true
	default:
		return false
	}
}

// TaxabilityFlag records whether one jurisdiction class treats the component
// as taxable. It contains no jurisdiction-specific employee values.
type TaxabilityFlag struct {
	Jurisdiction JurisdictionClass
	Taxable      bool
}

func (f TaxabilityFlag) Validate() error {
	if !f.Jurisdiction.Valid() {
		return fieldError(ErrInvalidDefinition, "taxability.jurisdiction", ErrUnknownTaxability)
	}
	return nil
}

func (f TaxabilityFlag) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.payinput.TaxabilityFlag", schemaVersion).
		String("jurisdiction", string(f.Jurisdiction)).Bool("taxable", f.Taxable).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CalculationBasis is the closed vocabulary for how an input is expressed.
type CalculationBasis string

const (
	BasisHourly     CalculationBasis = "HOURLY"
	BasisSalary     CalculationBasis = "SALARY"
	BasisFlatAmount CalculationBasis = "FLAT_AMOUNT"
	BasisPercentage CalculationBasis = "PERCENTAGE"
	BasisFormula    CalculationBasis = "FORMULA"
	Hourly                           = BasisHourly
	Salary                           = BasisSalary
	FlatAmount                       = BasisFlatAmount
	Percentage                       = BasisPercentage
	Formula                          = BasisFormula
)

func (b CalculationBasis) Valid() bool {
	switch b {
	case BasisHourly, BasisSalary, BasisFlatAmount, BasisPercentage, BasisFormula:
		return true
	default:
		return false
	}
}

// DefinitionState is the immutable lifecycle of a definition revision.
type DefinitionState string

const (
	StateDraft      DefinitionState = "DRAFT"
	StatePublished  DefinitionState = "PUBLISHED"
	StateRetired    DefinitionState = "RETIRED"
	StateSuperseded DefinitionState = "SUPERSEDED"
	Draft                           = StateDraft
	Published                       = StatePublished
	Retired                         = StateRetired
	Superseded                      = StateSuperseded
)

func (s DefinitionState) Valid() bool {
	switch s {
	case StateDraft, StatePublished, StateRetired, StateSuperseded:
		return true
	default:
		return false
	}
}

// DecimalLimits bounds an input without using floating point.
type DecimalLimits struct {
	Minimum values.Decimal
	Maximum values.Decimal
}

type Limits = DecimalLimits

func (l DecimalLimits) Validate() error {
	if l.Minimum.Validate() == nil && l.Maximum.Validate() == nil && l.Minimum.Cmp(l.Maximum) > 0 {
		return fieldError(ErrInvalidDefinition, "limits", errors.New("minimum exceeds maximum"))
	}
	if l.Minimum.Validate() != nil && l.Maximum.Validate() != nil {
		return nil
	}
	if l.Minimum.Validate() != nil && l.Maximum.Validate() == nil || l.Minimum.Validate() == nil && l.Maximum.Validate() != nil {
		return nil
	}
	return nil
}

func (l DecimalLimits) Canonical() []byte {
	if l.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.payinput.DecimalLimits", schemaVersion)
	minSet, maxSet := l.Minimum.Validate() == nil, l.Maximum.Validate() == nil
	w.Optional("minimum", minSet, l.Minimum).Optional("maximum", maxSet, l.Maximum)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Definition is one immutable, effective-dated earning or deduction
// definition revision.
type Definition struct {
	ID                       string
	DefinitionID             string
	Code                     string
	Kind                     DefinitionKind
	Currency                 string
	Taxability               map[JurisdictionClass]bool
	TaxabilityByJurisdiction map[JurisdictionClass]bool
	TaxabilityFlags          []TaxabilityFlag
	CalculationBasis         CalculationBasis
	Basis                    CalculationBasis
	Limits                   DecimalLimits
	Minimum                  values.Decimal
	Maximum                  values.Decimal
	FormulaRef               string
	AccountingCode           string
	AccountingComponentCode  string
	OwnerRef                 string
	Type                     DefinitionKind
	Effective                values.EffectiveInterval
	Version                  string
	Revision                 uint64
	State                    DefinitionState
	Status                   DefinitionState
	SupersedesDigest         string
	SupersedesRevision       uint64
	CanonicalDigest          string
}

func (d Definition) id() string {
	if d.DefinitionID != "" {
		return d.DefinitionID
	}
	return d.ID
}

func (d Definition) basis() CalculationBasis {
	if d.CalculationBasis != "" {
		return d.CalculationBasis
	}
	return d.Basis
}

func (d Definition) kind() DefinitionKind {
	if d.Kind != "" {
		return d.Kind
	}
	return d.Type
}

func (d Definition) accountingCode() string {
	if d.AccountingCode != "" {
		return d.AccountingCode
	}
	return d.AccountingComponentCode
}

func (d Definition) state() DefinitionState {
	if d.State != "" {
		return d.State
	}
	return d.Status
}

func (d Definition) limits() DecimalLimits {
	l := d.Limits
	if d.Minimum.Validate() == nil && l.Minimum.Validate() != nil {
		l.Minimum = d.Minimum
	}
	if d.Maximum.Validate() == nil && l.Maximum.Validate() != nil {
		l.Maximum = d.Maximum
	}
	return l
}

func (d Definition) taxability() ([]TaxabilityFlag, error) {
	flags := append([]TaxabilityFlag(nil), d.TaxabilityFlags...)
	for jurisdiction, taxable := range d.Taxability {
		flags = append(flags, TaxabilityFlag{Jurisdiction: jurisdiction, Taxable: taxable})
	}
	for jurisdiction, taxable := range d.TaxabilityByJurisdiction {
		flags = append(flags, TaxabilityFlag{Jurisdiction: jurisdiction, Taxable: taxable})
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Jurisdiction < flags[j].Jurisdiction })
	seen := make(map[JurisdictionClass]bool, len(flags))
	for _, flag := range flags {
		if err := flag.Validate(); err != nil {
			return nil, err
		}
		if previous, ok := seen[flag.Jurisdiction]; ok && previous != flag.Taxable {
			return nil, fieldError(ErrInvalidDefinition, "taxability", errors.New("duplicate jurisdiction flags disagree"))
		}
		seen[flag.Jurisdiction] = flag.Taxable
	}
	unique := make([]TaxabilityFlag, 0, len(seen))
	for jurisdiction, taxable := range seen {
		unique = append(unique, TaxabilityFlag{Jurisdiction: jurisdiction, Taxable: taxable})
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].Jurisdiction < unique[j].Jurisdiction })
	return unique, nil
}

func (d Definition) Validate() error {
	if strings.TrimSpace(d.id()) == "" {
		return fieldError(ErrInvalidDefinition, "id", errors.New("definition id is required"))
	}
	if strings.TrimSpace(d.Code) == "" {
		return fieldError(ErrInvalidDefinition, "code", errors.New("definition code is required"))
	}
	if !d.kind().Valid() {
		return fieldError(ErrInvalidDefinition, "kind", errors.New("kind is not declared"))
	}
	if strings.TrimSpace(d.Currency) == "" {
		return fieldError(ErrInvalidDefinition, "currency", errors.New("currency is required"))
	}
	if !d.basis().Valid() {
		return fieldError(ErrInvalidDefinition, "calculation_basis", errors.New("basis is not declared"))
	}
	if strings.TrimSpace(d.accountingCode()) == "" {
		return fieldError(ErrInvalidDefinition, "accounting_code", errors.New("accounting code is required"))
	}
	if strings.TrimSpace(d.OwnerRef) == "" {
		return fieldError(ErrInvalidDefinition, "owner_ref", errors.New("owner reference is required"))
	}
	if err := d.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidDefinition, "effective", err)
	}
	if strings.TrimSpace(d.Version) == "" || d.Revision == 0 {
		return fieldError(ErrInvalidDefinition, "version", errors.New("version and positive revision are required"))
	}
	state := d.state()
	if !state.Valid() {
		return fieldError(ErrInvalidDefinition, "state", errors.New("lifecycle state is not declared"))
	}
	if d.State != "" && d.Status != "" && d.State != d.Status {
		return fieldError(ErrInvalidDefinition, "state", errors.New("state and status disagree"))
	}
	if d.Revision == 1 && (d.SupersedesRevision != 0 || d.SupersedesDigest != "") {
		return fieldError(ErrDefinitionLineage, "supersedes", errors.New("first revision cannot have a predecessor"))
	}
	if d.Revision > 1 && (d.SupersedesRevision == 0 || strings.TrimSpace(d.SupersedesDigest) == "") {
		return fieldError(ErrDefinitionLineage, "supersedes", errors.New("successor requires predecessor revision and digest"))
	}
	flags, err := d.taxability()
	if err != nil {
		return err
	}
	if len(flags) == 0 {
		return fieldError(ErrInvalidDefinition, "taxability", errors.New("at least one taxability flag is required"))
	}
	limits := d.limits()
	if d.Minimum.Validate() == nil && d.Limits.Minimum.Validate() == nil && !d.Minimum.Equal(d.Limits.Minimum) {
		return fieldError(ErrInvalidDefinition, "limits.minimum", errors.New("minimum aliases disagree"))
	}
	if d.Maximum.Validate() == nil && d.Limits.Maximum.Validate() == nil && !d.Maximum.Equal(d.Limits.Maximum) {
		return fieldError(ErrInvalidDefinition, "limits.maximum", errors.New("maximum aliases disagree"))
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if d.Minimum.Validate() == nil && d.Maximum.Validate() == nil && d.Minimum.Cmp(d.Maximum) > 0 {
		return fieldError(ErrInvalidDefinition, "limits", errors.New("minimum exceeds maximum"))
	}
	if d.basis() == BasisFormula && strings.TrimSpace(d.FormulaRef) == "" {
		return fieldError(ErrInvalidDefinition, "formula_ref", errors.New("formula reference is required for formula basis"))
	}
	if d.CanonicalDigest != "" && d.CanonicalDigest != d.computedDigest() {
		return fieldError(ErrInvalidDefinition, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (d Definition) body() []byte {
	flags, _ := d.taxability()
	limits := d.limits()
	w := canonicalbytes.New("hcmnext.domains.payinput.Definition", schemaVersion).
		String("id", d.id()).String("code", d.Code).String("kind", string(d.kind())).
		String("currency", d.Currency).String("calculation_basis", string(d.basis())).
		Value("effective", d.Effective).Value("limits", limits).
		String("formula_ref", d.FormulaRef).String("accounting_code", d.accountingCode()).
		String("owner_ref", d.OwnerRef).String("version", d.Version).
		Int("revision", int64(d.Revision)).String("state", string(d.state())).
		String("supersedes_digest", d.SupersedesDigest).Int("supersedes_revision", int64(d.SupersedesRevision)).
		Count("taxability", len(flags))
	for _, flag := range flags {
		w.Value("taxability", flag)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (d Definition) computedDigest() string { return canonicalbytes.Digest(d.body()) }

// NewDefinition validates and digests a definition. A zero lifecycle state is
// treated as PUBLISHED for the constructor so a caller can immediately bind a
// new definition; direct struct validation still requires an explicit state.
func NewDefinition(d Definition) (Definition, error) {
	if d.State == "" && d.Status == "" {
		d.State, d.Status = StatePublished, StatePublished
	} else if d.State == "" {
		d.State = d.Status
	} else if d.Status == "" {
		d.Status = d.State
	}
	if d.DefinitionID == "" {
		d.DefinitionID = d.ID
	}
	if d.ID == "" {
		d.ID = d.DefinitionID
	}
	if d.CalculationBasis == "" {
		d.CalculationBasis = d.Basis
	}
	if d.Basis == "" {
		d.Basis = d.CalculationBasis
	}
	if d.Kind == "" {
		d.Kind = d.Type
	}
	if d.Type == "" {
		d.Type = d.Kind
	}
	if d.AccountingCode == "" {
		d.AccountingCode = d.AccountingComponentCode
	}
	if d.Revision == 0 {
		d.Revision = 1
	}
	d.CanonicalDigest = ""
	if err := d.Validate(); err != nil {
		return Definition{}, err
	}
	d.CanonicalDigest = d.computedDigest()
	return d, nil
}

// NewVersion returns a successor definition and leaves d unchanged.
func (d Definition) NewVersion(version string, effective values.EffectiveInterval) (Definition, error) {
	if err := d.Validate(); err != nil {
		return Definition{}, err
	}
	next := d
	next.Version, next.Effective = version, effective
	next.Revision, next.SupersedesRevision = d.Revision+1, d.Revision
	next.SupersedesDigest, next.CanonicalDigest = d.CanonicalDigest, ""
	next.State, next.Status = StatePublished, StatePublished
	return NewDefinition(next)
}

// Retire returns a new immutable retired revision.
func (d Definition) Retire(version string) (Definition, error) {
	next := d
	if version != "" {
		next.Version = version
	}
	next.State, next.Status = StateRetired, StateRetired
	return d.NewRevision(next)
}

// NewRevision validates an explicitly prepared successor.
func (d Definition) NewRevision(next Definition) (Definition, error) {
	if err := d.Validate(); err != nil {
		return Definition{}, err
	}
	next.ID, next.DefinitionID = d.ID, d.DefinitionID
	next.Revision = d.Revision + 1
	next.SupersedesRevision, next.SupersedesDigest = d.Revision, d.CanonicalDigest
	next.CanonicalDigest = ""
	return NewDefinition(next)
}

// Canonical returns the validated canonical definition bytes.
func (d Definition) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	return d.body()
}

// Digest returns the canonical digest of a validated definition.
func (d Definition) Digest() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	return d.computedDigest(), nil
}

// Recurrence is the closed schedule vocabulary for worker assignments.
type Recurrence string

const (
	RecurrenceOneTime    Recurrence = "ONE_TIME"
	RecurrencePerPayroll Recurrence = "PER_PAYROLL_PERIOD"
	RecurrenceMonthly    Recurrence = "MONTHLY"
	RecurrenceAnnual     Recurrence = "ANNUAL"
	OneTime                         = RecurrenceOneTime
	PerPayrollPeriod                = RecurrencePerPayroll
	Monthly                         = RecurrenceMonthly
	Annual                          = RecurrenceAnnual
)

func (r Recurrence) Valid() bool {
	switch r {
	case RecurrenceOneTime, RecurrencePerPayroll, RecurrenceMonthly, RecurrenceAnnual:
		return true
	default:
		return false
	}
}

// DefinitionRef binds an assignment to one exact immutable definition.
type DefinitionRef struct {
	ID      string
	Version string
	Digest  string
}

func (r DefinitionRef) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fieldError(ErrInvalidAssignment, "definition_ref.id", errors.New("definition id is required"))
	}
	if strings.TrimSpace(r.Version) == "" {
		return fieldError(ErrInvalidAssignment, "definition_ref.version", errors.New("definition version is required"))
	}
	if strings.TrimSpace(r.Digest) == "" {
		return fieldError(ErrInvalidAssignment, "definition_ref.digest", errors.New("definition digest is required"))
	}
	return nil
}

func (r DefinitionRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.payinput.DefinitionRef", schemaVersion).
		String("id", r.ID).String("version", r.Version).String("digest", r.Digest).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// WorkerAssignment is one immutable worker binding of a definition.
type WorkerAssignment struct {
	ID                string
	AssignmentID      string
	WorkerRef         string
	DefinitionRef     DefinitionRef
	DefinitionID      string
	DefinitionVersion string
	DefinitionDigest  string
	Effective         values.EffectiveInterval
	EffectiveWindow   values.EffectiveInterval
	Amount            values.Decimal
	Rate              values.Decimal
	FormulaRef        string
	Recurrence        Recurrence
	RecurrenceRule    string
	PeriodRule        string
	CanonicalDigest   string
}

type Assignment = WorkerAssignment

func (a WorkerAssignment) id() string {
	if a.AssignmentID != "" {
		return a.AssignmentID
	}
	return a.ID
}

func (a WorkerAssignment) effective() values.EffectiveInterval {
	if a.Effective.Kind() != values.IntervalKindUnspecified {
		return a.Effective
	}
	return a.EffectiveWindow
}

func (a WorkerAssignment) definitionRef() DefinitionRef {
	r := a.DefinitionRef
	if r.ID == "" {
		r.ID = a.DefinitionID
	}
	if r.Version == "" {
		r.Version = a.DefinitionVersion
	}
	if r.Digest == "" {
		r.Digest = a.DefinitionDigest
	}
	return r
}

func (a WorkerAssignment) Validate() error {
	if strings.TrimSpace(a.id()) == "" {
		return fieldError(ErrInvalidAssignment, "id", errors.New("assignment id is required"))
	}
	if strings.TrimSpace(a.WorkerRef) == "" {
		return fieldError(ErrInvalidAssignment, "worker_ref", errors.New("worker reference is required"))
	}
	if err := a.definitionRef().Validate(); err != nil {
		return err
	}
	if err := a.effective().Validate(); err != nil {
		return fieldError(ErrInvalidAssignment, "effective", err)
	}
	if !a.Recurrence.Valid() {
		return fieldError(ErrInvalidAssignment, "recurrence", ErrUnknownRecurrence)
	}
	period := a.RecurrenceRule
	if period == "" {
		period = a.PeriodRule
	}
	if a.Recurrence != RecurrenceOneTime && strings.TrimSpace(period) == "" {
		return fieldError(ErrInvalidAssignment, "recurrence_rule", errors.New("period rule is required for recurring assignments"))
	}
	amountSet, rateSet := a.Amount.Validate() == nil, a.Rate.Validate() == nil
	if amountSet == rateSet {
		return fieldError(ErrInvalidAssignment, "amount_or_rate", errors.New("exactly one amount or rate is required"))
	}
	if amountSet && a.Amount.Sign() < 0 || rateSet && a.Rate.Sign() < 0 {
		return fieldError(ErrInvalidAssignment, "amount_or_rate", errors.New("value cannot be negative"))
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != a.computedDigest() {
		return fieldError(ErrInvalidAssignment, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (a WorkerAssignment) body() []byte {
	r := a.definitionRef()
	w := canonicalbytes.New("hcmnext.domains.payinput.WorkerAssignment", schemaVersion).
		String("id", a.id()).String("worker_ref", a.WorkerRef).Value("definition_ref", r).
		Value("effective", a.effective()).String("recurrence", string(a.Recurrence)).
		String("recurrence_rule", a.RecurrenceRule).String("period_rule", a.PeriodRule).
		Bool("amount_present", a.Amount.Validate() == nil).Bool("rate_present", a.Rate.Validate() == nil).
		String("formula_ref", a.FormulaRef)
	if a.Amount.Validate() == nil {
		w.Value("amount", a.Amount)
	}
	if a.Rate.Validate() == nil {
		w.Value("rate", a.Rate)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (a WorkerAssignment) computedDigest() string { return canonicalbytes.Digest(a.body()) }

// NewWorkerAssignment validates, binds and digests an assignment. The
// definition must be published or draft; retired and superseded revisions are
// refused so a new payroll input cannot silently bind stale meaning.
func NewWorkerAssignment(definition Definition, assignment WorkerAssignment) (WorkerAssignment, error) {
	if err := definition.Validate(); err != nil {
		return WorkerAssignment{}, err
	}
	if definition.state() == StateRetired || definition.state() == StateSuperseded {
		return WorkerAssignment{}, fmt.Errorf("%w: %s/%s", ErrDefinitionRetired, definition.id(), definition.Version)
	}
	amountSet, rateSet := assignment.Amount.Validate() == nil, assignment.Rate.Validate() == nil
	switch definition.basis() {
	case BasisPercentage:
		if !rateSet || amountSet {
			return WorkerAssignment{}, fieldError(ErrInvalidAssignment, "rate", errors.New("percentage definitions require a rate"))
		}
	case BasisFormula:
		if strings.TrimSpace(assignment.FormulaRef) == "" {
			return WorkerAssignment{}, fieldError(ErrInvalidAssignment, "formula_ref", errors.New("formula assignment reference is required"))
		}
	default:
		if !amountSet || rateSet {
			return WorkerAssignment{}, fieldError(ErrInvalidAssignment, "amount", errors.New("this calculation basis requires an amount"))
		}
	}
	limits := definition.limits()
	value := assignment.Amount
	if rateSet {
		value = assignment.Rate
	}
	if limits.Minimum.Validate() == nil && value.Cmp(limits.Minimum) < 0 || limits.Maximum.Validate() == nil && value.Cmp(limits.Maximum) > 0 {
		return WorkerAssignment{}, fieldError(ErrInvalidAssignment, "amount_or_rate", errors.New("assignment is outside definition limits"))
	}
	ref := assignment.definitionRef()
	if ref.ID != "" && (ref.ID != definition.id() || ref.Version != definition.Version || ref.Digest != definition.CanonicalDigest) {
		return WorkerAssignment{}, fmt.Errorf("%w: expected %s/%s/%s", ErrAssignmentDefinitionRef, definition.id(), definition.Version, definition.CanonicalDigest)
	}
	assignment.DefinitionRef = DefinitionRef{ID: definition.id(), Version: definition.Version, Digest: definition.CanonicalDigest}
	assignment.DefinitionID, assignment.DefinitionVersion, assignment.DefinitionDigest = definition.id(), definition.Version, definition.CanonicalDigest
	if assignment.AssignmentID == "" {
		assignment.AssignmentID = assignment.ID
	}
	if assignment.ID == "" {
		assignment.ID = assignment.AssignmentID
	}
	assignment.CanonicalDigest = ""
	if err := assignment.Validate(); err != nil {
		return WorkerAssignment{}, err
	}
	assignment.CanonicalDigest = assignment.computedDigest()
	return assignment, nil
}

// NewAssignment is a flexible boundary constructor supporting either
// NewAssignment(definition, assignment) or NewAssignment(assignment,
// definition), which keeps integrations from duplicating reference binding.
func NewAssignment(args ...any) (WorkerAssignment, error) {
	if len(args) != 2 {
		return WorkerAssignment{}, fieldError(ErrInvalidAssignment, "arguments", errors.New("definition and assignment are required"))
	}
	var definition Definition
	var assignment WorkerAssignment
	for _, arg := range args {
		switch value := arg.(type) {
		case Definition:
			definition = value
		case WorkerAssignment:
			assignment = value
		default:
			return WorkerAssignment{}, fieldError(ErrInvalidAssignment, "arguments", errors.New("unsupported constructor argument"))
		}
	}
	return NewWorkerAssignment(definition, assignment)
}

// ValidateAssignments rejects overlapping effective windows for one worker.
func ValidateAssignments(assignments []WorkerAssignment) error {
	copyOf := append([]WorkerAssignment(nil), assignments...)
	sort.Slice(copyOf, func(i, j int) bool {
		if copyOf[i].WorkerRef != copyOf[j].WorkerRef {
			return copyOf[i].WorkerRef < copyOf[j].WorkerRef
		}
		return copyOf[i].effective().String() < copyOf[j].effective().String()
	})
	for i := range copyOf {
		if err := copyOf[i].Validate(); err != nil {
			return err
		}
		for j := i + 1; j < len(copyOf) && copyOf[j].WorkerRef == copyOf[i].WorkerRef; j++ {
			overlaps, err := copyOf[i].effective().Overlaps(copyOf[j].effective())
			if err != nil {
				return fieldError(ErrOverlappingAssignments, "effective", err)
			}
			if overlaps {
				return fmt.Errorf("%w: worker_ref=%s assignments=%s,%s", ErrOverlappingAssignments, copyOf[i].WorkerRef, copyOf[i].id(), copyOf[j].id())
			}
		}
	}
	return nil
}

// CheckAssignments is an explicit alias for ValidateAssignments.
func CheckAssignments(assignments []WorkerAssignment) error { return ValidateAssignments(assignments) }

// DefinitionExplanation is a bounded, digest-oriented explanation.
type DefinitionExplanation struct {
	ID       string
	Code     string
	Kind     DefinitionKind
	Version  string
	Revision uint64
	State    DefinitionState
	Digest   string
}

func (d Definition) Explain() (DefinitionExplanation, error) {
	if err := d.Validate(); err != nil {
		return DefinitionExplanation{}, err
	}
	return DefinitionExplanation{ID: d.id(), Code: d.Code, Kind: d.Kind, Version: d.Version, Revision: d.Revision, State: d.state(), Digest: d.CanonicalDigest}, nil
}

func Explain(d Definition) (DefinitionExplanation, error) { return d.Explain() }

// AssignmentExplanation is a value-free assignment summary.
type AssignmentExplanation struct {
	ID         string
	WorkerRef  string
	Definition DefinitionRef
	Recurrence Recurrence
	Digest     string
}

func (a WorkerAssignment) Explain() (AssignmentExplanation, error) {
	if err := a.Validate(); err != nil {
		return AssignmentExplanation{}, err
	}
	return AssignmentExplanation{ID: a.id(), WorkerRef: a.WorkerRef, Definition: a.definitionRef(), Recurrence: a.Recurrence, Digest: a.CanonicalDigest}, nil
}

// DefinitionCatalog is an in-memory reference port for pure domain tests and
// adapters. It does not imply durable storage or provider authority.
type DefinitionCatalog interface {
	Put(Definition) error
	Get(id, version string) (Definition, error)
	Assign(WorkerAssignment) error
	Assignments(worker string) []WorkerAssignment
}

// InMemoryCatalog is a detached catalog implementation used by callers that
// need a fake or local semantic boundary.
type InMemoryCatalog struct {
	mu          sync.RWMutex
	definitions map[string]Definition
	assignments []WorkerAssignment
}

func NewInMemoryCatalog(definitions []Definition) (*InMemoryCatalog, error) {
	c := &InMemoryCatalog{definitions: make(map[string]Definition)}
	for _, definition := range definitions {
		if err := c.Put(definition); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func catalogKey(id, version string) string { return id + "\x00" + version }

func (c *InMemoryCatalog) Put(definition Definition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.definitions == nil {
		c.definitions = make(map[string]Definition)
	}
	c.definitions[catalogKey(definition.id(), definition.Version)] = definition
	return nil
}

func (c *InMemoryCatalog) Get(id, version string) (Definition, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	definition, ok := c.definitions[catalogKey(id, version)]
	if !ok {
		return Definition{}, fmt.Errorf("%w: definition %s/%s not found", ErrInvalidDefinition, id, version)
	}
	return definition, nil
}

func (c *InMemoryCatalog) Assign(assignment WorkerAssignment) error {
	if err := assignment.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	definition, ok := c.definitions[catalogKey(assignment.definitionRef().ID, assignment.definitionRef().Version)]
	if !ok {
		return fmt.Errorf("%w: definition is not cataloged", ErrAssignmentDefinitionRef)
	}
	if definition.CanonicalDigest != assignment.definitionRef().Digest {
		return fmt.Errorf("%w: digest differs from catalog", ErrAssignmentDefinitionRef)
	}
	if definition.state() == StateRetired || definition.state() == StateSuperseded {
		return ErrDefinitionRetired
	}
	if err := ValidateAssignments(append(c.assignments, assignment)); err != nil {
		return err
	}
	c.assignments = append(c.assignments, assignment)
	return nil
}

func (c *InMemoryCatalog) Assignments(worker string) []WorkerAssignment {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]WorkerAssignment, 0)
	for _, assignment := range c.assignments {
		if assignment.WorkerRef == worker {
			result = append(result, assignment)
		}
	}
	return result
}

type ValidationError struct {
	Field string
	Cause error
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %v", e.Field, e.Cause) }
func (e *ValidationError) Unwrap() error { return e.Cause }

func fieldError(root error, field string, cause error) error {
	return fmt.Errorf("%w: %w", root, &ValidationError{Field: field, Cause: cause})
}
