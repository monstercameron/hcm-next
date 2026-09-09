package leave_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/leave"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func request(t *testing.T) leave.RequestLeave {
	t.Helper()
	start, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2026-10-08")
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "business", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return leave.RequestLeave{WorkerID: "worker:abc-123", LeaveType: "medical", Interval: iv, Mode: leave.ModeContinuous, Reason: "planned absence", ExpectedWorkerRevision: "rev:worker:v1", ClientRequestID: "client-1234", EvidenceRefs: []string{"evidence:request-1"}}
}

func TestTodo_LEAVE_001(t *testing.T) {
	r := request(t)
	p, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if p.Family != intent.FamilyChangeRequest || p.DefinitionType != leave.RequestLeaveIntentType || p.DefinitionVersion != 1 {
		t.Fatalf("not a child-bound CHANGE_REQUEST: %+v", p)
	}
	if len(p.ChildKinds) != 8 || p.CanonicalDigest == "" {
		t.Fatalf("incomplete process contract: %+v", p)
	}
	if p.Request.WorkerID != r.WorkerID {
		t.Fatal("binding changed caller payload")
	}
}

func TestTodo_LEAVE_001_Property(t *testing.T) {
	r := request(t)
	a, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	r.EvidenceRefs = []string{"evidence:request-1"}
	b, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalDigest != b.CanonicalDigest {
		t.Fatal("canonical digest is not stable")
	}
	r.ExpectedWorkerRevision = ""
	if err := r.Validate(); !errors.Is(err, leave.ErrInvalidRequest) {
		t.Fatalf("missing revision error = %v", err)
	}
}

func TestTodo_LEAVE_001_Security(t *testing.T) {
	for _, field := range []string{"eligibility", "balance", "manager_id", "legal_context", "tenant_id", "principal_id"} {
		if err := leave.ValidateRawFields(map[string]string{field: "caller-asserted"}); !errors.Is(err, leave.ErrForbiddenField) {
			t.Errorf("%s error = %v", field, err)
		}
	}
}

func TestTodo_LEAVE_001_Conformance(t *testing.T) {
	r := request(t)
	if err := leave.ValidateRawFields(map[string]string{"worker_id": r.WorkerID, "leave_type": r.LeaveType, "mode": string(r.Mode), "reason": r.Reason, "expected_worker_revision": r.ExpectedWorkerRevision, "client_request_id": r.ClientRequestID, "evidence_ref": r.EvidenceRefs[0]}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_LEAVE_001_Golden(t *testing.T) {
	p, err := leave.Bind(request(t), leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	want := "hcmnext.workforce.leave_request"
	if p.DefinitionType != want || p.DefinitionVersion != 1 || p.Family != intent.FamilyChangeRequest {
		t.Fatalf("identity = %q/v%d/%q, want %q/v1/%q", p.DefinitionType, p.DefinitionVersion, p.Family, want, intent.FamilyChangeRequest)
	}
	wantChildren := []leave.ChildKind{leave.ChildLeave, leave.ChildAvailability, leave.ChildBalance, leave.ChildPayroll, leave.ChildBenefits, leave.ChildSchedule, leave.ChildAccess, leave.ChildReturn}
	for i, want := range wantChildren {
		if p.ChildKinds[i] != want {
			t.Fatalf("child %d = %q, want %q", i, p.ChildKinds[i], want)
		}
	}
	if len(p.CanonicalDigest) != 64 {
		t.Fatalf("canonical digest = %q, want sha256 hex", p.CanonicalDigest)
	}
}

func TestTodo_LEAVE_001_Mutation(t *testing.T) {
	r := request(t)
	p, err := leave.Bind(r, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	r.EvidenceRefs[0] = "evidence:caller-mutated"
	if p.Request.EvidenceRefs[0] != "evidence:request-1" {
		t.Fatal("bound process retained caller-owned evidence slice")
	}
	if p.ChildKinds[0] != leave.ChildLeave {
		t.Fatalf("bound child vocabulary changed unexpectedly: %q", p.ChildKinds[0])
	}
}
