package operation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

// TestTodo_CONN_RT_007 is the PRIMARY test. It proves that a successful
// provider call leaves the operation in PROVIDER_ACCEPTED, never complete on
// its own, and that only a typed, normalized observation moves it to a
// resolved state.
//
// The acceptance path (StateProviderAccepted, RetryObservationNeeded,
// ObservationRequired=true) already existed from INTG-011/INTG-016; this test
// pins that behavior rather than reimplementing it. NormalizeObservation is
// new: it is the piece that computes an ObservationVerdict from raw,
// provider-specific read-back evidence instead of letting a caller assert
// whatever verdict it likes.
func TestTodo_CONN_RT_007(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:conn-rt-007", 1, operation.OrderingIndependent)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()

	result, err := j.Dispatch(context.Background(), lease, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation.State != operation.StateProviderAccepted {
		t.Fatalf("provider acceptance state = %s, want PROVIDER_ACCEPTED", result.Operation.State)
	}
	if !result.ObservationRequired {
		t.Fatal("provider acceptance did not require observation")
	}
	if result.Attempt.RetryDisposition != operation.RetryObservationNeeded {
		t.Fatalf("attempt disposition = %s, want OBSERVATION_REQUIRED", result.Attempt.RetryDisposition)
	}

	// Acceptance alone is not completion: re-reading the row must still show
	// PROVIDER_ACCEPTED, not any form of success, before any observation.
	stillAccepted, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if stillAccepted.State != operation.StateProviderAccepted || stillAccepted.CompletionState == "COMPLETE" {
		t.Fatalf("operation completed without an observation: %+v", stillAccepted)
	}

	// A caller only has raw, provider-specific facts: whether the resource was
	// found, and what its content digested to. NormalizeObservation computes
	// the verdict; it is not supplied by the caller.
	raw := operation.ProviderReadBack{
		ExternalResourceKey: stillAccepted.ExternalResourceKey,
		Found:               true,
		ExternalObjectRef:   "payroll-object-1",
		ExternalVersion:     "v2",
		ObservedDigest:      result.Attempt.RequestDigest,
	}
	obs, err := operation.NormalizeObservation(stillAccepted, raw, uuid.New(), testNow, "payroll-authority")
	if err != nil {
		t.Fatal(err)
	}
	if obs.Verdict != operation.ObservationApplied {
		t.Fatalf("normalized verdict = %s, want APPLIED (digest matched the dispatched attempt)", obs.Verdict)
	}

	resolved, err := j.RecordObservation(context.Background(), obs)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != operation.StateReconciled || resolved.CompletionState != "COMPLETE" {
		t.Fatalf("resolved operation = %+v, want RECONCILED/COMPLETE", resolved)
	}
	if len(writer.Calls()) != 1 {
		t.Fatalf("observation triggered a provider call: calls=%d", len(writer.Calls()))
	}
}

func TestTodo_CONN_RT_007_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:conn-rt-007-int", 1, operation.OrderingIndependent)
	queued(t, j, id)

	// Dispatch: provider accepts but the send is ambiguous (a timeout after
	// the request may have reached the provider).
	firstLease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = true
	_, err := j.Dispatch(context.Background(), firstLease, writer)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ambiguous dispatch error = %v", err)
	}
	ambiguous, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil || ambiguous.State != operation.StateAmbiguous || len(ambiguous.Attempts) != 1 {
		t.Fatalf("ambiguous state = %+v, err=%v", ambiguous, err)
	}
	firstAttempt := ambiguous.Attempts[0]

	// Observation: a read-back that cannot find the record classifies as
	// NOT_APPLIED, so the operation goes RETRYABLE rather than fabricating
	// success.
	notApplied, err := operation.NormalizeObservation(ambiguous, operation.ProviderReadBack{
		ExternalResourceKey: ambiguous.ExternalResourceKey,
		Found:               false,
	}, uuid.New(), testNow, "payroll-authority")
	if err != nil {
		t.Fatal(err)
	}
	if notApplied.Verdict != operation.ObservationNotApplied {
		t.Fatalf("verdict = %s, want NOT_APPLIED", notApplied.Verdict)
	}
	afterObservation, err := j.RecordObservation(context.Background(), notApplied)
	if err != nil || afterObservation.State != operation.StateRetryable {
		t.Fatalf("post-observation state = %+v, err=%v", afterObservation, err)
	}

	// Reconciliation: a targeted redrive adds a second attempt to the SAME
	// operation id. The redrive itself never fabricates success either.
	redriven, err := j.Redrive(context.Background(), operation.RedriveRequest{
		TenantID: "tenant-promotion", OperationID: id, ActorRef: "operator:payroll", At: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if redriven.OperationID != id || redriven.RedriveCount != 1 || redriven.State != operation.StateQueued {
		t.Fatalf("redriven operation = %+v, want same id queued once", redriven)
	}

	secondLease := leaseFor(t, j, id)
	secondWriter := operation.NewPayrollSync()
	secondResult, err := j.Dispatch(context.Background(), secondLease, secondWriter)
	if err != nil {
		t.Fatal(err)
	}
	if secondResult.Operation.OperationID != id {
		t.Fatalf("redrive forked a new operation: %s != %s", secondResult.Operation.OperationID, id)
	}
	if len(secondResult.Operation.Attempts) != 2 {
		t.Fatalf("attempt count after redrive = %d, want 2 (parent attempt preserved)", len(secondResult.Operation.Attempts))
	}
	if secondResult.Operation.Attempts[0].AttemptID != firstAttempt.AttemptID {
		t.Fatal("redrive rewrote the parent attempt instead of appending")
	}
	if secondResult.Operation.Attempts[1].AttemptNumber != 2 {
		t.Fatalf("second attempt number = %d, want 2", secondResult.Operation.Attempts[1].AttemptNumber)
	}
	if secondResult.Operation.State != operation.StateProviderAccepted {
		t.Fatalf("redriven dispatch state = %s, want PROVIDER_ACCEPTED (still not complete)", secondResult.Operation.State)
	}

	// Reconcile with a normalized, matching observation and confirm exactly
	// one operation row exists under this id throughout.
	final, err := operation.NormalizeObservation(secondResult.Operation, operation.ProviderReadBack{
		ExternalResourceKey: secondResult.Operation.ExternalResourceKey,
		Found:               true,
		ObservedDigest:      secondResult.Attempt.RequestDigest,
	}, uuid.New(), testNow, "payroll-authority")
	if err != nil {
		t.Fatal(err)
	}
	reconciled, err := j.RecordObservation(context.Background(), final)
	if err != nil || reconciled.State != operation.StateReconciled {
		t.Fatalf("reconciliation = %+v, err=%v", reconciled, err)
	}
	rows, err := j.List(context.Background(), "tenant-promotion")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.OperationID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("operation id %s appears %d times, want 1 (redrive must never fork)", id, count)
	}
}

// TestTodo_CONN_RT_007_Fault proves that a timeout and a MayHaveSent failure
// each land on AMBIGUOUS with observation required and no resend, and that an
// UNKNOWN observation verdict escalates to repair rather than ever resolving
// to success.
func TestTodo_CONN_RT_007_Fault(t *testing.T) {
	t.Run("timeout is ambiguous and blocks a second lease without observation", func(t *testing.T) {
		j := newJournal()
		id := uuid.New()
		planned(t, j, id, "worker:conn-rt-007-fault-timeout", 1, operation.OrderingIndependent)
		queued(t, j, id)
		lease := leaseFor(t, j, id)
		writer := operation.NewPayrollSync()
		writer.TimeoutAfterSend = true
		_, err := j.Dispatch(context.Background(), lease, writer)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout dispatch error = %v", err)
		}
		op, err := j.Get(context.Background(), "tenant-promotion", id)
		if err != nil || op.State != operation.StateAmbiguous || op.ResponseClass != operation.ResponseAmbiguous {
			t.Fatalf("ambiguous operation = %+v, err=%v", op, err)
		}
		// No blind resend: AMBIGUOUS is not a leaseable state, so a second
		// dispatch attempt cannot even be prepared without an observation.
		if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-2", At: testNow, Duration: time.Minute, Revalidate: confirmed}); !errors.Is(err, operation.ErrInvalidTransition) {
			t.Fatalf("ambiguous operation accepted a lease = %v, want ErrInvalidTransition", err)
		}
		if len(writer.Calls()) != 1 {
			t.Fatalf("provider was called %d times, want exactly 1", len(writer.Calls()))
		}
	})

	t.Run("MayHaveSent provider error is also ambiguous with observation required", func(t *testing.T) {
		j := newJournal()
		id := uuid.New()
		planned(t, j, id, "worker:conn-rt-007-fault-mayhavesent", 1, operation.OrderingIndependent)
		queued(t, j, id)
		lease := leaseFor(t, j, id)
		writer := &mayHaveSentWriter{}
		result, err := j.Dispatch(context.Background(), lease, writer)
		var provErr *operation.ProviderError
		if !errors.As(err, &provErr) || !provErr.MayHaveSent {
			t.Fatalf("dispatch error = %v, want a MayHaveSent ProviderError", err)
		}
		if result.Operation.State != operation.StateAmbiguous || !result.ObservationRequired {
			t.Fatalf("MayHaveSent result = %+v", result)
		}
	})

	t.Run("UNKNOWN verdict escalates to repair, never to success", func(t *testing.T) {
		j := newJournal()
		id := uuid.New()
		planned(t, j, id, "worker:conn-rt-007-fault-unknown", 1, operation.OrderingIndependent)
		queued(t, j, id)
		lease := leaseFor(t, j, id)
		writer := operation.NewPayrollSync()
		writer.TimeoutAfterSend = true
		_, _ = j.Dispatch(context.Background(), lease, writer)
		ambiguous, err := j.Get(context.Background(), "tenant-promotion", id)
		if err != nil {
			t.Fatal(err)
		}

		// A read-back the connector itself cannot resolve normalizes to
		// UNKNOWN, never to APPLIED.
		obs, err := operation.NormalizeObservation(ambiguous, operation.ProviderReadBack{
			ExternalResourceKey: ambiguous.ExternalResourceKey,
			Ambiguous:           true,
		}, uuid.New(), testNow, "payroll-authority")
		if err != nil {
			t.Fatal(err)
		}
		if obs.Verdict != operation.ObservationUnknown {
			t.Fatalf("normalized verdict = %s, want UNKNOWN", obs.Verdict)
		}
		resolved, err := j.RecordObservation(context.Background(), obs)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.State != operation.StateRepairRequired || resolved.CompletionState == "COMPLETE" || resolved.ResponseClass == operation.ResponseSuccess {
			t.Fatalf("UNKNOWN verdict resolved to success: %+v", resolved)
		}
	})

	t.Run("a found record with a foreign digest normalizes to CONFLICT, not APPLIED", func(t *testing.T) {
		j := newJournal()
		id := uuid.New()
		planned(t, j, id, "worker:conn-rt-007-fault-conflict", 1, operation.OrderingIndependent)
		queued(t, j, id)
		lease := leaseFor(t, j, id)
		writer := operation.NewPayrollSync()
		result, err := j.Dispatch(context.Background(), lease, writer)
		if err != nil {
			t.Fatal(err)
		}
		obs, err := operation.NormalizeObservation(result.Operation, operation.ProviderReadBack{
			ExternalResourceKey: result.Operation.ExternalResourceKey,
			Found:               true,
			ObservedDigest:      "sha256:someone-elses-write",
		}, uuid.New(), testNow, "payroll-authority")
		if err != nil {
			t.Fatal(err)
		}
		if obs.Verdict != operation.ObservationConflict {
			t.Fatalf("verdict = %s, want CONFLICT (digest did not match any dispatched attempt)", obs.Verdict)
		}
	})
}

// FuzzTodo_CONN_RT_007 is the oracle test: no sequence of provider results,
// timeouts, observations and redrives may ever drive an operation to
// RECONCILED without a typed APPLIED observation having actually resolved it.
func FuzzTodo_CONN_RT_007(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{2, 0})
	f.Add([]byte{2, 3, 1, 0, 3})
	f.Add([]byte{4, 4, 4, 0})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, opcodes []byte) {
		if len(opcodes) > 60 {
			opcodes = opcodes[:60]
		}
		j := operation.NewMemoryJournal(func() time.Time { return testNow })
		id := uuid.New()
		req := request(id, "worker:fuzz-conn-rt-007", 1, operation.OrderingIndependent)
		if _, err := j.Plan(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if _, err := j.Queue(context.Background(), "tenant-promotion", id); err != nil {
			t.Fatal(err)
		}

		resolvedByTypedObservation := false
		verdicts := [...]operation.ObservationVerdict{operation.ObservationApplied, operation.ObservationNotApplied, operation.ObservationConflict, operation.ObservationUnknown}

		for _, raw := range opcodes {
			op, err := j.Get(context.Background(), "tenant-promotion", id)
			if err != nil {
				t.Fatal(err)
			}
			opcode := int(raw)

			switch op.State {
			case operation.StateQueued:
				lease, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "fuzz-worker", At: testNow, Duration: time.Minute, Revalidate: confirmed})
				if err != nil {
					continue
				}
				switch opcode % 3 {
				case 0:
					_, _ = j.Dispatch(context.Background(), lease, operation.NewPayrollSync())
				case 1:
					timeoutWriter := operation.NewPayrollSync()
					timeoutWriter.TimeoutAfterSend = true
					_, _ = j.Dispatch(context.Background(), lease, timeoutWriter)
				default:
					_, _ = j.Dispatch(context.Background(), lease, fuzzFailWriter{})
				}
			case operation.StateProviderAccepted, operation.StateAmbiguous, operation.StateObserving:
				var raw operation.ProviderReadBack
				verdict := verdicts[opcode%len(verdicts)]
				raw.ExternalResourceKey = op.ExternalResourceKey
				switch verdict {
				case operation.ObservationApplied:
					if len(op.Attempts) > 0 {
						raw.Found, raw.ObservedDigest = true, op.Attempts[len(op.Attempts)-1].RequestDigest
					}
				case operation.ObservationNotApplied:
					raw.Found = false
				case operation.ObservationConflict:
					raw.Found, raw.ObservedDigest = true, "sha256:unrelated-write"
				case operation.ObservationUnknown:
					raw.Ambiguous = true
				}
				obs, err := operation.NormalizeObservation(op, raw, uuid.New(), testNow, "fuzz-authority")
				if err != nil {
					t.Fatalf("NormalizeObservation refused a well-formed read-back: %v", err)
				}
				if obs.Verdict != verdict {
					t.Fatalf("NormalizeObservation verdict = %s, want %s for raw=%+v", obs.Verdict, verdict, raw)
				}
				resolved, err := j.RecordObservation(context.Background(), obs)
				if err == nil && obs.Verdict == operation.ObservationApplied && resolved.State == operation.StateReconciled {
					resolvedByTypedObservation = true
				}
			case operation.StateRetryable, operation.StateFailed:
				if opcode%2 == 0 {
					lease, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "fuzz-worker", At: testNow, Duration: time.Minute, Revalidate: confirmed})
					if err == nil {
						_, _ = j.Dispatch(context.Background(), lease, operation.NewPayrollSync())
					}
				} else {
					_, _ = j.Redrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: id, ActorRef: "fuzz-actor", At: testNow, Approved: true, RepairPlanID: "plan-fuzz"})
				}
			case operation.StateRepairRequired:
				_, _ = j.Redrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: id, ActorRef: "fuzz-actor", At: testNow, Approved: true, RepairPlanID: "plan-fuzz"})
			}

			current, err := j.Get(context.Background(), "tenant-promotion", id)
			if err != nil {
				t.Fatal(err)
			}
			if current.State == operation.StateReconciled && !resolvedByTypedObservation {
				t.Fatalf("operation %s reached RECONCILED without a typed APPLIED observation resolving it", id)
			}
		}
	})
}

type mayHaveSentWriter struct{}

func (mayHaveSentWriter) Write(context.Context, operation.WriteRequest) (operation.WriteResponse, error) {
	return operation.WriteResponse{}, &operation.ProviderError{Result: operation.ResponseAmbiguous, MayHaveSent: true, Cause: errors.New("connection reset after send")}
}

type fuzzFailWriter struct{}

func (fuzzFailWriter) Write(context.Context, operation.WriteRequest) (operation.WriteResponse, error) {
	return operation.WriteResponse{}, errors.New("fuzz: provider validation failure")
}
