package attest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/timeauth"
)

func TestResponseEnumsAndTrustedTimeValidation(t *testing.T) {
	for _, status := range []ResponseStatus{ResponseAccepted, ResponseRefused, ResponseUnknown} {
		if !status.Valid() {
			t.Errorf("status %q invalid", status)
		}
	}
	if ResponseStatus("BAD").Valid() {
		t.Fatal("unknown status accepted")
	}
	for _, kind := range []AssertionKind{AssertionResponse, AssertionCorrection, AssertionRevocation} {
		if !kind.Valid() {
			t.Errorf("kind %q invalid", kind)
		}
	}
	if AssertionKind("BAD").Valid() {
		t.Fatal("unknown kind accepted")
	}
	valid := testTrustedAt
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid trusted time: %v", err)
	}
	for _, bad := range []TrustedTime{{}, {At: valid.At, Source: "", EvidenceID: "id", Health: "TRUSTED"}, {At: valid.At, Source: "source", EvidenceID: "", Health: "TRUSTED"}, {At: valid.At, Source: "source", EvidenceID: "id", Health: ""}} {
		if err := bad.Validate(); !errors.Is(err, ErrUntrustedTime) {
			t.Fatalf("bad trusted time=%+v err=%v", bad, err)
		}
	}
	negative := valid
	negative.Uncertainty = -time.Nanosecond
	if err := negative.Validate(); !errors.Is(err, ErrUntrustedTime) {
		t.Fatalf("negative uncertainty err=%v", err)
	}
	called := false
	if _, err := (TrustedClockFunc(func() (TrustedTime, error) { called = true; return valid, nil })).TrustedNow(); err != nil || !called {
		t.Fatalf("clock adapter err=%v called=%v", err, called)
	}

	if _, err := (TimeAuthClock{}).TrustedNow(); !errors.Is(err, ErrUntrustedTime) {
		t.Fatalf("nil monitor err=%v", err)
	}
	fake := timeauth.NewFakeClock("response-test", valid.At)
	monitor, err := timeauth.NewMonitor(fake, timeauth.Options{})
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := (TimeAuthClock{Monitor: monitor}).TrustedNow()
	if err != nil || trusted.Source != "response-test" || trusted.Health != "TRUSTED" || trusted.EvidenceID == "" {
		t.Fatalf("monitor clock=%+v err=%v", trusted, err)
	}
}

func TestValidateResponseDraftAndResponse_ContractBranches(t *testing.T) {
	valid := testResponseRequest(ResponseAccepted)
	if err := ValidateResponseDraft(Response{Tenant: valid.Tenant, ResponseID: valid.ResponseID, StatementID: valid.StatementID, StatementVersion: valid.StatementVersion, StatementDigest: valid.StatementDigest, BindingDigest: valid.BindingDigest, Status: valid.Status, Kind: AssertionResponse, EvidenceReceipt: valid.EvidenceReceipt, IdempotencyKey: valid.IdempotencyKey}); err != nil {
		t.Fatalf("valid draft: %v", err)
	}
	base := valid
	mutations := []struct {
		name   string
		mutate func(*ResponseRequest)
	}{
		{"tenant", func(r *ResponseRequest) { r.Tenant = "" }}, {"padded id", func(r *ResponseRequest) { r.ResponseID = " response-1" }}, {"statement", func(r *ResponseRequest) { r.StatementID = "" }}, {"statement digest", func(r *ResponseRequest) { r.StatementDigest = "" }}, {"binding", func(r *ResponseRequest) { r.BindingDigest = "" }}, {"receipt", func(r *ResponseRequest) { r.EvidenceReceipt = "" }}, {"idempotency", func(r *ResponseRequest) { r.IdempotencyKey = "" }}, {"version", func(r *ResponseRequest) { r.StatementVersion = 0 }}, {"status", func(r *ResponseRequest) { r.Status = "BAD" }}, {"kind", func(r *ResponseRequest) { r.Kind = "BAD" }},
		{"accepted reason", func(r *ResponseRequest) { r.Reason = "reason" }}, {"refused reason", func(r *ResponseRequest) { r.Status = ResponseRefused; r.Reason = "" }}, {"response target", func(r *ResponseRequest) { r.CorrectsResponseID = "old" }}, {"correction target", func(r *ResponseRequest) { r.Kind = AssertionCorrection }}, {"correction reason", func(r *ResponseRequest) {
			r.Kind = AssertionCorrection
			r.CorrectsResponseID = "old"
			r.Authority = "authority"
			r.Reason = ""
		}}, {"correction authority", func(r *ResponseRequest) { r.Kind = AssertionCorrection; r.CorrectsResponseID = "old" }}, {"padded obligation", func(r *ResponseRequest) { r.AffectedObligations = []string{" obligation"} }}, {"duplicate obligation", func(r *ResponseRequest) { r.AffectedObligations = []string{"o", "o"} }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			if err := ValidateResponseDraft(Response{Tenant: r.Tenant, ResponseID: r.ResponseID, StatementID: r.StatementID, StatementVersion: r.StatementVersion, StatementDigest: r.StatementDigest, BindingDigest: r.BindingDigest, Status: r.Status, Kind: r.Kind, Reason: r.Reason, EvidenceReceipt: r.EvidenceReceipt, IdempotencyKey: r.IdempotencyKey, CorrectsResponseID: r.CorrectsResponseID, Authority: r.Authority, AffectedObligations: r.AffectedObligations}); err == nil || !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	good := base
	good.Kind = AssertionCorrection
	good.CorrectsResponseID = "old"
	good.Authority = "authority"
	good.Reason = "corrected"
	full := Response{Tenant: good.Tenant, ResponseID: good.ResponseID, Revision: 1, StatementID: good.StatementID, StatementVersion: good.StatementVersion, StatementDigest: good.StatementDigest, BindingDigest: good.BindingDigest, Status: good.Status, Kind: good.Kind, Reason: good.Reason, EvidenceReceipt: good.EvidenceReceipt, IdempotencyKey: good.IdempotencyKey, CorrectsResponseID: good.CorrectsResponseID, Authority: good.Authority, RecordedAt: testTrustedAt, RequestDigest: "request"}
	if err := ValidateResponse(full); err != nil {
		t.Fatalf("valid full response: %v", err)
	}
	full.Revision = 0
	if err := ValidateResponse(full); err == nil || !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("missing revision err=%v", err)
	}
}

func TestMemoryResponseStore_AppendLookupHistoryAndContext(t *testing.T) {
	store := NewMemoryResponseStore()
	recorder, err := NewRecorder(store, TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil }))
	if err != nil {
		t.Fatal(err)
	}
	first, err := recorder.Record(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	first.AffectedObligations = []string{"mutated"}
	got, err := store.GetResponse(context.Background(), "tenant-a", first.ResponseID, 1)
	if err != nil || len(got.AffectedObligations) != 0 {
		t.Fatalf("stored clone=%+v err=%v", got, err)
	}
	if _, err := store.GetResponse(context.Background(), "tenant-a", first.ResponseID, 0); !errors.Is(err, ErrResponseNotFound) {
		t.Fatalf("revision zero err=%v", err)
	}
	if _, err := store.GetResponse(context.Background(), "tenant-a", first.ResponseID, 2); !errors.Is(err, ErrResponseNotFound) {
		t.Fatalf("missing revision err=%v", err)
	}
	if _, err := store.GetResponseByIdempotency(context.Background(), "other", first.IdempotencyKey); !errors.Is(err, ErrResponseNotFound) {
		t.Fatalf("cross-tenant idem err=%v", err)
	}
	if _, err := store.ListResponseHistory(context.Background(), "tenant-a", "missing"); !errors.Is(err, ErrResponseNotFound) {
		t.Fatalf("missing history err=%v", err)
	}
	cancel, cancelFn := context.WithCancel(context.Background())
	cancelFn()
	if _, err := store.GetResponse(cancel, "tenant-a", first.ResponseID, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled get err=%v", err)
	}
	if _, err := store.GetResponseByIdempotency(cancel, "tenant-a", first.IdempotencyKey); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled idem err=%v", err)
	}
	if _, err := store.ListResponseHistory(cancel, "tenant-a", first.ResponseID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled history err=%v", err)
	}
	if _, err := (*MemoryResponseStore)(nil).AppendResponse(context.Background(), Response{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("nil store err=%v", err)
	}
	if _, err := store.AppendResponse(context.Background(), Response{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("invalid append err=%v", err)
	}
	if _, err := NewRecorder(nil, TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil })); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("nil store recorder err=%v", err)
	}
	if _, err := NewRecorder(store, nil); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("nil clock recorder err=%v", err)
	}
	var nilRecorder *Recorder
	if _, err := nilRecorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("nil recorder err=%v", err)
	}
	replayed, err := RecordResponse(context.Background(), store, TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil }), testResponseRequest(ResponseAccepted))
	if err != nil || !replayed.Replayed {
		t.Fatalf("package recorder replay=%+v err=%v", replayed, err)
	}
}
