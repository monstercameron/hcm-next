package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_PEOPLE_001 is the registry's exact primary matrix symbol for
// "Implement authorized Person and Worker reads."
//
// It proves the GREEN clause directly: a current/as-of/known-at query
// returns typed Person and Worker roles, each restricted to its own closed
// field mask, each fact carrying revision, source authority and provenance -
// and that the field masks never reach into another domain's fields or a
// compartment this domain does not model at all.
func TestTodo_PEOPLE_001(t *testing.T) {
	ctx := context.Background()
	worker := workerRef(t, "jane-doe")
	coordinate := asOf(t)

	allFields := append(append([]people.FieldID{}, people.PersonFields()...), people.WorkerFields()...)
	req := people.PersonWorkerReadRequest{
		Tenant:        fixtures.Tenant,
		Worker:        worker,
		AsOf:          coordinate,
		Authorization: fixtures.AllowAll(testPolicyVersion, testPurpose, allFields),
	}

	person, err := people.ReadPerson(ctx, reader(t), req)
	if err != nil {
		t.Fatalf("ReadPerson: %v", err)
	}
	if person.Disclosure != people.DisclosureFull {
		t.Errorf("person disclosure = %s, want FULL", person.Disclosure)
	}
	if person.Presence != people.SubjectPresent {
		t.Errorf("person presence = %s, want PRESENT", person.Presence)
	}
	if person.Subject != worker {
		t.Errorf("person subject = %s, want %s", person.Subject, worker)
	}
	if person.AsOf != coordinate {
		t.Error("ReadPerson must echo the bitemporal coordinate it answered at")
	}
	if !person.Watermark.IsSpecified() {
		t.Error("a disclosed person record must pin the read watermark")
	}
	for name, f := range map[string]people.ExplainedFact{
		"legal_name":     person.LegalName,
		"preferred_name": person.PreferredName,
	} {
		if f.Access != people.AccessAuthorized {
			t.Fatalf("%s access = %s, want AUTHORIZED", name, f.Access)
		}
		if f.Effective.Validate() != nil {
			t.Errorf("%s has no effective interval", name)
		}
		if f.KnownAt.Canonical() == nil {
			t.Errorf("%s has no known-at", name)
		}
		if !f.Revision.IsSpecified() {
			t.Errorf("%s has no revision", name)
		}
		if f.Authority.Validate() != nil {
			t.Errorf("%s has no source authority", name)
		}
		if f.Provenance.Validate() != nil {
			t.Errorf("%s has no provenance", name)
		}
	}
	if v, ok := person.LegalName.Value.Get(); !ok || v != "Jane Doe" {
		t.Errorf("legal name = %q (present %v), want %q", v, ok, "Jane Doe")
	}
	if v, ok := person.PreferredName.Value.Get(); !ok || v != "Jane" {
		t.Errorf("preferred name = %q (present %v), want %q", v, ok, "Jane")
	}

	worker2, err := people.ReadWorker(ctx, reader(t), req)
	if err != nil {
		t.Fatalf("ReadWorker: %v", err)
	}
	if worker2.Disclosure != people.DisclosureFull {
		t.Errorf("worker disclosure = %s, want FULL", worker2.Disclosure)
	}
	if worker2.Presence != people.SubjectPresent {
		t.Errorf("worker presence = %s, want PRESENT", worker2.Presence)
	}
	if worker2.Worker != worker {
		t.Errorf("worker subject = %s, want %s", worker2.Worker, worker)
	}
	if worker2.WorkerNumber.Access != people.AccessAuthorized {
		t.Fatalf("worker_number access = %s, want AUTHORIZED", worker2.WorkerNumber.Access)
	}
	if v, ok := worker2.WorkerNumber.Value.Get(); !ok || v != "W-1001" {
		t.Errorf("worker number = %q (present %v), want %q", v, ok, "W-1001")
	}
	if worker2.LifecycleStatus.Access != people.AccessAuthorized {
		t.Fatalf("lifecycle_status access = %s, want AUTHORIZED", worker2.LifecycleStatus.Access)
	}
	if v, ok := worker2.LifecycleStatus.Value.Get(); !ok || v != "active" {
		t.Errorf("lifecycle status = %q (present %v), want %q", v, ok, "active")
	}

	// The field mask is closed: Person owns exactly the two identity fields
	// and nothing from Employment/Assignment (PEOPLE-002/PEOPLE-003 territory),
	// and there is no FieldID at all for identity-resolution claims, candidate
	// data, or the medical/immigration/payroll/bank compartments - so no
	// projection built from these masks can ever reach them.
	wantPerson := map[people.FieldID]bool{people.FieldLegalName: true, people.FieldPreferredName: true}
	if got := people.PersonFields(); len(got) != len(wantPerson) {
		t.Fatalf("PersonFields = %v, want exactly %v", got, wantPerson)
	} else {
		for _, f := range got {
			if !wantPerson[f] {
				t.Errorf("PersonFields leaked field %s outside the Person mask", f)
			}
		}
	}
	wantWorker := map[people.FieldID]bool{people.FieldWorkerNumber: true, people.FieldLifecycleStatus: true}
	if got := people.WorkerFields(); len(got) != len(wantWorker) {
		t.Fatalf("WorkerFields = %v, want exactly %v", got, wantWorker)
	} else {
		for _, f := range got {
			if !wantWorker[f] {
				t.Errorf("WorkerFields leaked field %s outside the Worker mask", f)
			}
		}
	}
}

// TestTodo_PEOPLE_001_Security is the registry's exact security matrix
// symbol.
//
// It proves the RED clauses: a denied Person/Worker field is reported as
// denied and never as a value, a withheld subject discloses no presence at
// all, and a worker reference from another tenant is refused outright rather
// than silently answered.
func TestTodo_PEOPLE_001_Security(t *testing.T) {
	ctx := context.Background()
	worker := workerRef(t, "jane-doe")
	coordinate := asOf(t)
	allFields := append(append([]people.FieldID{}, people.PersonFields()...), people.WorkerFields()...)
	allow := fixtures.AllowAll(testPolicyVersion, testPurpose, allFields)

	t.Run("denied field is reported denied, never leaked", func(t *testing.T) {
		denied := fixtures.DenyFields(allow, "identity_protected", people.FieldLegalName)
		person, err := people.ReadPerson(ctx, reader(t), people.PersonWorkerReadRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: coordinate, Authorization: denied,
		})
		if err != nil {
			t.Fatalf("ReadPerson: %v", err)
		}
		if person.Disclosure != people.DisclosurePartial {
			t.Errorf("disclosure = %s, want PARTIAL", person.Disclosure)
		}
		if person.LegalName.Access != people.AccessDenied {
			t.Fatalf("legal_name access = %s, want DENIED", person.LegalName.Access)
		}
		if person.LegalName.DenialReason != "identity_protected" {
			t.Errorf("denial reason = %q, want %q", person.LegalName.DenialReason, "identity_protected")
		}
		if _, ok := person.LegalName.Value.Get(); ok {
			t.Error("a denied field must not disclose its value")
		}
		if person.LegalName.Provenance.Validate() == nil {
			t.Error("a denied field must not carry provenance")
		}
		if person.PreferredName.Access != people.AccessAuthorized {
			t.Errorf("preferred_name access = %s, want AUTHORIZED (only legal_name was denied)", person.PreferredName.Access)
		}
	})

	t.Run("withheld subject discloses no presence", func(t *testing.T) {
		withheld := fixtures.WithheldSubject(allow, "not_in_scope")
		person, err := people.ReadPerson(ctx, reader(t), people.PersonWorkerReadRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: coordinate, Authorization: withheld,
		})
		if err != nil {
			t.Fatalf("ReadPerson: %v", err)
		}
		if person.Disclosure != people.DisclosureWithheld {
			t.Errorf("disclosure = %s, want WITHHELD", person.Disclosure)
		}
		if person.Presence != people.SubjectPresenceUnspecified {
			t.Errorf("presence = %s, want UNSPECIFIED", person.Presence)
		}
		if person.WithheldReason != "not_in_scope" {
			t.Errorf("withheld reason = %q, want %q", person.WithheldReason, "not_in_scope")
		}
		if person.LegalName.Access != people.AccessUnspecified || person.PreferredName.Access != people.AccessUnspecified {
			t.Error("a withheld subject must not disclose any field access at all")
		}
		if person.Watermark.IsSpecified() {
			t.Error("a withheld subject must not pin a read watermark")
		}

		worker2, err := people.ReadWorker(ctx, reader(t), people.PersonWorkerReadRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: coordinate, Authorization: withheld,
		})
		if err != nil {
			t.Fatalf("ReadWorker: %v", err)
		}
		if worker2.Disclosure != people.DisclosureWithheld {
			t.Errorf("worker disclosure = %s, want WITHHELD", worker2.Disclosure)
		}
		if worker2.Presence != people.SubjectPresenceUnspecified {
			t.Errorf("worker presence = %s, want UNSPECIFIED", worker2.Presence)
		}
	})

	t.Run("a subject from another tenant is refused, not answered", func(t *testing.T) {
		foreign := values.EntityRef{Tenant: "other-tenant", Kind: people.KindWorker, Id: worker.Id}
		_, err := people.ReadPerson(ctx, reader(t), people.PersonWorkerReadRequest{
			Tenant: fixtures.Tenant, Worker: foreign, AsOf: coordinate, Authorization: allow,
		})
		if !errors.Is(err, people.ErrExplainRequestInvalid) {
			t.Fatalf("cross-tenant read error = %v, want ErrExplainRequestInvalid", err)
		}
	})

	t.Run("every requested field denied still refuses to leak existence", func(t *testing.T) {
		denied := fixtures.DenyFields(allow, "identity_protected", people.FieldLegalName, people.FieldPreferredName)
		person, err := people.ReadPerson(ctx, reader(t), people.PersonWorkerReadRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: coordinate, Authorization: denied,
		})
		if err != nil {
			t.Fatalf("ReadPerson: %v", err)
		}
		if person.Disclosure != people.DisclosurePartial {
			t.Errorf("disclosure = %s, want PARTIAL", person.Disclosure)
		}
		if person.Presence != people.SubjectPresent {
			t.Errorf("presence = %s, want PRESENT (existence is disclosable even though every field is denied)", person.Presence)
		}
		if person.LegalName.Access != people.AccessDenied || person.PreferredName.Access != people.AccessDenied {
			t.Error("both fields must be reported denied, not dropped")
		}
	})
}
