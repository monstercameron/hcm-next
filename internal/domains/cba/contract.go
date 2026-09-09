package cba

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const cbaSchemaVersion = 1

// FieldError identifies the field that made a CBA record unusable.
type FieldError struct {
	Field string
	Cause error
}

func (e *FieldError) Error() string {
	if e == nil {
		return "cba: invalid field"
	}
	return fmt.Sprintf("cba: field %s: %v", e.Field, e.Cause)
}

func (e *FieldError) Unwrap() error { return e.Cause }

func fieldError(field, message string) error {
	return &FieldError{Field: field, Cause: errors.New(message)}
}

// ValidateAgreement is the field-specific validation entry point. Validate
// remains available for the original concise contract in agreement.go.
func ValidateAgreement(a AgreementRevision) error {
	if strings.TrimSpace(a.ID) == "" {
		return fieldError("id", "is required")
	}
	if strings.TrimSpace(a.AgreementID) == "" {
		return fieldError("agreement_id", "is required")
	}
	if strings.TrimSpace(a.Revision) == "" {
		return fieldError("revision", "is required")
	}
	if a.EffectiveFrom.IsZero() {
		return fieldError("effective_from", "is required")
	}
	if !a.EffectiveTo.IsZero() && !a.EffectiveTo.After(a.EffectiveFrom) {
		return fieldError("effective_to", "must be after effective_from")
	}
	if a.KnownFrom.IsZero() && a.KnownAt.IsZero() {
		return fieldError("known_from", "is required")
	}
	if !a.KnownTo.IsZero() && !a.KnownTo.After(a.KnownFromOrAt()) {
		return fieldError("known_to", "must be after known_from")
	}
	if strings.TrimSpace(a.Representative) == "" {
		return fieldError("representative", "is required")
	}
	if strings.TrimSpace(a.Source) == "" {
		return fieldError("source", "is required")
	}
	return nil
}

func (a AgreementRevision) KnownFromOrAt() time.Time {
	if !a.KnownFrom.IsZero() {
		return a.KnownFrom
	}
	return a.KnownAt
}

// ValidateUnit is the field-specific validation entry point for a unit.
func ValidateUnit(u BargainingUnitRevision) error {
	if strings.TrimSpace(u.ID) == "" {
		return fieldError("id", "is required")
	}
	if strings.TrimSpace(u.UnitID) == "" {
		return fieldError("unit_id", "is required")
	}
	if strings.TrimSpace(u.AgreementID) == "" {
		return fieldError("agreement_id", "is required")
	}
	if strings.TrimSpace(u.Revision) == "" {
		return fieldError("revision", "is required")
	}
	if u.EffectiveFrom.IsZero() {
		return fieldError("effective_from", "is required")
	}
	if !u.EffectiveTo.IsZero() && !u.EffectiveTo.After(u.EffectiveFrom) {
		return fieldError("effective_to", "must be after effective_from")
	}
	if u.KnownFrom.IsZero() {
		return fieldError("known_from", "is required")
	}
	if !u.KnownTo.IsZero() && !u.KnownTo.After(u.KnownFrom) {
		return fieldError("known_to", "must be after known_from")
	}
	if strings.TrimSpace(u.Representative) == "" {
		return fieldError("representative", "is required")
	}
	if strings.TrimSpace(u.Source) == "" {
		return fieldError("source", "is required")
	}
	return nil
}

// ValidateMembership is the field-specific validation entry point for a
// worker's unit placement.
func ValidateMembership(m MembershipRevision) error {
	if strings.TrimSpace(m.ID) == "" {
		return fieldError("id", "is required")
	}
	if strings.TrimSpace(m.MembershipID) == "" {
		return fieldError("membership_id", "is required")
	}
	if strings.TrimSpace(m.WorkerID) == "" {
		return fieldError("worker_id", "is required")
	}
	if strings.TrimSpace(m.UnitID) == "" {
		return fieldError("unit_id", "is required")
	}
	if strings.TrimSpace(m.Revision) == "" {
		return fieldError("revision", "is required")
	}
	if m.EffectiveFrom.IsZero() {
		return fieldError("effective_from", "is required")
	}
	if !m.EffectiveTo.IsZero() && !m.EffectiveTo.After(m.EffectiveFrom) {
		return fieldError("effective_to", "must be after effective_from")
	}
	if m.KnownFrom.IsZero() {
		return fieldError("known_from", "is required")
	}
	if !m.KnownTo.IsZero() && !m.KnownTo.After(m.KnownFrom) {
		return fieldError("known_to", "must be after known_from")
	}
	if strings.TrimSpace(m.Source) == "" {
		return fieldError("source", "is required")
	}
	return nil
}

func cbaTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// Canonical returns the stable, value-free encoding of an agreement revision.
func (a AgreementRevision) Canonical() []byte {
	if err := ValidateAgreement(a); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.cba.AgreementRevision", cbaSchemaVersion).
		String("id", a.ID).String("agreement_id", a.AgreementID).
		String("revision", a.Revision).String("version", a.Version).
		String("title", a.Title).String("representative", a.Representative).
		String("source", a.Source).String("effective_from", cbaTime(a.EffectiveFrom)).
		String("effective_to", cbaTime(a.EffectiveTo)).String("known_from", cbaTime(a.KnownFromOrAt())).
		String("known_to", cbaTime(a.KnownTo)).Int("precedence", int64(a.Precedence)).Bool("retired", a.Retired).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (a AgreementRevision) Digest() (string, error) {
	raw := a.Canonical()
	if raw == nil {
		return "", ValidateAgreement(a)
	}
	return canonicalbytes.Digest(raw), nil
}

// Canonical returns the stable encoding of a bargaining unit revision.
func (u BargainingUnitRevision) Canonical() []byte {
	if err := ValidateUnit(u); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.cba.BargainingUnitRevision", cbaSchemaVersion).
		String("id", u.ID).String("unit_id", u.UnitID).String("revision", u.Revision).
		String("agreement_id", u.AgreementID).String("name", u.Name).
		String("representative", u.Representative).String("source", u.Source).
		String("effective_from", cbaTime(u.EffectiveFrom)).String("effective_to", cbaTime(u.EffectiveTo)).
		String("known_from", cbaTime(u.KnownFrom)).String("known_to", cbaTime(u.KnownTo)).Bool("retired", u.Retired).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (u BargainingUnitRevision) Digest() (string, error) {
	raw := u.Canonical()
	if raw == nil {
		return "", ValidateUnit(u)
	}
	return canonicalbytes.Digest(raw), nil
}

// Canonical returns the stable encoding of a membership revision.
func (m MembershipRevision) Canonical() []byte {
	if err := ValidateMembership(m); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.cba.MembershipRevision", cbaSchemaVersion).
		String("id", m.ID).String("membership_id", m.MembershipID).String("revision", m.Revision).
		String("worker_id", m.WorkerID).String("unit_id", m.UnitID).String("source", m.Source).
		String("effective_from", cbaTime(m.EffectiveFrom)).String("effective_to", cbaTime(m.EffectiveTo)).
		String("known_from", cbaTime(m.KnownFrom)).String("known_to", cbaTime(m.KnownTo)).Bool("retired", m.Retired).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (m MembershipRevision) Digest() (string, error) {
	raw := m.Canonical()
	if raw == nil {
		return "", ValidateMembership(m)
	}
	return canonicalbytes.Digest(raw), nil
}

// Canonical returns the evidence-bearing applicability decision.
func (r ApplicabilityResult) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.cba.ApplicabilityResult", cbaSchemaVersion).
		String("outcome", string(r.Outcome)).String("agreement_id", r.AgreementID).
		String("agreement_revision", r.AgreementRevision).String("unit_id", r.UnitID).
		String("unit_revision", r.UnitRevision).String("membership_id", r.MembershipID).
		String("membership_revision", r.MembershipRevision).String("representative", r.Representative).
		String("source", r.Source).String("precedence_basis", r.PrecedenceBasis).
		String("effective_from", cbaTime(r.EffectiveFrom)).String("effective_to", cbaTime(r.EffectiveTo)).
		String("known_from", cbaTime(r.KnownFrom)).String("known_to", cbaTime(r.KnownTo)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r ApplicabilityResult) Digest() (string, error) {
	raw := r.Canonical()
	if raw == nil {
		return "", r.Validate()
	}
	return canonicalbytes.Digest(raw), nil
}

// Explain returns an audit-safe summary and never repeats a clause body or
// worker/location value. It names references and the decision basis only.
func (r ApplicabilityResult) Explain() string {
	return fmt.Sprintf("cba applicability %s: agreement=%s revision=%s unit=%s membership=%s basis=%s effective=%s known=%s",
		r.Outcome, r.AgreementID, r.AgreementRevision, r.UnitID, r.MembershipID,
		r.PrecedenceBasis, cbaTime(r.EffectiveFrom), cbaTime(r.KnownFrom))
}

// Explain is the package-level audit explanation entry point.
func Explain(r ApplicabilityResult) string { return r.Explain() }

// WorkerFactSet is the minimum sourced worker context needed to scope clauses.
type WorkerFactSet struct {
	WorkerID   string
	JobCode    string
	LocationID string
	Source     string
}

func (f WorkerFactSet) Validate() error {
	if f.WorkerID == "" {
		return fieldError("worker_id", "is required")
	}
	if f.JobCode == "" {
		return fieldError("job_code", "is required")
	}
	if f.LocationID == "" {
		return fieldError("location_id", "is required")
	}
	if f.Source == "" {
		return fieldError("source", "is required")
	}
	return nil
}

// AgreementClauseRevision is an immutable clause scope. Clause text is not
// carried into explanations; callers can cite ID, revision, and source.
type AgreementClauseRevision struct {
	ID, AgreementID, AgreementRevision, Revision string
	Kind, Source                                 string
	JobCodes, LocationIDs                        []string
	EffectiveFrom, EffectiveTo                   time.Time
	KnownFrom, KnownTo                           time.Time
	Retired                                      bool
}

func (c AgreementClauseRevision) Validate() error {
	if c.ID == "" || c.AgreementID == "" || c.AgreementRevision == "" || c.Revision == "" {
		return fieldError("clause.identity", "id, agreement_id, agreement_revision and revision are required")
	}
	if c.Kind == "" {
		return fieldError("clause.kind", "is required")
	}
	if c.Source == "" {
		return fieldError("clause.source", "is required")
	}
	if c.EffectiveFrom.IsZero() || (!c.EffectiveTo.IsZero() && !c.EffectiveTo.After(c.EffectiveFrom)) {
		return fieldError("clause.effective_window", "is invalid")
	}
	if c.KnownFrom.IsZero() || (!c.KnownTo.IsZero() && !c.KnownTo.After(c.KnownFrom)) {
		return fieldError("clause.known_window", "is invalid")
	}
	return nil
}

func (c AgreementClauseRevision) active(at, known time.Time) bool {
	return !at.Before(c.EffectiveFrom) && (c.EffectiveTo.IsZero() || at.Before(c.EffectiveTo)) &&
		!known.Before(c.KnownFrom) && (c.KnownTo.IsZero() || known.Before(c.KnownTo)) && !c.Retired
}

func (c AgreementClauseRevision) matches(f WorkerFactSet) bool {
	job := len(c.JobCodes) == 0
	for _, v := range c.JobCodes {
		if v == f.JobCode {
			job = true
		}
	}
	location := len(c.LocationIDs) == 0
	for _, v := range c.LocationIDs {
		if v == f.LocationID {
			location = true
		}
	}
	return job && location
}

// Canonical returns a stable reference encoding without clause text.
func (c AgreementClauseRevision) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	b := canonicalbytes.New("hcmnext.domains.cba.AgreementClauseRevision", cbaSchemaVersion).
		String("id", c.ID).String("agreement_id", c.AgreementID).
		String("agreement_revision", c.AgreementRevision).String("revision", c.Revision).
		String("kind", c.Kind).SortedStrings("job_code", c.JobCodes).
		SortedStrings("location_id", c.LocationIDs).String("source", c.Source).
		String("effective_from", cbaTime(c.EffectiveFrom)).String("effective_to", cbaTime(c.EffectiveTo)).
		String("known_from", cbaTime(c.KnownFrom)).String("known_to", cbaTime(c.KnownTo)).Bool("retired", c.Retired)
	raw, err := b.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (c AgreementClauseRevision) Digest() (string, error) {
	raw := c.Canonical()
	if raw == nil {
		return "", c.Validate()
	}
	return canonicalbytes.Digest(raw), nil
}

// ClauseApplicability is a reference-only clause decision.
type ClauseApplicability struct {
	ClauseID, ClauseRevision, Kind, Source string
}

// ApplicabilityPorts are read-only ports. Adapters own storage and may return
// any immutable revisions known at the requested coordinate.
type ApplicabilityPorts struct {
	Agreements  AgreementReader
	Units       UnitReader
	Memberships MembershipReader
	Workers     WorkerFactsReader
	Clauses     ClauseReader
}

type AgreementQuery struct {
	AgreementID          string
	EffectiveAt, KnownAt time.Time
}
type UnitQuery struct {
	AgreementID          string
	EffectiveAt, KnownAt time.Time
}
type MembershipQuery struct {
	WorkerID             string
	EffectiveAt, KnownAt time.Time
}
type WorkerFactsQuery struct {
	WorkerID             string
	EffectiveAt, KnownAt time.Time
}
type ClauseQuery struct {
	AgreementID, AgreementRevision string
	EffectiveAt, KnownAt           time.Time
}

type AgreementReader interface {
	AgreementRevisions(context.Context, AgreementQuery) ([]AgreementRevision, error)
}
type UnitReader interface {
	BargainingUnitRevisions(context.Context, UnitQuery) ([]BargainingUnitRevision, error)
}
type MembershipReader interface {
	MembershipRevisions(context.Context, MembershipQuery) ([]MembershipRevision, error)
}
type WorkerFactsReader interface {
	WorkerFacts(context.Context, WorkerFactsQuery) (WorkerFactSet, error)
}
type ClauseReader interface {
	AgreementClauses(context.Context, ClauseQuery) ([]AgreementClauseRevision, error)
}

var ErrPortFailed = errors.New("cba: applicability port failed")

// PortApplicabilityRequest is the bitemporal, port-backed applicability
// question. Agreement clauses are selected only after unit membership and
// sourced job/location facts have been established.
type PortApplicabilityRequest struct {
	WorkerID, AgreementID string
	EffectiveAt, KnownAt  time.Time
}

type PortApplicabilityResult struct {
	Applicability ApplicabilityResult
	Worker        WorkerFactSet
	Clauses       []ClauseApplicability
}

func (r PortApplicabilityResult) Explain() string {
	return fmt.Sprintf("%s; clauses=%d; worker facts are sourced by port", r.Applicability.Explain(), len(r.Clauses))
}

// ResolveApplicabilityFromPorts obtains all decision inputs through ports,
// then delegates the agreement/unit/membership precedence to the original
// pure resolver. Port failures are UNKNOWN and never guessed as inapplicable.
func ResolveApplicabilityFromPorts(ctx context.Context, ports ApplicabilityPorts, q PortApplicabilityRequest) (PortApplicabilityResult, error) {
	out := PortApplicabilityResult{Applicability: ApplicabilityResult{Outcome: Unknown}}
	if ports.Agreements == nil || ports.Units == nil || ports.Memberships == nil || ports.Workers == nil || ports.Clauses == nil {
		return out, fieldError("ports", "all agreement, unit, membership, worker and clause ports are required")
	}
	if q.WorkerID == "" || q.AgreementID == "" || q.EffectiveAt.IsZero() || q.KnownAt.IsZero() {
		return out, fieldError("request", "worker_id, agreement_id, effective_at and known_at are required")
	}
	facts, err := ports.Workers.WorkerFacts(ctx, WorkerFactsQuery{WorkerID: q.WorkerID, EffectiveAt: q.EffectiveAt, KnownAt: q.KnownAt})
	if err != nil {
		return out, fmt.Errorf("%w: worker facts: %w", ErrPortFailed, err)
	}
	if err := facts.Validate(); err != nil {
		return out, err
	}
	agreements, err := ports.Agreements.AgreementRevisions(ctx, AgreementQuery{AgreementID: q.AgreementID, EffectiveAt: q.EffectiveAt, KnownAt: q.KnownAt})
	if err != nil {
		return out, fmt.Errorf("%w: agreements: %w", ErrPortFailed, err)
	}
	units, err := ports.Units.BargainingUnitRevisions(ctx, UnitQuery{AgreementID: q.AgreementID, EffectiveAt: q.EffectiveAt, KnownAt: q.KnownAt})
	if err != nil {
		return out, fmt.Errorf("%w: units: %w", ErrPortFailed, err)
	}
	memberships, err := ports.Memberships.MembershipRevisions(ctx, MembershipQuery{WorkerID: q.WorkerID, EffectiveAt: q.EffectiveAt, KnownAt: q.KnownAt})
	if err != nil {
		return out, fmt.Errorf("%w: memberships: %w", ErrPortFailed, err)
	}
	decision, err := ResolveApplicability(ApplicabilityRequest{WorkerID: q.WorkerID, AgreementID: q.AgreementID, EffectiveAt: q.EffectiveAt, KnownAt: q.KnownAt, Agreements: agreements, Units: units, Memberships: memberships})
	if err != nil {
		return out, err
	}
	out.Applicability, out.Worker = decision, facts
	if decision.Outcome != Applicable {
		return out, nil
	}
	clauses, err := ports.Clauses.AgreementClauses(ctx, ClauseQuery{AgreementID: decision.AgreementID, AgreementRevision: decision.AgreementRevision, EffectiveAt: q.EffectiveAt, KnownAt: q.KnownAt})
	if err != nil {
		return PortApplicabilityResult{Applicability: ApplicabilityResult{Outcome: Unknown}}, fmt.Errorf("%w: clauses: %w", ErrPortFailed, err)
	}
	for _, clause := range clauses {
		if err := clause.Validate(); err != nil {
			return PortApplicabilityResult{Applicability: ApplicabilityResult{Outcome: Unknown}}, err
		}
		if clause.AgreementID == decision.AgreementID && clause.AgreementRevision == decision.AgreementRevision && clause.active(q.EffectiveAt, q.KnownAt) && clause.matches(facts) {
			out.Clauses = append(out.Clauses, ClauseApplicability{ClauseID: clause.ID, ClauseRevision: clause.Revision, Kind: clause.Kind, Source: clause.Source})
		}
	}
	sort.Slice(out.Clauses, func(i, j int) bool { return out.Clauses[i].ClauseID < out.Clauses[j].ClauseID })
	return out, nil
}
