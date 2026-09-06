package authorizedhealth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/operations/admin"
	"github.com/monstercameron/hcm-next/internal/operations/authorizedhealth"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

var now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func operator(t *testing.T, roles ...string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "acme-corp", Subject: "operator-1", SubjectKind: trust.SubjectKindHuman, Roles: append([]string{admin.OperatorRole}, roles...), Purposes: []string{"operator_diagnostics"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-1", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func request() authorizedhealth.Request {
	return authorizedhealth.Request{
		Tenant: "acme-corp", Now: now,
		Projections:    []authorizedhealth.ProjectionObservation{{Name: "worker-summary", SchemaVersion: "v1", DefinitionVersion: "worker-summary-v2", SourceSequence: 42, AppliedSequence: 42, ObservedAt: now.Add(-time.Minute), MaxAge: 5 * time.Minute, Status: "CURRENT"}},
		Workflows:      []authorizedhealth.WorkflowObservation{{Name: "promotion", SchemaVersion: "v1", DefinitionVersion: "promotion-v3", ActiveRuns: 2, PendingWorkItems: 1, ObservedAt: now.Add(-time.Minute), MaxAge: 5 * time.Minute, Status: "HEALTHY"}},
		Outbox:         &authorizedhealth.OutboxObservation{SchemaVersion: "v1", Pending: 1, ObservedAt: now.Add(-time.Minute), OldestPendingAt: now.Add(-time.Minute), MaxAge: 5 * time.Minute, MaxPendingAge: 5 * time.Minute, Status: "HEALTHY"},
		Reconciliation: &authorizedhealth.ReconciliationObservation{SchemaVersion: "v1", ObservedAt: now.Add(-time.Minute), MaxAge: 5 * time.Minute, Status: "HEALTHY"},
	}
}

func TestTodo_ALIGN_049(t *testing.T) {
	got, err := authorizedhealth.Project(operator(t), request())
	if err != nil || got.State != authorizedhealth.StateHealthy || len(got.Projections) != 1 || got.Projections[0].Freshness != authorizedhealth.FreshnessCurrent {
		t.Fatalf("freshness view=%+v err=%v", got, err)
	}
}

func TestTodo_ALIGN_049_Property(t *testing.T) {
	for _, lag := range []int64{0, 1, 100} {
		r := request()
		r.Projections[0].SourceSequence = 100
		r.Projections[0].AppliedSequence = r.Projections[0].SourceSequence - lag
		got, err := authorizedhealth.Project(operator(t), r)
		if err != nil {
			t.Fatal(err)
		}
		if got.Projections[0].Lag != lag {
			t.Fatalf("lag=%d got=%d", lag, got.Projections[0].Lag)
		}
		if lag == 0 && got.Projections[0].State != authorizedhealth.StateHealthy {
			t.Fatal("zero lag must be healthy")
		}
		if lag > 0 && got.Projections[0].State != authorizedhealth.StateDegraded {
			t.Fatal("positive lag must be degraded")
		}
	}
}

func TestTodo_ALIGN_049_Golden(t *testing.T) {
	got, err := authorizedhealth.Project(operator(t), request())
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyVersion != "hcmnext.admin.operator-profile/1" || got.Purpose != "operator_diagnostics" || got.EvidenceID == "" || got.Digest() == "" {
		t.Fatalf("operator metadata=%+v", got)
	}
	if strings.Contains(got.Explain(), "acme-corp") || strings.Contains(got.Explain(), "operator-1") {
		t.Fatal("operator explanation leaked identity")
	}
}

func TestTodo_ALIGN_049_Security(t *testing.T) {
	ordinary, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "acme-corp", Subject: "worker-1", SubjectKind: trust.SubjectKindHuman, Roles: []string{"worker_self"}, Purposes: []string{"self_service"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-2", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizedhealth.Project(ordinary, request()); !errors.Is(err, authorizedhealth.ErrUnauthorized) {
		t.Fatalf("ordinary error=%v", err)
	}
	other := request()
	other.Tenant = values.TenantId("other-corp")
	if _, err := authorizedhealth.Project(operator(t), other); !errors.Is(err, authorizedhealth.ErrUnauthorized) {
		t.Fatalf("cross-tenant error=%v", err)
	}
}

func TestTodo_ALIGN_049_Integration(t *testing.T) {
	r := request()
	r.Projections = append(r.Projections, authorizedhealth.ProjectionObservation{Name: "assignment-summary", SchemaVersion: "v1", DefinitionVersion: "assignment-v1", SourceSequence: 7, AppliedSequence: 7, ObservedAt: now.Add(-30 * time.Second), MaxAge: time.Minute, Status: "CURRENT"})
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Projections) != 2 || got.Projections[0].Name != "assignment-summary" || got.Projections[1].Name != "worker-summary" || got.Projections[0].Lag != 0 || got.State != authorizedhealth.StateHealthy {
		t.Fatalf("integrated projection=%+v state=%s", got.Projections, got.State)
	}
}
func TestTodo_ALIGN_049_Fault(t *testing.T) {
	r := request()
	r.Projections[0].ObservedAt = time.Time{}
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Projections[0].Freshness != authorizedhealth.FreshnessUnknown || got.State != authorizedhealth.StateUnknown {
		t.Fatalf("missing evidence=%+v", got)
	}
}
func TestTodo_ALIGN_049_Conformance(t *testing.T) {
	if authorizedhealth.Version() != 1 || authorizedhealth.Explain() == "" {
		t.Fatal("health contract incomplete")
	}
}

func TestTodo_ALIGN_050(t *testing.T) {
	r := request()
	r.Workflows[0].FailedRuns = 1
	r.Workflows[0].OverdueWorkItems = 2
	r.Workflows[0].Status = "DEGRADED"
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil || got.State != authorizedhealth.StateDegraded || got.Workflows[0].FailedRuns != 1 || got.Workflows[0].OverdueWorkItems != 2 {
		t.Fatalf("workflow health=%+v err=%v", got, err)
	}
}
func TestTodo_ALIGN_050_Property(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorizedhealth.WorkflowObservation)
		want   authorizedhealth.State
	}{
		{"failed run", func(w *authorizedhealth.WorkflowObservation) { w.FailedRuns = 1 }, authorizedhealth.StateDegraded},
		{"blocked run", func(w *authorizedhealth.WorkflowObservation) { w.BlockedRuns = 1 }, authorizedhealth.StateDegraded},
		{"overdue work", func(w *authorizedhealth.WorkflowObservation) { w.OverdueWorkItems = 1 }, authorizedhealth.StateDegraded},
		{"healthy counts", func(*authorizedhealth.WorkflowObservation) {}, authorizedhealth.StateHealthy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request()
			tc.mutate(&r.Workflows[0])
			got, err := authorizedhealth.Project(operator(t), r)
			if err != nil || got.Workflows[0].State != tc.want {
				t.Fatalf("workflow=%+v err=%v", got.Workflows[0], err)
			}
		})
	}
}
func TestTodo_ALIGN_050_Golden(t *testing.T) {
	r := request()
	r.Workflows[0].ActiveRuns = 4
	r.Workflows[0].PendingWorkItems = 7
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	w := got.Workflows[0]
	if w.Name != "promotion" || w.SchemaVersion != "v1" || w.DefinitionVersion != "promotion-v3" || w.ActiveRuns != 4 || w.PendingWorkItems != 7 || w.Freshness != authorizedhealth.FreshnessCurrent || w.Status != "HEALTHY" {
		t.Fatalf("workflow golden=%+v", w)
	}
}
func TestTodo_ALIGN_050_Security(t *testing.T) {
	r := request()
	r.Workflows[0].PendingWorkItems = -1
	if _, err := authorizedhealth.Project(operator(t), r); !errors.Is(err, authorizedhealth.ErrInvalidInput) {
		t.Fatalf("negative work item count error=%v", err)
	}
}
func TestTodo_ALIGN_050_Integration(t *testing.T) {
	r := request()
	r.Workflows = append(r.Workflows, authorizedhealth.WorkflowObservation{Name: "approval", SchemaVersion: "v1", DefinitionVersion: "approval-v1", ObservedAt: now.Add(-time.Minute), MaxAge: time.Minute, Status: "HEALTHY"})
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Workflows) != 2 || got.Workflows[0].Name != "approval" || got.Workflows[1].Name != "promotion" || got.State != authorizedhealth.StateHealthy {
		t.Fatalf("combined workflow projection=%+v state=%s", got.Workflows, got.State)
	}
}
func TestTodo_ALIGN_050_Fault(t *testing.T) {
	r := request()
	r.Workflows[0].MaxAge = 0
	if _, err := authorizedhealth.Project(operator(t), r); !errors.Is(err, authorizedhealth.ErrInvalidInput) {
		t.Fatalf("missing workflow freshness bound error=%v", err)
	}
}
func TestTodo_ALIGN_050_Conformance(t *testing.T) {
	r := request()
	r.Workflows[0].ActiveRuns = -1
	if _, err := authorizedhealth.Project(operator(t), r); !errors.Is(err, authorizedhealth.ErrInvalidInput) {
		t.Fatalf("negative active runs error=%v", err)
	}
}

func TestTodo_ALIGN_051(t *testing.T) {
	r := request()
	r.Outbox.Failed = 1
	r.Outbox.Abandoned = 1
	r.Reconciliation.RepairRequired = 1
	r.Reconciliation.Status = "DEGRADED"
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil || got.State != authorizedhealth.StateDegraded || got.Outbox.Failed != 1 || got.Outbox.Abandoned != 1 || got.Reconciliation.RepairRequired != 1 {
		t.Fatalf("outbox/reconciliation health=%+v err=%v", got, err)
	}
}
func TestTodo_ALIGN_051_Property(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorizedhealth.OutboxObservation, *authorizedhealth.ReconciliationObservation)
		want   authorizedhealth.State
	}{
		{"pending age breach", func(o *authorizedhealth.OutboxObservation, _ *authorizedhealth.ReconciliationObservation) {
			o.OldestPendingAt = now.Add(-6 * time.Minute)
		}, authorizedhealth.StateDegraded},
		{"mismatch", func(_ *authorizedhealth.OutboxObservation, r *authorizedhealth.ReconciliationObservation) {
			r.Mismatch = 1
		}, authorizedhealth.StateDegraded},
		{"repair required", func(_ *authorizedhealth.OutboxObservation, r *authorizedhealth.ReconciliationObservation) {
			r.RepairRequired = 1
		}, authorizedhealth.StateDegraded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request()
			tc.mutate(r.Outbox, r.Reconciliation)
			got, err := authorizedhealth.Project(operator(t), r)
			if err != nil || got.State != tc.want {
				t.Fatalf("view=%+v err=%v", got, err)
			}
		})
	}
}
func TestTodo_ALIGN_051_Golden(t *testing.T) {
	r := request()
	r.Outbox.Pending = 2
	r.Outbox.OldestPendingAt = now.Add(-2 * time.Minute)
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outbox == nil || got.Outbox.Pending != 2 || got.Outbox.OldestPendingAge != 2*time.Minute || got.Outbox.Freshness != authorizedhealth.FreshnessCurrent || got.Outbox.State != authorizedhealth.StateHealthy {
		t.Fatalf("outbox golden=%+v", got.Outbox)
	}
}
func TestTodo_ALIGN_051_Security(t *testing.T) {
	r := request()
	r.Outbox.Pending = 1
	r.Outbox.OldestPendingAt = time.Time{}
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outbox.Freshness != authorizedhealth.FreshnessUnknown || got.State != authorizedhealth.StateUnknown {
		t.Fatalf("missing outbox evidence=%+v", got)
	}
}
func TestTodo_ALIGN_051_Integration(t *testing.T) {
	r := request()
	r.Outbox.ObservedAt = now.Add(-10 * time.Minute)
	r.Reconciliation.RepairRequired = 1
	got, err := authorizedhealth.Project(operator(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outbox.Freshness != authorizedhealth.FreshnessStale || got.Reconciliation.State != authorizedhealth.StateDegraded || got.State != authorizedhealth.StateDegraded {
		t.Fatalf("combined delivery/reconciliation view=%+v state=%s", got, got.State)
	}
}
func TestTodo_ALIGN_051_Fault(t *testing.T) {
	r := request()
	r.Outbox.Failed = -1
	if _, err := authorizedhealth.Project(operator(t), r); !errors.Is(err, authorizedhealth.ErrInvalidInput) {
		t.Fatalf("negative outbox count error=%v", err)
	}
}
func TestTodo_ALIGN_051_Conformance(t *testing.T) {
	if _, err := authorizedhealth.Project(nil, request()); !errors.Is(err, authorizedhealth.ErrUnauthorized) {
		t.Fatalf("nil principal error=%v", err)
	}
}

func authorizedDecision(t *testing.T) (authz.Decision, *trust.Principal) {
	t.Helper()
	p := operator(t, string(authz.RoleAuditor))
	instant := values.NewInstant(now)
	decision, err := authz.Enforce(authz.Request{Principal: p, Purpose: "operator_diagnostics", EffectiveAt: instant, Subject: values.EntityRef{Tenant: p.Tenant(), Kind: "projection", Id: "00000000-0000-0000-0000-000000000001"}, Fields: []authz.FieldID{authz.FieldWorkerNumber}})
	if err != nil {
		t.Fatal(err)
	}
	return decision, p
}

func TestAuthorizedEnvelopeAndInvalidationAreBounded(t *testing.T) {
	decision, p := authorizedDecision(t)
	envelope, err := authorizedhealth.NewQueryEnvelope(p, decision, authorizedhealth.QueryRequest{Purpose: "operator_diagnostics", PolicyVersion: "authz.p1a.bootstrap.v1", SchemaVersion: "projection.v1", DefinitionVersion: "projection.v2", SourceWatermark: "stream:42", Freshness: authorizedhealth.FreshnessCurrent, RowCount: 1})
	if err != nil || envelope.AuthorizationDigest == "" {
		t.Fatalf("envelope=%+v err=%v", envelope, err)
	}
	message, err := authorizedhealth.NewInvalidation(p, decision, authorizedhealth.InvalidationInput{ResourceKind: "projection", ResourceID: "projection-1", Projection: "worker-summary", SchemaVersion: "v1", DefinitionVersion: "v2", SourceWatermark: "stream:42"})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := message.MarshalBounded()
	if err != nil || len(wire) > 1024 {
		t.Fatalf("wire len=%d err=%v", len(wire), err)
	}
	if err := authorizedhealth.CheckNoninterference([]authorizedhealth.SurfaceObservation{{ErrorClass: "NOT_FOUND_OR_FORBIDDEN", Reason: "not_disclosable"}, {ErrorClass: "NOT_FOUND_OR_FORBIDDEN", Reason: "not_disclosable"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizedHealthProjectRejectsMalformedEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*authorizedhealth.Request)
	}{
		{"missing observation time", func(r *authorizedhealth.Request) { r.Now = time.Time{} }},
		{"missing projection identity", func(r *authorizedhealth.Request) { r.Projections[0].Name = " " }},
		{"invalid projection watermark", func(r *authorizedhealth.Request) {
			r.Projections[0].AppliedSequence = r.Projections[0].SourceSequence + 1
		}},
		{"invalid workflow count", func(r *authorizedhealth.Request) { r.Workflows[0].BlockedRuns = -1 }},
		{"invalid outbox metadata", func(r *authorizedhealth.Request) { r.Outbox.Status = "" }},
		{"invalid reconciliation count", func(r *authorizedhealth.Request) { r.Reconciliation.Mismatch = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := request()
			tc.mutate(&r)
			if _, err := authorizedhealth.Project(operator(t), r); !errors.Is(err, authorizedhealth.ErrInvalidInput) {
				t.Fatalf("Project error=%v", err)
			}
		})
	}
}

func TestAuthorizedHealthEnvelopeAndInvalidationRejectForgedOrIncompleteDecisions(t *testing.T) {
	decision, p := authorizedDecision(t)
	envelopeRequest := authorizedhealth.QueryRequest{Purpose: "operator_diagnostics", PolicyVersion: "authz.p1a.bootstrap.v1", SchemaVersion: "projection.v1", DefinitionVersion: "projection.v2", SourceWatermark: "stream:42", Freshness: authorizedhealth.FreshnessCurrent}
	for _, tc := range []struct {
		name   string
		mutate func(*authz.Decision, *authorizedhealth.QueryRequest)
	}{
		{"wrong tenant", func(d *authz.Decision, _ *authorizedhealth.QueryRequest) { d.Scope.Subject.Tenant = "other-corp" }},
		{"missing evidence", func(d *authz.Decision, _ *authorizedhealth.QueryRequest) { d.InputsDigest = "" }},
		{"invalid request freshness", func(_ *authz.Decision, r *authorizedhealth.QueryRequest) {
			r.Freshness = authorizedhealth.Freshness("BROKEN")
		}},
		{"negative row count", func(_ *authz.Decision, r *authorizedhealth.QueryRequest) { r.RowCount = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := decision
			r := envelopeRequest
			tc.mutate(&d, &r)
			if _, err := authorizedhealth.NewQueryEnvelope(p, d, r); !errors.Is(err, authorizedhealth.ErrUnauthorized) {
				t.Fatalf("NewQueryEnvelope error=%v", err)
			}
		})
	}
	in := authorizedhealth.InvalidationInput{ResourceKind: "projection", ResourceID: "projection-1", Projection: "worker-summary", SchemaVersion: "v1", DefinitionVersion: "v2", SourceWatermark: "stream:42"}
	for _, tc := range []struct {
		name   string
		mutate func(*authz.Decision, *authorizedhealth.InvalidationInput)
	}{
		{"wrong effect", func(d *authz.Decision, _ *authorizedhealth.InvalidationInput) { d.Scope.Effect = authz.EffectDenied }},
		{"wrong tenant", func(d *authz.Decision, _ *authorizedhealth.InvalidationInput) { d.Scope.Subject.Tenant = "other-corp" }},
		{"malformed token", func(_ *authz.Decision, i *authorizedhealth.InvalidationInput) { i.ResourceID = "line\nbreak" }},
		{"missing decision evidence", func(d *authz.Decision, _ *authorizedhealth.InvalidationInput) { d.EvidenceID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := decision
			i := in
			tc.mutate(&d, &i)
			_, err := authorizedhealth.NewInvalidation(p, d, i)
			want := authorizedhealth.ErrUnauthorized
			if tc.name == "malformed token" {
				want = authorizedhealth.ErrInvalidInput
			}
			if !errors.Is(err, want) {
				t.Fatalf("NewInvalidation error=%v want=%v", err, want)
			}
		})
	}
}

func TestAuthorizedHealthMarshalAndNoninterferenceRejectBoundsAndDisclosure(t *testing.T) {
	large := strings.Repeat("x", 512)
	if _, err := (authorizedhealth.Invalidation{Tenant: "acme-corp", ResourceKind: large, ResourceID: large, Projection: large, SchemaVersion: large, DefinitionVersion: large, SourceWatermark: large}).MarshalBounded(); !errors.Is(err, authorizedhealth.ErrInvalidInput) {
		t.Fatalf("oversized invalidation error=%v", err)
	}
	cases := []authorizedhealth.SurfaceObservation{
		{Visible: false, ErrorClass: "NOT_FOUND_OR_FORBIDDEN", Reason: "same", Count: 1},
		{Visible: false, ErrorClass: "", Reason: "same"},
		{Visible: false, ErrorClass: "NOT_FOUND_OR_FORBIDDEN", Reason: "same", Payload: []byte("secret")},
	}
	for i, observation := range cases {
		if err := authorizedhealth.CheckNoninterference([]authorizedhealth.SurfaceObservation{observation}); err == nil {
			t.Fatalf("case %d disclosed or incomplete observation was accepted", i)
		}
	}
	if err := authorizedhealth.CheckNoninterference([]authorizedhealth.SurfaceObservation{{Visible: true, Count: 4, Payload: []byte("allowed")}}); err != nil {
		t.Fatalf("visible observation should be ignored: %v", err)
	}
	if err := authorizedhealth.CheckNoninterference([]authorizedhealth.SurfaceObservation{{ErrorClass: "a", Reason: "r"}, {ErrorClass: "b", Reason: "r"}}); err == nil {
		t.Fatal("distinguishable denied observations were accepted")
	}
}
