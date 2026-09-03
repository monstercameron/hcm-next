package signal_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/steps/signal"
)

var updateGolden = flag.Bool("update", false, "update golden fixtures in testdata/")

// hmacVerifier is a plain HMAC-SHA256 Verifier for tests. Production wiring
// binds signal.Verifier to whatever key/cert material the integration's
// IngressTrustProfile names; a shared-secret HMAC is enough to exercise
// Accept's signature check.
type hmacVerifier struct{ key []byte }

func (v hmacVerifier) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, v.key)
	mac.Write(payload)
	return mac.Sum(nil)
}

func (v hmacVerifier) Verify(sig signal.Signal) error {
	want := v.sign(sig.Payload)
	if !hmac.Equal(want, sig.Signature) {
		return errors.New("hmac: signature mismatch")
	}
	return nil
}

func mustInstant(t *testing.T, s string) values.Instant {
	t.Helper()
	var i values.Instant
	if err := i.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("instant %q: %v", s, err)
	}
	return i
}

var (
	testVerifier = hmacVerifier{key: []byte("wf-step-006-shared-secret")}
	testNow      = panicInstant("2026-06-17T12:00:00Z")
)

// panicInstant parses a fixed, known-good RFC 3339 literal at package init
// time, where no *testing.T is available to report a failure.
func panicInstant(s string) values.Instant {
	var i values.Instant
	if err := i.UnmarshalText([]byte(s)); err != nil {
		panic(err)
	}
	return i
}

func baseSubscription() signal.SignalSubscription {
	return signal.SignalSubscription{
		Tenant:             "acme-corp",
		WorkflowInstanceID: "wf-instance-1",
		NodeID:             "signal.background_check_result",
		EventType:          "background_check.completed",
		CorrelationKey:     "candidate_id",
		CorrelationValue:   "cand-42",
		ExpectedSchemaRef:  "schema.background_check_result/v1",
		AcceptedSources:    []string{"vendor.checkr", "vendor.sterling"},
		Ordering:           signal.OrderingMonotonicSequence,
	}
}

func baseSignal(t *testing.T, payload []byte) signal.Signal {
	t.Helper()
	return signal.Signal{
		Tenant:           "acme-corp",
		Source:           "vendor.checkr",
		EventType:        "background_check.completed",
		SchemaRef:        "schema.background_check_result/v1",
		CorrelationKey:   "candidate_id",
		CorrelationValue: "cand-42",
		SequenceNumber:   1,
		IdempotencyKey:   "checkr-delivery-1001",
		Payload:          payload,
		Taint:            workflow.TaintTainted,
		Signature:        testVerifier.sign(payload),
		ReceivedAt:       testNow,
	}
}

// TestTodo_WF_STEP_006 proves planning/todos.md WF-STEP-006: a signal wrong in
// any single dimension, a duplicate with different bytes, a late signal or an
// unmatched signal resumes nothing and returns a typed status; a signal
// conforming in every dimension is durably correlated and schedules its
// continuation exactly once, and a duplicate stays inspectable without
// repeating it.
func TestTodo_WF_STEP_006(t *testing.T) {
	sub := baseSubscription()
	payload := []byte(`{"result":"CLEAR"}`)

	t.Run("RED_wrong_tenant", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.Tenant = "other-tenant"
		sig.Signature = testVerifier.sign(sig.Payload)
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedWrongTenant)
	})

	t.Run("RED_unmatched_event_type", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.EventType = "offer.signed"
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedUnmatched)
	})

	t.Run("RED_unmatched_correlation_key", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.CorrelationKey = "requisition_id"
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedUnmatched)
	})

	t.Run("RED_wrong_correlation_value", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.CorrelationValue = "cand-99"
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedWrongCorrelation)
	})

	t.Run("RED_wrong_schema", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.SchemaRef = "schema.background_check_result/v2"
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedWrongSchema)
	})

	t.Run("RED_wrong_source", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.Source = "vendor.unlisted"
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedWrongSource)
	})

	t.Run("RED_wrong_signature", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.Signature = []byte("forged")
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedInvalidSignature)
	})

	t.Run("RED_wrong_order", func(t *testing.T) {
		first := baseSignal(t, payload)
		first.IdempotencyKey = "checkr-delivery-seq-1"
		log := signal.NewSignalLog()
		if res, err := log.Accept(sub, first, testVerifier, testNow); err != nil || res.Status != signal.StatusAccepted {
			t.Fatalf("seeding first signal: res=%+v err=%v", res, err)
		}
		second := baseSignal(t, payload)
		second.SequenceNumber = 1 // not greater than the first
		second.IdempotencyKey = "checkr-delivery-seq-2"
		res, err := log.Accept(sub, second, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedWrongOrder)
	})

	t.Run("RED_duplicate_different_bytes_is_refused_as_incident", func(t *testing.T) {
		log := signal.NewSignalLog()
		first := baseSignal(t, payload)
		if res, err := log.Accept(sub, first, testVerifier, testNow); err != nil || res.Status != signal.StatusAccepted {
			t.Fatalf("seeding first signal: res=%+v err=%v", res, err)
		}
		differentPayload := []byte(`{"result":"FLAGGED"}`)
		second := baseSignal(t, differentPayload) // same IdempotencyKey, different bytes
		res, err := log.Accept(sub, second, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedDuplicateDifferentBytes)
	})

	t.Run("RED_late_signal", func(t *testing.T) {
		closingSub := sub
		closingSub.ClosesAt = mustInstant(t, "2026-06-17T11:00:00Z")
		sig := baseSignal(t, payload)
		res, err := signal.Accept(closingSub, sig, nil, testVerifier, testNow) // testNow is after ClosesAt
		requireStatus(t, res, err, signal.StatusRefusedLate)
	})

	t.Run("GREEN_accepted_schedules_continuation_exactly_once", func(t *testing.T) {
		log := signal.NewSignalLog()
		sig := baseSignal(t, payload)
		res, err := log.Accept(sub, sig, testVerifier, testNow)
		if err != nil {
			t.Fatalf("Accept: %v", err)
		}
		if res.Status != signal.StatusAccepted || !res.Continuation {
			t.Fatalf("Result = %+v, want ACCEPTED with a scheduled continuation", res)
		}

		// A duplicate delivery of the exact same bytes remains inspectable but
		// never repeats the continuation.
		dup, err := log.Accept(sub, sig, testVerifier, testNow)
		if err != nil {
			t.Fatalf("duplicate Accept: %v", err)
		}
		if dup.Status != signal.StatusDuplicateSameBytes || dup.Continuation {
			t.Fatalf("duplicate Result = %+v, want DUPLICATE_SAME_BYTES with no continuation", dup)
		}
		if log.Len() != 2 {
			t.Fatalf("log has %d entries, want 2 (both attempts inspectable)", log.Len())
		}
	})
}

func requireStatus(t *testing.T, res signal.Result, err error, want signal.Status) {
	t.Helper()
	if err != nil {
		t.Fatalf("Accept returned error %v, want a typed %s status", err, want)
	}
	if res.Status != want {
		t.Fatalf("Status = %s, want %s (reason: %s)", res.Status, want, res.Reason)
	}
	if res.Continuation {
		t.Fatalf("Status %s must never schedule a continuation", res.Status)
	}
}

// TestTodo_WF_STEP_006_Golden proves the LogEntry a durable runtime would
// persist for an accepted signal renders byte-identical to a checked-in
// fixture, digested with the same profile-prefixed canonical sha256 style as
// internal/workflow/frontier/digest.go.
func TestTodo_WF_STEP_006_Golden(t *testing.T) {
	sub := baseSubscription()
	payload := []byte(`{"result":"CLEAR"}`)
	sig := baseSignal(t, payload)

	log := signal.NewSignalLog()
	if _, err := log.Accept(sub, sig, testVerifier, testNow); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	entries := log.For(sub.Digest())
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got, err := entries[0].JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	goldenPath := filepath.Join("testdata", "accepted_signal_golden.json")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("writing golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("accepted LogEntry does not match golden fixture.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestTodo_WF_STEP_006_Security proves the boundary-invariant negative
// families this step type must gate: a forged signature is refused even when
// every other dimension matches, and a duplicate idempotency key with
// different bytes is treated as the security incident it is (never silently
// coerced into a retry of the original).
func TestTodo_WF_STEP_006_Security(t *testing.T) {
	sub := baseSubscription()
	payload := []byte(`{"result":"CLEAR"}`)

	t.Run("forged_signature_never_accepted_regardless_of_payload", func(t *testing.T) {
		attacker := hmacVerifier{key: []byte("attacker-controlled-key")}
		sig := baseSignal(t, payload)
		sig.Signature = attacker.sign(payload) // valid under the wrong key
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedInvalidSignature)
	})

	t.Run("cross_tenant_signal_never_correlates_even_with_matching_correlation_value", func(t *testing.T) {
		sig := baseSignal(t, payload)
		sig.Tenant = "other-tenant" // otherwise identical, including correlation value
		res, err := signal.Accept(sub, sig, nil, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedWrongTenant)
	})

	t.Run("duplicate_key_different_bytes_is_an_incident_not_a_retry", func(t *testing.T) {
		log := signal.NewSignalLog()
		first := baseSignal(t, payload)
		if res, err := log.Accept(sub, first, testVerifier, testNow); err != nil || res.Status != signal.StatusAccepted {
			t.Fatalf("seeding: res=%+v err=%v", res, err)
		}
		tampered := baseSignal(t, []byte(`{"result":"CLEAR","extra":"injected"}`))
		res, err := log.Accept(sub, tampered, testVerifier, testNow)
		requireStatus(t, res, err, signal.StatusRefusedDuplicateDifferentBytes)
		// Both attempts remain inspectable: the incident is recorded, not lost.
		if log.Len() != 2 {
			t.Fatalf("log has %d entries, want 2", log.Len())
		}
	})

	t.Run("payload_taint_survives_into_the_log_unchanged", func(t *testing.T) {
		log := signal.NewSignalLog()
		sig := baseSignal(t, payload)
		sig.Taint = workflow.TaintTainted
		if _, err := log.Accept(sub, sig, testVerifier, testNow); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		entries := log.For(sub.Digest())
		if len(entries) != 1 || entries[0].Signal.Taint != workflow.TaintTainted {
			t.Fatalf("entries = %+v, want exactly one entry retaining TAINTED", entries)
		}
	})
}

// TestTodo_WF_STEP_006_Conformance proves Accept's Result maps onto exactly
// the routes StepSignal's conformance table declares (SUCCEEDED, TIMED_OUT,
// CANCELLED) or a Failed marker — never an invented outcome string
// frontier.Advance would reject — for every declared Status.
func TestTodo_WF_STEP_006_Conformance(t *testing.T) {
	conf, ok := workflow.ConformanceFor(workflow.StepSignal)
	if !ok {
		t.Fatal("StepSignal has no declared conformance table entry")
	}
	declared := map[workflow.Outcome]bool{}
	for _, o := range conf.Outcomes {
		declared[o] = true
	}

	statuses := []signal.Status{
		signal.StatusAccepted, signal.StatusDuplicateSameBytes,
		signal.StatusRefusedWrongTenant, signal.StatusRefusedUnmatched,
		signal.StatusRefusedWrongCorrelation, signal.StatusRefusedWrongSchema,
		signal.StatusRefusedInvalidSignature, signal.StatusRefusedWrongSource,
		signal.StatusRefusedWrongOrder, signal.StatusRefusedDuplicateDifferentBytes,
		signal.StatusRefusedLate,
	}
	for _, status := range statuses {
		res := signal.Result{Status: status}
		out := res.ToNodeOutcome("n1")
		switch {
		case out.Outcome != "":
			if !declared[out.Outcome] {
				t.Errorf("%s maps to outcome %q, which StepSignal's conformance table does not declare", status, out.Outcome)
			}
			if out.Failed {
				t.Errorf("%s: a completed route outcome must not also set Failed", status)
			}
		case !out.Failed:
			t.Errorf("%s maps to neither a declared route nor Failed; frontier.Advance would reject it", status)
		}
	}

	t.Run("accepted_and_duplicate_map_to_the_same_route", func(t *testing.T) {
		a := signal.Result{Status: signal.StatusAccepted}.ToNodeOutcome("n1")
		d := signal.Result{Status: signal.StatusDuplicateSameBytes}.ToNodeOutcome("n1")
		if a.Outcome != d.Outcome {
			t.Fatalf("ACCEPTED routes to %q but DUPLICATE_SAME_BYTES routes to %q; a duplicate must complete identically", a.Outcome, d.Outcome)
		}
	})
}

// TestTodo_WF_STEP_006_Mutation flips one field of an otherwise-accepted
// signal or subscription at a time and proves Accept's status changes, and
// that the resulting LogEntry/subscription digests change too — a digest
// that survives a mutated field cannot be trusted as a content identity.
func TestTodo_WF_STEP_006_Mutation(t *testing.T) {
	sub := baseSubscription()
	payload := []byte(`{"result":"CLEAR"}`)
	baseSig := baseSignal(t, payload)

	baseRes, err := signal.Accept(sub, baseSig, nil, testVerifier, testNow)
	if err != nil || baseRes.Status != signal.StatusAccepted {
		t.Fatalf("baseline Accept = %+v, %v, want ACCEPTED", baseRes, err)
	}

	mutations := []struct {
		name       string
		mutateSig  func(signal.Signal) signal.Signal
		wantStatus signal.Status
	}{
		{"tenant", func(s signal.Signal) signal.Signal { s.Tenant = "other-tenant"; return s }, signal.StatusRefusedWrongTenant},
		{"correlation_value", func(s signal.Signal) signal.Signal { s.CorrelationValue = "cand-1"; return s }, signal.StatusRefusedWrongCorrelation},
		{"schema", func(s signal.Signal) signal.Signal { s.SchemaRef = "schema.other/v1"; return s }, signal.StatusRefusedWrongSchema},
		{"source", func(s signal.Signal) signal.Signal { s.Source = "vendor.unlisted"; return s }, signal.StatusRefusedWrongSource},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			// Every mutation below changes a field other than Payload, so the
			// HMAC (computed over Payload only) stays valid and the check
			// under test is the one actually exercised, not a signature
			// failure masking it.
			mutated := m.mutateSig(baseSig)
			res, err := signal.Accept(sub, mutated, nil, testVerifier, testNow)
			if err != nil {
				t.Fatalf("Accept: %v", err)
			}
			if res.Status != m.wantStatus {
				t.Fatalf("Status = %s, want %s", res.Status, m.wantStatus)
			}
		})
	}

	t.Run("subscription_digest_changes_with_accepted_sources", func(t *testing.T) {
		mutated := sub
		mutated.AcceptedSources = append([]string{}, sub.AcceptedSources...)
		mutated.AcceptedSources = append(mutated.AcceptedSources, "vendor.new")
		if mutated.Digest() == sub.Digest() {
			t.Fatal("adding an accepted source did not change the subscription digest")
		}
	})

	t.Run("log_entry_digest_changes_with_status", func(t *testing.T) {
		log1 := signal.NewSignalLog()
		if _, err := log1.Accept(sub, baseSig, testVerifier, testNow); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		wrongTenantSig := baseSig
		wrongTenantSig.Tenant = "other-tenant"
		log2 := signal.NewSignalLog()
		if _, err := log2.Accept(sub, wrongTenantSig, testVerifier, testNow); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		e1, e2 := log1.For(sub.Digest()), log2.For(sub.Digest())
		if len(e1) != 1 || len(e2) != 1 {
			t.Fatalf("expected exactly one entry each, got %d and %d", len(e1), len(e2))
		}
		if e1[0].Digest == e2[0].Digest {
			t.Fatal("an ACCEPTED entry and a REFUSED_WRONG_TENANT entry produced the same digest")
		}
	})
}
