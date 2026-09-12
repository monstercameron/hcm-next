package outbox_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/performance"
)

// perf004Policy derives PERF-004's ResourcePolicy from the declared
// PERF-ENV-001 workload envelopes instead of a pair of magic numbers: the
// SMALL tier's own concurrent-command limit is exactly the bound one small
// tenant is entitled to occupy of a resource it shares with others, and the
// MEDIUM tier's concurrent-command limit stands in for that resource's total
// shared capacity across every tenant. Fails outright (RED) if PERF-ENV-001
// ever stops declaring both tiers, rather than silently falling back to an
// invented number.
func perf004Policy(t *testing.T) outbox.ResourcePolicy {
	t.Helper()
	var small, medium performance.WorkloadEnvelope
	var haveSmall, haveMedium bool
	for _, envelope := range performance.EnvelopeFixtures() {
		if err := performance.Check(envelope); err != nil {
			t.Fatalf("declared envelope %q fails its own PERF-ENV-001 validation: %v", envelope.ID, err)
		}
		switch envelope.Tier {
		case performance.SmallTier:
			small, haveSmall = envelope, true
		case performance.MediumTier:
			medium, haveMedium = envelope, true
		}
	}
	if !haveSmall || !haveMedium {
		t.Fatal("PERF-ENV-001 fixtures no longer declare both a SMALL and a MEDIUM tier envelope")
	}
	policy := outbox.ResourcePolicy{
		Capacity:       int(medium.ConcurrentCommands.Value),
		PerTenantShare: int(small.ConcurrentCommands.Value),
	}
	if policy.Capacity <= policy.PerTenantShare {
		t.Fatalf("policy = %#v, want the declared resource capacity to exceed one tenant's declared share so a flood test can prove the SHARE, not the capacity, bounds it", policy)
	}
	return policy
}

// perf004Candidates builds count candidates for one tenant/criticality pair,
// FIFO-ordered oldest-first so a caller can control arrival order relative
// to other batches.
func perf004Candidates(tenant uuid.UUID, criticality string, count int, base time.Time, resource string) []outbox.Candidate {
	candidates := make([]outbox.Candidate, count)
	for i := 0; i < count; i++ {
		candidates[i] = outbox.Candidate{
			Record: outbox.Record{
				Tenant:      tenant,
				OutboxID:    uuid.New(),
				Criticality: criticality,
				AvailableAt: base.Add(-time.Duration(count-i) * time.Second),
			},
			Resource: resource,
		}
	}
	return candidates
}

// TestTodo_PERF_004 proves the RED/GREEN of PERF-004: a flood from one
// background tenant, older than everything else in the pool, cannot violate
// the declared small-tenant share or starve P0/P1 work queued after it --
// and every candidate that gets shed to hold that bound carries evidence
// naming exactly which quota shed it.
func TestTodo_PERF_004(t *testing.T) {
	resource := "perf-004-shared-dispatch"
	policy := perf004Policy(t)

	backgroundTenant := uuid.New()
	smallTenant := uuid.New()
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)

	// The flood: far more P3/P4 background work than either bound allows,
	// all older (arrived first) than the small tenant's own work.
	floodCount := policy.Capacity * 3
	var candidates []outbox.Candidate
	candidates = append(candidates, perf004Candidates(backgroundTenant, outbox.CriticalityP4, floodCount/2, now.Add(-time.Hour), resource)...)
	candidates = append(candidates, perf004Candidates(backgroundTenant, outbox.CriticalityP3, floodCount-floodCount/2, now.Add(-time.Hour), resource)...)

	// The small tenant's own high-criticality work, queued strictly after
	// the flood arrived.
	const p0Count, p1Count = 5, 5
	candidates = append(candidates, perf004Candidates(smallTenant, outbox.CriticalityP0, p0Count, now, resource)...)
	candidates = append(candidates, perf004Candidates(smallTenant, outbox.CriticalityP1, p1Count, now, resource)...)

	ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{resource: policy})
	result := outbox.Schedule(candidates, ledger)

	// Starvation bound: every one of the small tenant's P0/P1 items is
	// admitted, ahead of the much larger and strictly older P3/P4 flood.
	var smallAdmitted int
	for _, admitted := range result.Admitted {
		if admitted.Record.Tenant == smallTenant {
			smallAdmitted++
		}
	}
	if smallAdmitted != p0Count+p1Count {
		t.Fatalf("small tenant admitted = %d, want all %d of its P0/P1 items admitted despite arriving after the older flood", smallAdmitted, p0Count+p1Count)
	}
	for i, admitted := range result.Admitted[:p0Count+p1Count] {
		if admitted.Record.Tenant != smallTenant {
			t.Fatalf("admitted[%d] = %#v, want the small tenant's P0/P1 work ranked ahead of any P3/P4 flood item regardless of arrival order", i, admitted.Record)
		}
	}

	// Noisy-neighbour bound: the flood tenant is capped at exactly its
	// declared per-tenant share, never the (three times larger) resource
	// capacity, even though nothing else is competing for what remains.
	var floodAdmitted int
	for _, admitted := range result.Admitted {
		if admitted.Record.Tenant == backgroundTenant {
			floodAdmitted++
		}
	}
	if floodAdmitted != policy.PerTenantShare {
		t.Fatalf("flood tenant admitted = %d, want exactly its declared share %d", floodAdmitted, policy.PerTenantShare)
	}
	if got, want := ledger.InFlight(resource), policy.PerTenantShare+p0Count+p1Count; got != want {
		t.Fatalf("resource in-flight = %d, want %d (share-bound, not capacity-bound: capacity %d was never approached)", got, want, policy.Capacity)
	}

	// GREEN: every deferred candidate -- not a sample -- carries evidence
	// naming the specific quota that shed it.
	wantDeferred := floodCount - policy.PerTenantShare
	if len(result.Deferred) != wantDeferred {
		t.Fatalf("deferred = %d, want %d (the flood beyond its own tenant share)", len(result.Deferred), wantDeferred)
	}
	for i, deferred := range result.Deferred {
		if deferred.Reason == "" {
			t.Fatalf("deferred[%d] = %#v carries no evidence -- a bare zero value on a shed decision is the exact defect PERF-004 exists to remove", i, deferred.Candidate.Record)
		}
		if deferred.Reason != outbox.ReasonTenantShareExceeded {
			t.Fatalf("deferred[%d] reason = %q, want %q", i, deferred.Reason, outbox.ReasonTenantShareExceeded)
		}
		if deferred.Candidate.Record.Tenant != backgroundTenant {
			t.Fatalf("deferred[%d] belongs to tenant %v, want only the flood tenant's excess deferred", i, deferred.Candidate.Record.Tenant)
		}
	}

	// Latency bound under backpressure: once the shared resource itself
	// (not one tenant's share) is signalled unhealthy, even the small
	// tenant's next-pass P0 work is deferred -- but with distinct evidence
	// naming backpressure rather than a tenant share, so the two causes are
	// never confused with one another.
	unhealthy := admission.DecideBackpressure(
		admission.BackpressureSignal{Source: "perf-004", Dependency: resource, State: admission.BackpressureUnavailable, RetryAfter: 30},
		[]string{resource},
	)
	ledger.ApplyBackpressure(resource, unhealthy)
	nextPass := outbox.Schedule(
		perf004Candidates(smallTenant, outbox.CriticalityP0, 1, now.Add(time.Minute), resource),
		ledger,
	)
	if len(nextPass.Admitted) != 0 || len(nextPass.Deferred) != 1 {
		t.Fatalf("under sustained backpressure admitted = %d deferred = %d, want the P0 item deferred rather than dispatched into an unhealthy resource", len(nextPass.Admitted), len(nextPass.Deferred))
	}
	if nextPass.Deferred[0].Reason != outbox.ReasonBackpressureZeroed {
		t.Fatalf("deferral reason = %q, want %q", nextPass.Deferred[0].Reason, outbox.ReasonBackpressureZeroed)
	}
}

// TestTodo_PERF_004_Security proves tenant isolation of the fairness
// accounting itself: one tenant's flood cannot consume another tenant's
// configured share of a shared resource, no admitted or deferred candidate
// is ever mislabeled with the wrong tenant, and the evidence attached to a
// shed decision is a stable, tenant-blind constant that never leaks either
// tenant's identity or any state carried on their rows.
func TestTodo_PERF_004_Security(t *testing.T) {
	resource := "perf-004-tenant-isolation"
	policy := perf004Policy(t)
	floodTenant, fairTenant := uuid.New(), uuid.New()
	now := time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)

	var candidates []outbox.Candidate
	candidates = append(candidates, perf004Candidates(floodTenant, outbox.CriticalityP2, policy.Capacity*5, now.Add(-time.Hour), resource)...)
	candidates = append(candidates, perf004Candidates(fairTenant, outbox.CriticalityP2, policy.PerTenantShare, now, resource)...)

	ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{resource: policy})
	result := outbox.Schedule(candidates, ledger)

	var floodAdmitted, fairAdmitted int
	for _, admitted := range result.Admitted {
		switch admitted.Record.Tenant {
		case floodTenant:
			floodAdmitted++
		case fairTenant:
			fairAdmitted++
		default:
			t.Fatalf("admitted candidate for an unrecognized tenant: %#v", admitted.Record)
		}
	}
	if floodAdmitted != policy.PerTenantShare {
		t.Fatalf("flood tenant admitted = %d, want capped at its own declared share %d no matter how much larger its flood is", floodAdmitted, policy.PerTenantShare)
	}
	if fairAdmitted != policy.PerTenantShare {
		t.Fatalf("fair tenant admitted = %d, want its full declared share %d, untouched by the other tenant's flood", fairAdmitted, policy.PerTenantShare)
	}

	fairTenantID, floodTenantID := fairTenant.String(), floodTenant.String()
	for i, deferred := range result.Deferred {
		if deferred.Candidate.Record.Tenant != floodTenant {
			t.Fatalf("deferred[%d] belongs to tenant %v, want only the flood tenant's excess deferred -- the fair tenant's own share must never be touched", i, deferred.Candidate.Record.Tenant)
		}
		if deferred.Reason == "" {
			t.Fatalf("deferred[%d] carries no evidence", i)
		}
		if deferred.Reason != outbox.ReasonTenantShareExceeded {
			t.Fatalf("deferred[%d] reason = %q, want %q", i, deferred.Reason, outbox.ReasonTenantShareExceeded)
		}
		// The evidence itself is a stable, tenant-blind constant: it must
		// never leak the flood tenant's own identity, let alone the fair
		// tenant's, and it carries no state (payload, schema, causal
		// metadata) from either tenant's rows.
		if strings.Contains(deferred.Reason, fairTenantID) || strings.Contains(deferred.Reason, floodTenantID) {
			t.Fatalf("deferred[%d] reason %q leaks a tenant identity", i, deferred.Reason)
		}
	}

	if got, want := ledger.InFlight(resource), 2*policy.PerTenantShare; got != want {
		t.Fatalf("resource in-flight = %d, want %d (both tenants held at exactly their own share)", got, want)
	}
}

// BenchmarkTodo_PERF_004 benchmarks the actual scheduling decision -- sorting
// a mixed-tenant, mixed-criticality candidate pool and admitting it against a
// shared, per-tenant-capped resource -- not a stub. b.Loop() is used per this
// repository's b.N-loop analyzer policy.
func BenchmarkTodo_PERF_004(b *testing.B) {
	resource := "perf-004-bench"
	tenantA, tenantB := uuid.New(), uuid.New()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	var candidates []outbox.Candidate
	candidates = append(candidates, perf004Candidates(tenantA, outbox.CriticalityP4, 1800, now.Add(-time.Hour), resource)...)
	candidates = append(candidates, perf004Candidates(tenantB, outbox.CriticalityP0, 200, now, resource)...)
	policy := map[string]outbox.ResourcePolicy{resource: {Capacity: 500, PerTenantShare: 300}}

	b.ReportAllocs()
	for b.Loop() {
		ledger := outbox.NewResourceLedger(policy)
		outbox.Schedule(candidates, ledger)
	}
}
