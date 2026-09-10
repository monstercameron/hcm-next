package contact_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func confSubject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func confNow() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

func confService() *contact.UpdateService {
	return contact.NewUpdateService(contact.NewMemoryStore())
}

func confRequest() contact.UpdateRequest {
	subject := confSubject()
	return contact.UpdateRequest{
		Tenant:        "tenant-1",
		Subject:       subject,
		Actor:         subject,
		EndpointID:    "email-1",
		Kind:          contact.EndpointEmail,
		Raw:           "Jane.Doe@Example.COM",
		Purpose:       "payslip",
		Priority:      1,
		Source:        "self-service",
		Op:            contact.OpAdd,
		EffectiveDate: "2026-09-01",
		GuardedFacts:  map[string]string{"work_location": "berlin", "tax_residence": "DE"},
		Now:           confNow(),
	}
}

// TestContactUpdateConformanceRejectsScopeLeakLostSignalAndFalseCompletion
// proves contact updates type their operations, keep subject and actor
// evidence apart, isolate tenants, verify before trusting, revalidate by
// effective date, commit revision and outbox atomically, and never close
// external consistency by queueing.
func TestContactUpdateConformanceRejectsScopeLeakLostSignalAndFalseCompletion(t *testing.T) {
	ctx := context.Background()
	service := confService()

	outcome, err := service.Update(ctx, confRequest())
	if err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	if outcome.Revision.NormalizedValueDigest == "" || outcome.Verified {
		t.Fatalf("fresh endpoint wrong: %+v", outcome.Revision)
	}
	if outcome.Subject != confSubject() || outcome.Actor != confSubject() || outcome.Proxy {
		t.Fatalf("subject/actor evidence wrong: %+v", outcome)
	}
	if len(outcome.Outbox) != 1 || outcome.ExternalSync != contact.SyncPending {
		t.Fatalf("atomic outbox wrong: %+v", outcome)
	}
	if outcome.GuardedFacts["work_location"] != "berlin" || outcome.GuardedFacts["tax_residence"] != "DE" {
		t.Fatalf("guarded facts mutated: %+v", outcome.GuardedFacts)
	}

	adversaries := []struct {
		name   string
		mutate func(*contact.UpdateRequest)
		cause  error
	}{
		{"ambiguous null raw", func(r *contact.UpdateRequest) { r.Raw = "" }, contact.ErrInvalidUpdate},
		{"proxy without authority", func(r *contact.UpdateRequest) {
			r.Actor = values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000002"}
		}, contact.ErrProxyAuthorityMissing},
		{"wrong tenant subject", func(r *contact.UpdateRequest) {
			r.Subject = values.EntityRef{Tenant: "tenant-9", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
		}, contact.ErrTenantMismatch},
		{"duplicate add", func(r *contact.UpdateRequest) {}, contact.ErrDuplicateEndpoint},
		{"revise without fence", func(r *contact.UpdateRequest) { r.Op = contact.OpRevise }, contact.ErrStaleRevision},
		{"approval required", func(r *contact.UpdateRequest) { r.RequireApproval = true }, contact.ErrApprovalRequired},
		{"bad effective date", func(r *contact.UpdateRequest) { r.EffectiveDate = "09/01/2026" }, contact.ErrInvalidUpdate},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			fresh := confService()
			if _, err := fresh.Update(ctx, confRequest()); err != nil {
				t.Fatal(err)
			}
			req := confRequest()
			tc.mutate(&req)
			if _, err := fresh.Update(ctx, req); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
}
