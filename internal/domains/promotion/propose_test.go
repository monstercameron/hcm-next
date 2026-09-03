package promotion

import (
	"context"
	"strings"
	"testing"
)

type stubGateway struct {
	invoked int
	lastCap string
	fail    error
}

func (s *stubGateway) Invoke(ctx context.Context, cap string, ver int, payload any, p ProposePrincipal) (any, error) {
	s.invoked++
	s.lastCap = cap
	if s.fail != nil {
		return nil, s.fail
	}
	return "ok", nil
}

type stubIntent struct {
	called int
}

func (s *stubIntent) CreateIntent(ctx context.Context, req PromotionProposeRequest, p ProposePrincipal) (string, string, error) {
	s.called++
	return "intent-" + req.ClientRequestID, "proposal-" + req.ClientRequestID, nil
}

type stubPreflight struct {
	called int
}

func (s *stubPreflight) RunPreflight(ctx context.Context, req PromotionProposeRequest, intentID string) error {
	s.called++
	return nil
}

type stubEvidence struct {
	called int
}

func (s *stubEvidence) Record(ctx context.Context, e ProposeEvidence) (string, error) {
	s.called++
	return "ev-" + e.IntentID, nil
}

type countingPreflight struct {
	mutations int
}

func (c *countingPreflight) RunPreflight(ctx context.Context, req PromotionProposeRequest, intentID string) error {
	return nil
}

func validReq() PromotionProposeRequest {
	return PromotionProposeRequest{
		WorkerID:                "worker:abc-123",
		JobCode:                 "ENG-2",
		Grade:                   "G5",
		OrgUnit:                 "org-1",
		PositionID:              "pos-1",
		PayZone:                 "zone-a",
		ManagerID:               "worker:mgr-1",
		CompensationAmount:      "95000",
		CompensationCurrency:    "USD",
		CompensationPayBasis:    "SALARY",
		EffectiveDate:           "2026-09-01",
		Reason:                  "promotion",
		ExpectedSubjectRevision: "rev-42",
		ClientRequestID:         "client-req-12345678",
	}
}

func validPrincipal() ProposePrincipal {
	return ProposePrincipal{TenantID: "tenant-1", SubjectID: "user-1", SubjectKind: "human", Scopes: []string{"people.promote.propose"}}
}

func TestTodo_PROMO_007(t *testing.T) {
	gw := &stubGateway{}
	ic := &stubIntent{}
	pf := &stubPreflight{}
	ev := &stubEvidence{}
	svc := &ProposeService{Gateway: gw, Intents: ic, Preflight: pf, Evidence: ev}
	t.Run("GREEN_valid_propose_creates_intent_with_zero_mutation", func(t *testing.T) {
		req := validReq()
		resp, err := svc.Propose(context.Background(), req, validPrincipal())
		if err != nil {
			t.Fatalf("Propose %v", err)
		}
		if !resp.ZeroMutation {
			t.Fatalf("ZeroMutation false")
		}
		if resp.IntentID == "" || resp.ProposalID == "" || resp.EvidenceID == "" || resp.Digest == "" {
			t.Fatalf("missing ids %+v", resp)
		}
		if resp.CapabilityID != CapPromotionProposeID || resp.IntentType != IntentType {
			t.Fatalf("capability/intent mismatch %+v", resp)
		}
		if gw.invoked != 1 || ic.called != 1 || pf.called != 1 || ev.called != 1 {
			t.Fatalf("gateway %d intent %d preflight %d evidence %d", gw.invoked, ic.called, pf.called, ev.called)
		}
	})
	t.Run("RED_forbidden_field_rejected", func(t *testing.T) {
		raw := map[string]string{
			"worker_id": "worker:abc-123", "job_code": "ENG-2", "effective_date": "2026-09-01",
			"reason": "promotion", "expected_subject_revision": "rev-1", "client_request_id": "client-req-12345678",
			"current_salary": "100000",
		}
		if err := ValidateRawProposeFields(raw); err == nil || !strings.Contains(err.Error(), RejectionCodePromo007) {
			t.Fatalf("want PROMO_007_REJECTED for forbidden field, got %v", err)
		}
	})
	t.Run("RED_missing_expected_revision_rejected", func(t *testing.T) {
		req := validReq()
		req.ExpectedSubjectRevision = ""
		if _, err := svc.Propose(context.Background(), req, validPrincipal()); err == nil {
			t.Fatal("want error for missing revision")
		}
	})
	t.Run("RED_missing_client_request_id_rejected", func(t *testing.T) {
		req := validReq()
		req.ClientRequestID = ""
		if _, err := svc.Propose(context.Background(), req, validPrincipal()); err == nil {
			t.Fatal("want error for missing client_request_id")
		}
	})
	t.Run("RED_principal_tenant_not_in_payload", func(t *testing.T) {
		raw := map[string]string{
			"worker_id": "worker:abc-123", "job_code": "ENG-2", "effective_date": "2026-09-01",
			"reason": "promotion", "expected_subject_revision": "rev-1", "client_request_id": "client-req-12345678",
			"tenant_id": "tenant-evil",
		}
		if err := ValidateRawProposeFields(raw); err == nil {
			t.Fatal("want rejection for tenant in payload")
		}
		raw2 := map[string]string{
			"worker_id": "worker:abc-123", "job_code": "ENG-2", "effective_date": "2026-09-01",
			"reason": "promotion", "expected_subject_revision": "rev-1", "client_request_id": "client-req-12345678",
			"principal": "evil",
		}
		if err := ValidateRawProposeFields(raw2); err == nil {
			t.Fatal("want rejection for principal in payload")
		}
	})
	t.Run("RED_unauthorized_scope_denied", func(t *testing.T) {
		req := validReq()
		p := validPrincipal()
		p.Scopes = []string{"other.scope"}
		if _, err := svc.Propose(context.Background(), req, p); err == nil {
			t.Fatal("want authz failure")
		}
	})
}

func TestTodo_PROMO_007_Golden(t *testing.T) {
	req := validReq()
	d1 := DigestPromotionPropose(req)
	d2 := DigestPromotionPropose(req)
	if d1 != d2 {
		t.Fatalf("digest not deterministic %s vs %s", d1, d2)
	}
	req2 := req
	req2.JobCode = "ENG-3"
	d3 := DigestPromotionPropose(req2)
	if d1 == d3 {
		t.Fatal("different request produced same digest")
	}
	grpcReq, err := ParseProposeFromGRPC(map[string]string{
		"worker_id": "worker:abc-123", "job_code": "ENG-2", "grade": "G5", "org_unit": "org-1", "position_id": "pos-1", "pay_zone": "zone-a",
		"manager_id": "worker:mgr-1", "compensation_amount": "95000", "compensation_currency": "USD", "compensation_pay_basis": "SALARY",
		"effective_date": "2026-09-01", "reason": "promotion", "expected_subject_revision": "rev-42", "client_request_id": "client-req-12345678",
	})
	if err != nil {
		t.Fatalf("grpc parse %v", err)
	}
	httpReq, err := ParseProposeFromHTTP(map[string]string{
		"Worker_ID": "worker:abc-123", "Job_Code": "ENG-2", "Grade": "G5", "Org_Unit": "org-1", "Position_ID": "pos-1", "Pay_Zone": "zone-a",
		"Manager_ID": "worker:mgr-1", "Compensation_Amount": "95000", "Compensation_Currency": "USD", "Compensation_Pay_Basis": "SALARY",
		"Effective_Date": "2026-09-01", "Reason": "promotion", "Expected_Subject_Revision": "rev-42", "Client_Request_ID": "client-req-12345678",
	})
	if err != nil {
		t.Fatalf("http parse %v", err)
	}
	if DigestPromotionPropose(grpcReq) != DigestPromotionPropose(httpReq) {
		t.Fatal("grpc and http digests differ")
	}
	if grpcReq != httpReq {
		t.Fatalf("grpc vs http parsed structs differ %+v vs %+v", grpcReq, httpReq)
	}
}

func TestTodo_PROMO_007_Integration(t *testing.T) {
	gw := &stubGateway{}
	ic := &stubIntent{}
	pf := &stubPreflight{}
	ev := &stubEvidence{}
	svc := &ProposeService{Gateway: gw, Intents: ic, Preflight: pf, Evidence: ev}
	req := validReq()
	resp, err := svc.Propose(context.Background(), req, validPrincipal())
	if err != nil {
		t.Fatalf("integration propose %v", err)
	}
	if resp.Digest != DigestPromotionPropose(req) {
		t.Fatalf("digest mismatch %s vs %s", resp.Digest, DigestPromotionPropose(req))
	}
	if gw.lastCap != CapPromotionProposeID {
		t.Fatalf("gateway cap %q", gw.lastCap)
	}
	resp2, err := svc.Propose(context.Background(), req, validPrincipal())
	if err != nil {
		t.Fatalf("second propose %v", err)
	}
	if resp.IntentID != resp2.IntentID {
		t.Fatalf("idempotency broken %s vs %s", resp.IntentID, resp2.IntentID)
	}
}

func TestTodo_PROMO_007_Security(t *testing.T) {
	svc := &ProposeService{Gateway: &stubGateway{}, Intents: &stubIntent{}, Preflight: &stubPreflight{}, Evidence: &stubEvidence{}}
	t.Run("authority_fields_rejected", func(t *testing.T) {
		for _, k := range []string{"current_salary", "current_manager", "budget_authority", "authority", "vacancy"} {
			raw := map[string]string{
				"worker_id": "worker:abc-123", "job_code": "ENG-2", "effective_date": "2026-09-01",
				"reason": "promotion", "expected_subject_revision": "rev-1", "client_request_id": "client-req-12345678",
				k: "evil",
			}
			if err := ValidateRawProposeFields(raw); err == nil {
				t.Fatalf("field %q not rejected", k)
			}
		}
	})
	t.Run("missing_tenant_in_principal_rejected", func(t *testing.T) {
		req := validReq()
		p := validPrincipal()
		p.TenantID = ""
		if _, err := svc.Propose(context.Background(), req, p); err == nil {
			t.Fatal("want tenant injection failure")
		}
	})
	t.Run("extension_fields_rejected", func(t *testing.T) {
		raw := map[string]string{
			"worker_id": "worker:abc-123", "job_code": "ENG-2", "effective_date": "2026-09-01",
			"reason": "promotion", "expected_subject_revision": "rev-1", "client_request_id": "client-req-12345678",
			"extra_unknown": "payload",
		}
		if err := ValidateRawProposeFields(raw); err == nil {
			t.Fatal("want rejection for unknown extension field")
		}
	})
}

func TestTodo_PROMO_007_Mutation(t *testing.T) {
	req := validReq()
	base := DigestPromotionPropose(req)
	mutated := req
	mutated.Reason = "different"
	if DigestPromotionPropose(mutated) == base {
		t.Fatal("mutation did not change digest")
	}
	mutated2 := req
	mutated2.ClientRequestID = "client-req-99999999"
	if DigestPromotionPropose(mutated2) == base {
		t.Fatal("client_request_id mutation did not change digest")
	}
	svc := &ProposeService{Gateway: &stubGateway{}, Intents: &stubIntent{}, Preflight: &stubPreflight{}, Evidence: &stubEvidence{}}
	resp, err := svc.Propose(context.Background(), req, validPrincipal())
	if err != nil {
		t.Fatalf("propose %v", err)
	}
	if !resp.ZeroMutation {
		t.Fatal("mutation leaked: ZeroMutation false")
	}
}
