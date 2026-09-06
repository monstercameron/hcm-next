package attest

import (
	"context"
	"errors"
	"testing"
	"time"
)

func executionFixture(t *testing.T) (*MemoryResponseStore, Response, ExecutionRequirement) {
	t.Helper()
	store := NewMemoryResponseStore()
	recorder, err := NewRecorder(store, TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil }))
	if err != nil {
		t.Fatal(err)
	}
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	req := ExecutionRequirement{Tenant: response.Tenant, ObligationID: "obligation-1", StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest, ResponseID: response.ResponseID, ResponseRevision: response.Revision, TransactionID: response.TransactionID}
	return store, response, req
}

func TestValidateRequired_RefusesIdentityStateAndTimeBranches(t *testing.T) {
	store, response, req := executionFixture(t)
	valid, err := ValidateRequired(req, response, testTrustedAt)
	if err != nil || !valid.Allowed || valid.Reason != "ATTESTATION_ACCEPTED" || !valid.CheckedAt.At.Equal(testTrustedAt.At) {
		t.Fatalf("valid decision=%+v err=%v", valid, err)
	}
	cases := []struct {
		name           string
		mutateReq      func(*ExecutionRequirement)
		mutateResponse func(*Response)
		want           string
	}{
		{"tenant", func(r *ExecutionRequirement) { r.Tenant = "other" }, nil, "response_identity_mismatch"},
		{"response id", func(r *ExecutionRequirement) { r.ResponseID = "other" }, nil, "response_identity_mismatch"},
		{"revision", func(r *ExecutionRequirement) { r.ResponseRevision++ }, nil, "response_identity_mismatch"},
		{"statement id", func(r *ExecutionRequirement) { r.StatementID = "other" }, nil, "statement_or_binding_mismatch"},
		{"statement version", func(r *ExecutionRequirement) { r.StatementVersion++ }, nil, "statement_or_binding_mismatch"},
		{"statement digest", func(r *ExecutionRequirement) { r.StatementDigest = "other" }, nil, "statement_or_binding_mismatch"},
		{"binding digest", func(r *ExecutionRequirement) { r.BindingDigest = "other" }, nil, "statement_or_binding_mismatch"},
		{"refused", nil, func(r *Response) { r.Status = ResponseRefused }, "response_not_accepted"},
		{"correction", nil, func(r *Response) { r.Kind = AssertionCorrection }, "response_is_not_original_acceptance"},
		{"transaction", func(r *ExecutionRequirement) { r.TransactionID = "other" }, nil, "transaction_mismatch"},
		{"future", nil, func(r *Response) { r.RecordedAt.At = testTrustedAt.At.Add(time.Second) }, "response_recorded_in_future"},
		{"missing old evidence", nil, func(r *Response) { r.RecordedAt.At = testTrustedAt.At.Add(-time.Second); r.RecordedAt.Health = "" }, "response_time_evidence_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rq, resp := req, response
			if tc.mutateReq != nil {
				tc.mutateReq(&rq)
			}
			if tc.mutateResponse != nil {
				tc.mutateResponse(&resp)
			}
			got, err := ValidateRequired(rq, resp, testTrustedAt)
			if err != nil || got.Allowed || got.Reason != tc.want {
				t.Fatalf("decision=%+v err=%v, want refusal %q", got, err, tc.want)
			}
		})
	}

	if _, err := ValidateRequired(req, response, TrustedTime{}); !errors.Is(err, ErrUntrustedTime) {
		t.Fatalf("invalid trusted time err=%v", err)
	}
	for _, mutate := range []func(*ExecutionRequirement){
		func(r *ExecutionRequirement) { r.Tenant = "" }, func(r *ExecutionRequirement) { r.ObligationID = "" }, func(r *ExecutionRequirement) { r.StatementID = "" },
		func(r *ExecutionRequirement) { r.StatementVersion = 0 }, func(r *ExecutionRequirement) { r.StatementDigest = "" }, func(r *ExecutionRequirement) { r.BindingDigest = "" },
		func(r *ExecutionRequirement) { r.ResponseID = "" }, func(r *ExecutionRequirement) { r.ResponseRevision = 0 },
	} {
		rq := req
		mutate(&rq)
		if _, err := ValidateRequired(rq, response, testTrustedAt); err == nil || !errors.Is(err, ErrRequiredAttestation) {
			t.Fatalf("incomplete requirement err=%v", err)
		}
	}

	_ = store
}

func TestRevalidateAndEnforceRequired_StoreClockAndAliasErrors(t *testing.T) {
	store, response, req := executionFixture(t)
	clock := TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil })
	for name, fn := range map[string]func() (ExecutionDecision, error){
		"revalidate": func() (ExecutionDecision, error) {
			return RevalidateAtExecution(context.Background(), store, clock, req)
		},
		"enforce": func() (ExecutionDecision, error) { return EnforceRequired(context.Background(), store, clock, req) },
	} {
		decision, err := fn()
		if err != nil || !decision.Allowed || decision.ResponseDigest != response.Digest {
			t.Errorf("%s decision=%+v err=%v", name, decision, err)
		}
	}
	if _, err := RevalidateAtExecution(context.Background(), nil, clock, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("nil store err=%v", err)
	}
	if _, err := RevalidateAtExecution(context.Background(), store, nil, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("nil clock err=%v", err)
	}
	if _, err := RevalidateAtExecution(context.Background(), store, clock, ExecutionRequirement{Tenant: req.Tenant, ResponseID: "missing", ResponseRevision: 1}); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("lookup err=%v", err)
	}
	failingClock := TrustedClockFunc(func() (TrustedTime, error) { return TrustedTime{}, errors.New("clock unavailable") })
	if _, err := RevalidateAtExecution(context.Background(), store, failingClock, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("clock failure err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RevalidateAtExecution(ctx, store, clock, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("store context failure err=%v", err)
	}
	if _, err := RevalidateAtExecution(context.Background(), store, clock, req); err != nil {
		t.Fatalf("repeat valid revalidation: %v", err)
	}
}

func TestFixedTrustedTimeAndClockAdapter(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 123, time.FixedZone("x", 3600))
	got := FixedTrustedTime(at)
	if got.Source != "conformance" || got.EvidenceID == "" || got.Health != "TRUSTED" || !got.At.Equal(at.UTC()) {
		t.Fatalf("FixedTrustedTime=%+v", got)
	}
	called := false
	clock := TrustedClockFunc(func() (TrustedTime, error) { called = true; return got, nil })
	if _, err := clock.TrustedNow(); err != nil || !called {
		t.Fatalf("TrustedClockFunc err=%v called=%v", err, called)
	}
}
