package people

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PEOPLE-001 implements the two closed, governed reads the People domain
// contract names as `people.person.read` and `people.worker.read`: a
// subject's stable identity facts, and a subject's tenant workforce
// participation, each restricted to a fixed field mask that this package
// alone defines.
//
// Both reads are closed specializations of ExplainWorkerState rather than a
// second query engine over the WorkerFacts port: they share its reader
// contract, its AuthorizationDecision input, its bitemporal AsOf coordinate
// and its non-disclosure rule for a subject the caller may not know exists.
// The specialization is what makes the field mask closed instead of merely
// conventional - PersonFields and WorkerFields are the only Field lists these
// two functions will ever project, so a caller cannot widen a Person or
// Worker read into the Employment or Assignment fields (PEOPLE-002,
// PEOPLE-003 own those), let alone into a compartment - identity-resolution
// claims, candidate data, medical, immigration, or payroll/bank information -
// that has no FieldID in this domain at all.

// PersonFields returns the closed set of fields the Person role discloses:
// the stable human-identity facts People owns independent of any employment.
func PersonFields() []FieldID {
	return []FieldID{FieldLegalName, FieldPreferredName}
}

// WorkerFields returns the closed set of fields the Worker role discloses:
// tenant workforce participation, independent of any particular Employment or
// Assignment.
func WorkerFields() []FieldID {
	return []FieldID{FieldWorkerNumber, FieldLifecycleStatus}
}

// PersonWorkerReadRequest is what ReadPerson and ReadWorker are asked.
//
// It carries no Fields projection, unlike ExplainWorkerStateRequest: Person
// and Worker each expose a fixed field mask, so there is nothing for a caller
// to narrow or widen, and therefore nothing here that could accidentally
// smuggle a wider request through.
type PersonWorkerReadRequest struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   AsOf
	// Authorization is the already-evaluated AuthZ result for this caller,
	// purpose and subject. It must rule on every field in PersonFields (for
	// ReadPerson) or WorkerFields (for ReadWorker).
	Authorization AuthorizationDecision
}

// Validate reports whether the request is well formed on its own terms. The
// full check (including whether Authorization covers the projection) happens
// inside ExplainWorkerState, which is the single place that rule lives.
func (r PersonWorkerReadRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrExplainRequestInvalid, err)
	}
	if err := r.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %w", ErrExplainRequestInvalid, err)
	}
	if r.Worker.Tenant != r.Tenant {
		return fmt.Errorf("%w: worker %s is outside tenant %s", ErrExplainRequestInvalid, r.Worker, r.Tenant)
	}
	if r.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: subject kind is %q, want %q", ErrExplainRequestInvalid, r.Worker.Kind, KindWorker)
	}
	return r.AsOf.Validate()
}

// explainRequest projects this request onto ExplainWorkerState's shape at a
// fixed field mask.
func (r PersonWorkerReadRequest) explainRequest(fields []FieldID) ExplainWorkerStateRequest {
	return ExplainWorkerStateRequest{
		Tenant:        r.Tenant,
		Worker:        r.Worker,
		AsOf:          r.AsOf,
		Fields:        fields,
		Authorization: r.Authorization,
	}
}

// PersonRecord is the typed Person role: one subject's stable identity facts,
// as far as the evaluated authorization decision discloses them.
//
// Subject names the reference the read answered about. This phase of the
// People domain has no independent Person aggregate identifier distinct from
// the worker it is read through (the canonical entity model's `person_id` is
// not yet a separate read path here); Subject therefore carries the same
// worker EntityRef that WorkerRecord.Worker does; PEOPLE-002/PEOPLE-003 own
// the Employment and Assignment facts this type never discloses.
type PersonRecord struct {
	Subject values.EntityRef

	Disclosure     Disclosure
	Presence       SubjectPresence
	WithheldReason string
	AsOf           AsOf

	LegalName     ExplainedFact
	PreferredName ExplainedFact

	Watermark     values.RevisionToken
	PolicyVersion string
}

// WorkerRecord is the typed Worker role: one subject's tenant workforce
// participation, as far as the evaluated authorization decision discloses it.
type WorkerRecord struct {
	Worker values.EntityRef

	Disclosure     Disclosure
	Presence       SubjectPresence
	WithheldReason string
	AsOf           AsOf

	WorkerNumber    ExplainedFact
	LifecycleStatus ExplainedFact

	Watermark     values.RevisionToken
	PolicyVersion string
}

// fieldOrZero returns the explained fact for field, or a zero ExplainedFact
// naming the field when the explanation did not carry one (which happens
// exactly when the subject was WITHHELD and no field was ever projected).
func fieldOrZero(explanation Explanation, field FieldID) ExplainedFact {
	for _, f := range explanation.Fields {
		if f.Field == field {
			return f
		}
	}
	return ExplainedFact{Field: field}
}

// ReadPerson is the PEOPLE-001 `people.person.read` capability: an
// authorized, as-of/known-at read of one subject's stable identity facts.
//
// A subject the caller may not know about returns a WITHHELD record with no
// presence and zero-value facts, exactly as ExplainWorkerState does - this
// function adds a closed field mask, not a second non-disclosure rule.
func ReadPerson(ctx context.Context, reader WorkerFacts, req PersonWorkerReadRequest) (PersonRecord, error) {
	if err := req.Validate(); err != nil {
		return PersonRecord{}, err
	}
	fields := PersonFields()
	explanation, err := ExplainWorkerState(ctx, reader, req.explainRequest(fields))
	if err != nil {
		return PersonRecord{}, err
	}
	return PersonRecord{
		Subject:        explanation.Worker,
		Disclosure:     explanation.Disclosure,
		Presence:       explanation.Presence,
		WithheldReason: explanation.WithheldReason,
		AsOf:           explanation.AsOf,
		LegalName:      fieldOrZero(explanation, FieldLegalName),
		PreferredName:  fieldOrZero(explanation, FieldPreferredName),
		Watermark:      explanation.Watermark,
		PolicyVersion:  explanation.PolicyVersion,
	}, nil
}

// ReadWorker is the PEOPLE-001 `people.worker.read` capability: an
// authorized, as-of/known-at read of one subject's tenant workforce
// participation.
//
// A subject the caller may not know about returns a WITHHELD record with no
// presence and zero-value facts, exactly as ExplainWorkerState does - this
// function adds a closed field mask, not a second non-disclosure rule.
func ReadWorker(ctx context.Context, reader WorkerFacts, req PersonWorkerReadRequest) (WorkerRecord, error) {
	if err := req.Validate(); err != nil {
		return WorkerRecord{}, err
	}
	fields := WorkerFields()
	explanation, err := ExplainWorkerState(ctx, reader, req.explainRequest(fields))
	if err != nil {
		return WorkerRecord{}, err
	}
	return WorkerRecord{
		Worker:          explanation.Worker,
		Disclosure:      explanation.Disclosure,
		Presence:        explanation.Presence,
		WithheldReason:  explanation.WithheldReason,
		AsOf:            explanation.AsOf,
		WorkerNumber:    fieldOrZero(explanation, FieldWorkerNumber),
		LifecycleStatus: fieldOrZero(explanation, FieldLifecycleStatus),
		Watermark:       explanation.Watermark,
		PolicyVersion:   explanation.PolicyVersion,
	}, nil
}
