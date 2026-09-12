package operation_test

// PERF-006: bound external latency, quotas and retries.
//
// Most of what this todo asks for already exists in this package:
// ConnectorLedger (INTG-015) already enforces all four budget dimensions
// and already exposes QueueAge/PredictedCompletion on every decision, and
// the operation kernel (CONN-RT-007/INTG-011) already makes a blind resend
// after a timeout-after-send impossible by landing AMBIGUOUS, which is not
// leaseable. The tests below pin those properties under the PERF-006 name
// rather than reimplementing them.
//
// One property genuinely did not hold: RED names 5xx alongside 429, but
// only Observe429 existed. A 5xx server fault carries no vendor-declared
// reset the way a 429 does, so conflating the two would either (a) treat a
// broken vendor as if it had promised a reset time it never gave, or (b)
// have no way at all to tell a monitoring caller "the vendor is throttling
// us" from "the vendor is erroring". ObserveServerFault/ServerFaultUntil
// and the ScheduleReasonServerFault reason (added in quota.go) close that
// gap; the _Fault and _Security tests below are the first to exercise it.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

// TestTodo_PERF_006 is the PRIMARY test: all four ConnectorPolicy budget
// dimensions -- per-connection concurrency, per-tenant share, per-resource
// share and the criticality reserve -- are independently observable through
// TryReserve's stable refusal reason, and QueueAge/PredictedCompletion are
// populated and correct on both admitted and deferred ScheduleDecisions.
// ALREADY HELD by INTG-015's ConnectorLedger; this pins the composition
// PERF-006 requires under its own name.
func TestTodo_PERF_006(t *testing.T) {
	policy := operation.ConnectorPolicy{
		Quota:            operation.ConnectorQuota{Limit: 20, Window: time.Minute, MaxConcurrent: 2},
		PerTenantShare:   1,
		PerResourceShare: 1,
		ReserveForP0P1:   1,
	}
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-perf": policy})
	now := testNow

	// Dimension 1 (per-connection concurrency, indirectly): take the
	// connection's one non-reserved slot (MaxConcurrent 2, ReserveForP0P1 1
	// => 1 slot open to P2-P4).
	tenantA := candidateAt(uuid.New(), "tenant-a", "vendor-perf", "res-a", "P3", now)
	if ok, _ := ledger.TryReserve(now, tenantA); !ok {
		t.Fatal("first low-criticality reservation refused")
	}

	// Dimension 2: per-tenant share. tenant-a is capped at 1 regardless of
	// resource or criticality, independent of every other dimension.
	tenantAAgain := candidateAt(uuid.New(), "tenant-a", "vendor-perf", "res-b", "P2", now)
	if ok, reason := ledger.TryReserve(now, tenantAAgain); ok || reason != operation.ScheduleReasonTenantShareExceeded {
		t.Fatalf("tenant share not independently enforced: ok=%v reason=%q", ok, reason)
	}

	// Dimension 3: per-resource share. A fresh tenant reusing tenant-a's
	// resource is refused by the resource share specifically, not the
	// tenant share (a different tenant supplied it).
	sameResource := candidateAt(uuid.New(), "tenant-b", "vendor-perf", "res-a", "P2", now)
	if ok, reason := ledger.TryReserve(now, sameResource); ok || reason != operation.ScheduleReasonResourceShareExceeded {
		t.Fatalf("resource share not independently enforced: ok=%v reason=%q", ok, reason)
	}

	// Dimension 4: criticality reserve. A second low-criticality candidate
	// on a brand-new tenant/resource is refused by the reserve: the
	// connection's only P2-P4 slot is occupied and one slot is held back.
	freshLow := candidateAt(uuid.New(), "tenant-c", "vendor-perf", "res-c", "P4", now)
	if ok, reason := ledger.TryReserve(now, freshLow); ok || reason != operation.ScheduleReasonCriticalityReserved {
		t.Fatalf("criticality reserve not independently enforced: ok=%v reason=%q", ok, reason)
	}

	// The reserved slot exists to admit a P0/P1 even though the connection
	// otherwise looks fully committed.
	p0 := candidateAt(uuid.New(), "tenant-d", "vendor-perf", "res-d", "P0", now)
	if ok, _ := ledger.TryReserve(now, p0); !ok {
		t.Fatal("criticality reserve did not admit the P0 it exists to protect")
	}

	// QueueAge/PredictedCompletion on an admitted decision: age is zero,
	// predicted completion is the scheduling instant itself.
	later := now.Add(15 * time.Second)
	openLedger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"vendor-perf-open": {Quota: operation.ConnectorQuota{MaxConcurrent: 5}},
	})
	admitCandidate := candidateAt(uuid.New(), "tenant-e", "vendor-perf-open", "res-e", "P2", later)
	admitOut := openLedger.Schedule(later, []operation.ScheduleCandidate{admitCandidate})
	if len(admitOut.Admitted) != 1 || admitOut.Admitted[0].QueueAge != 0 || !admitOut.Admitted[0].PredictedCompletion.Equal(later) {
		t.Fatalf("admitted decision queue age/predicted completion = %+v", admitOut.Admitted)
	}

	// QueueAge/PredictedCompletion on a deferred decision: age reflects the
	// real elapsed wait, and predicted completion is strictly after now.
	deferCandidate := candidateAt(uuid.New(), "tenant-a", "vendor-perf", "res-a", "P2", now) // queued at `now`, rescheduled at `later`
	deferOut := ledger.Schedule(later, []operation.ScheduleCandidate{deferCandidate})
	if len(deferOut.Deferred) != 1 {
		t.Fatalf("expected the fully committed connection to defer the fresh candidate: %+v", deferOut)
	}
	wantAge := later.Sub(now)
	if deferOut.Deferred[0].QueueAge != wantAge {
		t.Fatalf("deferred queue age = %v, want %v", deferOut.Deferred[0].QueueAge, wantAge)
	}
	if !deferOut.Deferred[0].PredictedCompletion.After(later) {
		t.Fatalf("deferred predicted completion %v not after now %v", deferOut.Deferred[0].PredictedCompletion, later)
	}
}

// TestTodo_PERF_006_Fault proves each of the five RED conditions -- slow,
// 429, 5xx, timeout-after-send and unknown quota -- keeps the actual number
// of admitted/attempted retries bounded, never amplifying.
func TestTodo_PERF_006_Fault(t *testing.T) {
	now := testNow

	t.Run("slow: a long-held reservation bounds further admission by concurrency alone", func(t *testing.T) {
		ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
			"vendor-slow": {Quota: operation.ConnectorQuota{MaxConcurrent: 1}},
		})
		slow := candidateAt(uuid.New(), "tenant-a", "vendor-slow", "res", "P2", now)
		if ok, _ := ledger.TryReserve(now, slow); !ok {
			t.Fatal("slow candidate could not even take its own slot")
		}
		// The slow call never returns (no Release). A caller retrying
		// admission 25 times while it drags on must be admitted zero
		// further times: the single occupied slot is the bound, not luck.
		admitted := 0
		for i := 0; i < 25; i++ {
			at := now.Add(time.Duration(i) * time.Second)
			ok, reason := ledger.TryReserve(at, candidateAt(uuid.New(), "tenant-a", "vendor-slow", "res", "P2", now))
			if ok {
				admitted++
			} else if reason != operation.ScheduleReasonConcurrencyLimited {
				t.Fatalf("attempt %d refused for unexpected reason %q", i, reason)
			}
		}
		if admitted != 0 {
			t.Fatalf("admitted %d additional attempts while the slow slot was held, want 0", admitted)
		}
	})

	t.Run("429: a naive retry storm stays refused for the vendor's own reset, never amplifying", func(t *testing.T) {
		ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
			"vendor-429": {Quota: operation.ConnectorQuota{Limit: 100, Window: time.Minute, MaxConcurrent: 10}},
		})
		ledger.Observe429("vendor-429", now, 20*time.Second)
		admitted := 0
		const attempts = 25
		for i := 0; i < attempts; i++ {
			ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "vendor-429", "res", "P1", now))
			if ok {
				admitted++
			} else if reason != operation.ScheduleReasonRateLimited {
				t.Fatalf("attempt %d refused for unexpected reason %q, want RATE_LIMITED", i, reason)
			}
		}
		if admitted != 0 {
			t.Fatalf("admitted %d of %d attempts during 429 backoff, want 0 (retries amplified)", admitted, attempts)
		}
	})

	t.Run("5xx: a server fault backs off distinctly from a 429, bounding retries the same way", func(t *testing.T) {
		ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
			"vendor-5xx": {Quota: operation.ConnectorQuota{Limit: 100, Window: time.Minute, MaxConcurrent: 10}},
		})
		ledger.ObserveServerFault("vendor-5xx", now, 15*time.Second)
		admitted := 0
		const attempts = 25
		for i := 0; i < attempts; i++ {
			ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "vendor-5xx", "res", "P1", now))
			if ok {
				admitted++
			} else if reason != operation.ScheduleReasonServerFault {
				t.Fatalf("attempt %d refused for unexpected reason %q, want SERVER_FAULT_BACKOFF (distinct from RATE_LIMITED)", i, reason)
			}
		}
		if admitted != 0 {
			t.Fatalf("admitted %d of %d attempts during 5xx backoff, want 0 (retries amplified)", admitted, attempts)
		}
		// A 5xx must never register as a 429: a caller distinguishing
		// "the vendor said stop" from "the vendor is broken" needs a
		// genuinely different signal, not a reused RATE_LIMITED reason.
		if _, is429 := ledger.ThrottledUntil("vendor-5xx"); is429 {
			t.Fatal("5xx observation was recorded as a 429 throttle")
		}
		if until, faulted := ledger.ServerFaultUntil("vendor-5xx"); !faulted || !until.Equal(now.Add(15*time.Second)) {
			t.Fatalf("server fault deadline = %v faulted=%v, want %v", until, faulted, now.Add(15*time.Second))
		}
	})

	t.Run("timeout-after-send: AMBIGUOUS blocks a second lease, so a retry loop cannot multiply provider calls", func(t *testing.T) {
		j := newJournal()
		id := uuid.New()
		planned(t, j, id, "worker:perf-006-timeout", 1, operation.OrderingIndependent)
		queued(t, j, id)
		lease := leaseFor(t, j, id)
		writer := operation.NewPayrollSync()
		writer.TimeoutAfterSend = true
		if _, err := j.Dispatch(context.Background(), lease, writer); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout dispatch error = %v", err)
		}
		// A naive caller retries the lease step 10 times after the timeout.
		// Every one must be refused before a second provider call could even
		// be prepared.
		for i := 0; i < 10; i++ {
			_, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "retry-worker", At: testNow, Duration: time.Minute, Revalidate: confirmed})
			if !errors.Is(err, operation.ErrInvalidTransition) {
				t.Fatalf("retry %d: ambiguous operation accepted a lease, err=%v", i, err)
			}
		}
		if got := len(writer.Calls()); got != 1 {
			t.Fatalf("provider calls after a 10-attempt retry storm = %d, want exactly 1", got)
		}
	})

	t.Run("unknown quota: a connection nobody measured admits at most the conservative bound, however many attempts are made", func(t *testing.T) {
		ledger := operation.NewConnectorLedger(nil)
		admitted := 0
		const attempts = 50
		for i := 0; i < attempts; i++ {
			ok, _ := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "vendor-unmeasured", "res", "P0", now))
			if ok {
				admitted++
			}
		}
		if admitted != operation.UnknownConnectorQuota.Limit {
			t.Fatalf("admitted %d of %d attempts against an undeclared connection, want exactly %d (the conservative bound, not unlimited)",
				admitted, attempts, operation.UnknownConnectorQuota.Limit)
		}
	})
}

// TestTodo_PERF_006_Security proves tenant scoping holds under the full
// composition: tenant-a flooding a connection cannot consume tenant-b's own
// configured share, deferred decisions from tenant-a's flood never carry a
// foreign candidate or a tenant-naming reason, and a connection-wide 5xx
// fault (which is legitimately shared -- the vendor throttles the
// connection, not a tenant) still resolves through the same tenant-blind
// reason constant for every tenant.
func TestTodo_PERF_006_Security(t *testing.T) {
	policy := operation.ConnectorPolicy{
		Quota:          operation.ConnectorQuota{MaxConcurrent: 50},
		PerTenantShare: 1,
	}
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-sec-perf": policy})
	now := testNow

	var floodCandidates []operation.ScheduleCandidate
	for i := 0; i < 20; i++ {
		floodCandidates = append(floodCandidates, candidateAt(uuid.New(), "tenant-a", "vendor-sec-perf", "res-flood", "P2", now))
	}
	floodOut := ledger.Schedule(now, floodCandidates)
	if len(floodOut.Admitted) != 1 {
		t.Fatalf("tenant-a flood admitted %d, want exactly 1 (its own share)", len(floodOut.Admitted))
	}

	bCandidate := candidateAt(uuid.New(), "tenant-b", "vendor-sec-perf", "res-b", "P2", now)
	bOut := ledger.Schedule(now, []operation.ScheduleCandidate{bCandidate})
	if len(bOut.Admitted) != 1 || bOut.Admitted[0].Candidate.TenantID != "tenant-b" {
		t.Fatalf("tenant-b's own share was consumed by tenant-a's 20-candidate flood: %+v", bOut)
	}

	for _, d := range floodOut.Deferred {
		if d.Candidate.TenantID != "tenant-a" {
			t.Fatalf("deferred decision leaked a foreign candidate: %+v", d)
		}
		if d.Reason != operation.ScheduleReasonTenantShareExceeded {
			t.Fatalf("deferred reason = %q, want the stable tenant-blind constant", d.Reason)
		}
	}

	ledger.ObserveServerFault("vendor-sec-perf", now, 10*time.Second)
	if ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-b", "vendor-sec-perf", "res-b2", "P0", now)); ok || reason != operation.ScheduleReasonServerFault {
		t.Fatalf("tenant-b refusal during a connection-wide 5xx fault = ok=%v reason=%q, want SERVER_FAULT_BACKOFF", ok, reason)
	}
}

// TestTodo_PERF_006_Race drives two tenants concurrently against one
// connection with per-connection, per-tenant and criticality dimensions all
// configured, and asserts the exact admitted count per tenant: a wrong
// count fails this test, not merely a crash or a data race.
func TestTodo_PERF_006_Race(t *testing.T) {
	policy := operation.ConnectorPolicy{
		Quota:            operation.ConnectorQuota{MaxConcurrent: 6},
		PerTenantShare:   2,
		PerResourceShare: 5,
		ReserveForP0P1:   1,
	}
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-race-perf": policy})
	now := testNow

	var wg sync.WaitGroup
	var admittedA, admittedB int64
	const attemptsPerTenant = 15
	for i := 0; i < attemptsPerTenant; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cand := candidateAt(uuid.New(), "tenant-a", "vendor-race-perf", "res-a", "P2", now)
			if ok, _ := ledger.TryReserve(now, cand); ok {
				atomic.AddInt64(&admittedA, 1)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			cand := candidateAt(uuid.New(), "tenant-b", "vendor-race-perf", "res-b", "P2", now)
			if ok, _ := ledger.TryReserve(now, cand); ok {
				atomic.AddInt64(&admittedB, 1)
			}
		}()
	}
	wg.Wait()

	if admittedA != 2 {
		t.Fatalf("tenant-a admitted = %d, want exactly 2 (PerTenantShare, charged correctly under real concurrency)", admittedA)
	}
	if admittedB != 2 {
		t.Fatalf("tenant-b admitted = %d, want exactly 2 (PerTenantShare, charged correctly under real concurrency)", admittedB)
	}
	if got := ledger.InFlight("vendor-race-perf"); got != int(admittedA+admittedB) {
		t.Fatalf("InFlight after race = %d, want %d (charged exactly once per admitted reservation, never more)", got, admittedA+admittedB)
	}
}

// BenchmarkTodo_PERF_006 benchmarks the real decision path: Schedule
// arbitrating a mixed-criticality, mixed-tenant, mixed-resource candidate
// batch against a fully configured four-dimension policy. Uses b.Loop()
// per this repo's analyzer rules (b.N loops are flagged).
func BenchmarkTodo_PERF_006(b *testing.B) {
	policy := operation.ConnectorPolicy{
		Quota:            operation.ConnectorQuota{Limit: 1000, Window: time.Minute, MaxConcurrent: 8},
		PerTenantShare:   3,
		PerResourceShare: 3,
		ReserveForP0P1:   2,
	}
	now := testNow
	criticalities := []string{"P0", "P1", "P2", "P3", "P4"}
	for b.Loop() {
		ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-bench": policy})
		candidates := make([]operation.ScheduleCandidate, 0, 40)
		for i := 0; i < 40; i++ {
			candidates = append(candidates, candidateAt(
				uuid.New(),
				fmt.Sprintf("tenant-%d", i%5),
				"vendor-bench",
				fmt.Sprintf("res-%d", i%7),
				criticalities[i%len(criticalities)],
				now,
			))
		}
		out := ledger.Schedule(now, candidates)
		if len(out.Admitted)+len(out.Deferred) != 40 {
			b.Fatalf("schedule dropped candidates: admitted=%d deferred=%d", len(out.Admitted), len(out.Deferred))
		}
	}
}
