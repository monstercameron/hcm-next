package app

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/engines/fielddiff"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ComparisonFields is the closed projection this cell compares against the
// external system of record.
//
// It is the intersection of what internal/domains/people masters and what the
// incumbent connector observes, in canonical order. It is closed on purpose: a
// drift run is an operator diagnostic, and a projection that grew implicitly
// with whatever the connector happened to return would be a bulk export of the
// tenant wearing a diagnostic's name.
func ComparisonFields() []dataops.FieldID {
	return []dataops.FieldID{
		dataops.FieldID(people.FieldEmploymentStatus),
		dataops.FieldID(people.FieldHireDate),
		dataops.FieldID(people.FieldWorkerType),
		dataops.FieldID(people.FieldEmploymentID),
		dataops.FieldID(people.FieldGrade),
		dataops.FieldID(people.FieldJobCode),
		dataops.FieldID(people.FieldLocation),
		dataops.FieldID(people.FieldOrgUnit),
		dataops.FieldID(people.FieldPayZone),
		dataops.FieldID(people.FieldPositionID),
		dataops.FieldID(people.FieldFTE),
		dataops.FieldID(people.FieldLegalName),
		dataops.FieldID(people.FieldPreferredName),
		dataops.FieldID(people.FieldWorkerNumber),
		dataops.FieldID(people.FieldLifecycleStatus),
	}
}

// fieldKinds is the declared value kind of every comparison field.
//
// The kind is what makes a comparison meaningful rather than textual: two
// sides that declare different kinds for one field are a mapping defect, and
// the comparison engine says so instead of reporting a value mismatch. Both
// sides of this cell's comparison read this one table, so they cannot disagree
// about what a field is.
var fieldKinds = map[dataops.FieldID]fielddiff.ValueKind{
	dataops.FieldID(people.FieldWorkerNumber):     fielddiff.KindString,
	dataops.FieldID(people.FieldLifecycleStatus):  fielddiff.KindEnum,
	dataops.FieldID(people.FieldLegalName):        fielddiff.KindString,
	dataops.FieldID(people.FieldPreferredName):    fielddiff.KindString,
	dataops.FieldID(people.FieldEmploymentID):     fielddiff.KindString,
	dataops.FieldID(people.FieldWorkerType):       fielddiff.KindEnum,
	dataops.FieldID(people.FieldHireDate):         fielddiff.KindDate,
	dataops.FieldID(people.FieldEmploymentStatus): fielddiff.KindEnum,
	dataops.FieldID(people.FieldJobCode):          fielddiff.KindEnum,
	dataops.FieldID(people.FieldGrade):            fielddiff.KindEnum,
	dataops.FieldID(people.FieldOrgUnit):          fielddiff.KindEnum,
	dataops.FieldID(people.FieldPositionID):       fielddiff.KindString,
	dataops.FieldID(people.FieldLocation):         fielddiff.KindString,
	dataops.FieldID(people.FieldPayZone):          fielddiff.KindEnum,
	dataops.FieldID(people.FieldFTE):              fielddiff.KindDecimal,
}

// kindOf returns the declared kind of a comparison field, or KindString for a
// field outside the closed projection.
func kindOf(f dataops.FieldID) fielddiff.ValueKind {
	if k, ok := fieldKinds[f]; ok {
		return k
	}
	return fielddiff.KindString
}

// workerFieldHistory is the canonical half of every comparison: a
// [dataops.FieldHistory] over the governed worker read this cell already
// serves explain_worker_state from.
//
// It exists so that the canonical side of a drift finding is the same fact,
// read through the same port, that the worker-state explanation would have
// disclosed. A second read path would eventually disagree with the first, and
// then the diagnostic would be diagnosing itself.
type workerFieldHistory struct {
	workers people.WorkerFacts
}

var _ dataops.FieldHistory = (*workerFieldHistory)(nil)

// FieldHistoryAt implements [dataops.FieldHistory].
//
// The query carries a knowledge cut-off but no business date, because a field
// history is the whole timeline and the debugger picks the assertion in force
// itself. The governed worker read needs both coordinates, so the business
// date is taken from the cut-off: reading "what was recorded as of T" at the
// business date T is the coordinate that reproduces the belief held at T.
func (h *workerFieldHistory) FieldHistoryAt(ctx context.Context, q dataops.HistoryQuery) (dataops.HistorySet, error) {
	if err := q.Validate(); err != nil {
		return dataops.HistorySet{}, err
	}
	fields := make([]people.FieldID, 0, len(q.Fields))
	for _, f := range q.Fields {
		field := people.FieldID(f)
		if err := field.Validate(); err != nil {
			// A field this cell does not master has no canonical assertion.
			// It is absent from the answer rather than fabricated, and the
			// comparison reports it as canonically unasserted.
			continue
		}
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return dataops.HistorySet{Subject: q.Subject, Exists: true, Watermark: values.UnspecifiedRevision()}, nil
	}

	at := q.KnownAt.Instant().Time().UTC()
	on, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return dataops.HistorySet{}, fmt.Errorf("app: field history business date: %w", err)
	}
	set, err := h.workers.WorkerFactsAt(ctx, people.FactQuery{
		Tenant: q.Tenant,
		Worker: q.Subject,
		AsOf:   people.AsOf{EffectiveOn: on, KnownAt: q.KnownAt},
		Fields: fields,
	})
	if err != nil {
		return dataops.HistorySet{}, err
	}
	if !set.Exists {
		return dataops.HistorySet{Subject: q.Subject, Exists: false}, nil
	}

	out := dataops.HistorySet{Subject: q.Subject, Exists: true, Watermark: set.Watermark}
	for _, fact := range set.Facts {
		assertion, convErr := assertionFrom(fact)
		if convErr != nil {
			return dataops.HistorySet{}, convErr
		}
		if assertion.KnownAt.Instant().After(q.KnownAt.Instant()) {
			// The port applies the cut-off itself: an assertion that became
			// known after the caller's knowledge horizon is invisible, which
			// is how a past belief is reproduced rather than reconstructed.
			continue
		}
		if assertion.Class.IsClaim() && !q.IncludeClaims {
			continue
		}
		out.Assertions = append(out.Assertions, assertion)
	}
	return out, nil
}

// assertionFrom projects one governed worker fact onto the debugger's
// assertion shape.
//
// Three of the assertion's fields have no counterpart on a worker fact and are
// derived rather than invented: the identity is derived from the field and the
// revision it was read at (so re-reading the same fact is the same assertion),
// the class follows the fact's own source authority, and the change kind is
// INITIAL because the governed worker read publishes the assertion in force,
// not the correction lineage behind it. A cell that published a fabricated
// correction lineage would be answering the debugger's central question with
// a guess.
func assertionFrom(fact people.Fact) (dataops.Assertion, error) {
	if err := fact.Validate(); err != nil {
		return dataops.Assertion{}, fmt.Errorf("app: governed worker fact: %w", err)
	}
	return dataops.Assertion{
		ID:         string(fact.Field) + "@" + string(fact.Revision.Canonical()),
		Field:      dataops.FieldID(fact.Field),
		Kind:       kindOf(dataops.FieldID(fact.Field)),
		Value:      fact.Value,
		Effective:  fact.Effective,
		KnownAt:    fact.KnownAt,
		Revision:   fact.Revision,
		Class:      assertionClassOf(fact),
		Change:     dataops.ChangeInitial,
		Authority:  fact.Authority,
		Provenance: fact.Provenance,
	}, nil
}

// assertionClassOf reads the class off the fact's own source authority. An
// observed incumbent value must never be published as a local domain fact, and
// this is the one place that distinction is preserved on the way in.
func assertionClassOf(fact people.Fact) dataops.AssertionClass {
	switch fact.Authority.Kind {
	case evidence.AuthorityExternalObservation:
		return dataops.ClassExternalObservation
	default:
		return dataops.ClassDomainFact
	}
}
