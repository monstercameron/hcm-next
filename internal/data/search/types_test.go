package search_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/search"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const testWorkerID = "018f5a2e-6b3a-7c3a-8b7a-1a2b3c4d5e6f"

func validSubject(tenant values.TenantId) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: values.Kind(search.KindWorker), Id: testWorkerID}
}

func validRevision(t *testing.T) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision("search.test.stream", 1)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	return rev
}

func validInput(t *testing.T) search.ProjectionInput {
	t.Helper()
	return search.ProjectionInput{
		Tenant:   "acme-corp",
		Subject:  validSubject("acme-corp"),
		Kind:     search.KindWorker,
		Revision: validRevision(t),
		Fields:   map[people.FieldID]string{people.FieldLegalName: "Ada Lovelace"},
	}
}

// TestProjectionInputValidateAcceptsAWellFormedInput is the RED-to-GREEN
// baseline every mutation in [TestProjectionInputValidateRejectsEveryMutation]
// is compared against.
func TestProjectionInputValidateAcceptsAWellFormedInput(t *testing.T) {
	t.Parallel()
	if err := validInput(t).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestProjectionInputValidateRejectsEveryMutation proves every way a
// projection input can be incomplete is caught before [search.Project] would
// ever write anything: search_projection_event is append-only evidence, so
// an incomplete input has to be refused rather than corrected later.
func TestProjectionInputValidateRejectsEveryMutation(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*search.ProjectionInput){
		"no tenant":            func(in *search.ProjectionInput) { in.Tenant = "" },
		"subject wrong tenant": func(in *search.ProjectionInput) { in.Subject = validSubject("other-tenant") },
		"invalid subject":      func(in *search.ProjectionInput) { in.Subject = values.EntityRef{} },
		"unknown kind":         func(in *search.ProjectionInput) { in.Kind = "candidate" },
		"kind/subject mismatch": func(in *search.ProjectionInput) {
			in.Subject.Kind = "position"
		},
		"unspecified revision": func(in *search.ProjectionInput) { in.Revision = values.UnspecifiedRevision() },
	} {
		t.Run(name, func(t *testing.T) {
			in := validInput(t)
			mutate(&in)
			err := in.Validate()
			if err == nil {
				t.Fatalf("Validate(%s) = nil, want an error", name)
			}
			if !errors.Is(err, search.ErrInvalidProjectionInput) {
				t.Errorf("Validate(%s) = %v, want ErrInvalidProjectionInput", name, err)
			}
		})
	}
}
