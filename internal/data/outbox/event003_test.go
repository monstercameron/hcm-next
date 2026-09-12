package outbox_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// TestTodo_EVENT_003 proves the RED/GREEN of EVENT-003: a flood of
// background work spread across tenants must not delay a P0 payroll/IAM
// item that shares its resource, and the resulting admission order is
// deterministic rather than an accident of map or slice iteration order.
func TestTodo_EVENT_003(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	// A flood of P4 background work, spread across two tenants, all older
	// (earlier AvailableAt, so FIFO-first) than the one P0 item.
	var candidates []outbox.Candidate
	for i := 0; i < 20; i++ {
		tenant := tenantA
		if i%2 == 0 {
			tenant = tenantB
		}
		candidates = append(candidates, outbox.Candidate{
			Record: outbox.Record{
				Tenant:      tenant,
				OutboxID:    uuid.New(),
				Criticality: outbox.CriticalityP4,
				AvailableAt: now.Add(-time.Duration(i+1) * time.Second),
			},
			Resource: "shared-mailer",
		})
	}
	payroll := outbox.Record{
		Tenant: tenantA, OutboxID: uuid.New(), Criticality: outbox.CriticalityP0, AvailableAt: now,
	}
	candidates = append(candidates, outbox.Candidate{Record: payroll, Resource: "shared-mailer"})

	newLedger := func() *outbox.ResourceLedger {
		return outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{"shared-mailer": {Capacity: 5}})
	}

	result := outbox.Schedule(candidates, newLedger())
	if len(result.Admitted) != 5 {
		t.Fatalf("admitted = %d, want 5 (the configured resource capacity)", len(result.Admitted))
	}
	if result.Admitted[0].Record.OutboxID != payroll.OutboxID {
		t.Fatalf("first admitted = %#v, want the P0 payroll item ahead of the older P4 flood", result.Admitted[0])
	}
	for _, admitted := range result.Admitted[1:] {
		if admitted.Record.Criticality != outbox.CriticalityP4 {
			t.Fatalf("unexpected criticality filling the remaining slots: %#v", admitted.Record)
		}
	}
	if len(result.Deferred) != 16 {
		t.Fatalf("deferred = %d, want the remaining 16 flood items (20 flood - 4 that filled the leftover capacity)", len(result.Deferred))
	}
	for _, deferred := range result.Deferred {
		if deferred.Record.OutboxID == payroll.OutboxID {
			t.Fatal("the P0 payroll item was deferred behind the flood")
		}
	}

	// Deterministic: scheduling the exact same pool again produces the exact
	// same admitted set in the exact same order, not an accident of one
	// run's map/slice iteration.
	again := outbox.Schedule(candidates, newLedger())
	if len(again.Admitted) != len(result.Admitted) {
		t.Fatalf("non-deterministic admission count: %d vs %d", len(again.Admitted), len(result.Admitted))
	}
	for i := range again.Admitted {
		if again.Admitted[i].Record.OutboxID != result.Admitted[i].Record.OutboxID {
			t.Fatalf("non-deterministic admission order at index %d", i)
		}
	}

	t.Run("one logical retry budget spans layers", func(t *testing.T) {
		// admission.Provisioner.Consume is already replay-safe by attempt
		// identity; this proves outbox's own AttemptIdentity formula lets a
		// second, independent layer (standing in for a transaction
		// coordinator's own retry callback) that computes the same identity
		// converge on the one stored receipt instead of minting a second
		// token from a budget that only ever allowed one.
		provisioner := admission.NewProvisioner()
		account := outbox.RetryAccount{Provisioner: provisioner}
		spec := admission.ProvisionSpec{
			TenantID: "tenant-x", Service: "outbox", Dependency: "payments-api",
			LogicalOperationID: "op-1", OperationKind: "payroll.run",
			Allowed: 1, Retryable: []admission.FailureClass{admission.FailureTransient}, Version: "v1",
		}
		budget, err := provisioner.Provision(spec)
		if err != nil {
			t.Fatalf("provision: %v", err)
		}

		rec := outbox.Record{OutboxID: uuid.New(), Causal: &outbox.CausalMetadata{LogicalOperationID: "op-1"}}
		identity := outbox.AttemptIdentity(rec, 1)
		attempt := admission.RetryAttempt{
			LogicalOperationID: spec.LogicalOperationID, OperationKind: spec.OperationKind,
			TenantID: spec.TenantID, Dependency: spec.Dependency, Failure: admission.FailureTransient, Attempt: 1,
		}

		first, err := account.Consume(spec, identity, attempt)
		if err != nil {
			t.Fatalf("layer one consume: %v", err)
		}
		if first.Disposition != admission.RetryAllowed {
			t.Fatalf("layer one disposition = %v, want RETRY", first.Disposition)
		}

		// A second layer computing the identical identity for the identical
		// physical attempt must not consume a second token.
		second, err := account.Consume(spec, identity, attempt)
		if err != nil {
			t.Fatalf("layer two consume: %v", err)
		}
		if second != first {
			t.Fatalf("layer two receipt = %#v, want the identical stored receipt from layer one: %#v", second, first)
		}
		got, ok := provisioner.Snapshot(budget.ID)
		if !ok {
			t.Fatal("budget missing after consumption")
		}
		if got.Consumed != 1 {
			t.Fatalf("consumed = %d, want exactly 1 despite two layers observing the same attempt", got.Consumed)
		}
	})
}
