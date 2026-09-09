package transaction_test

import (
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
)

// sampleBoundary returns a valid ConsistencyBoundary admitting the
// "people." stream prefix under LOCAL_POSTGRES storage, fenced at epoch 7.
func sampleBoundary() transaction.ConsistencyBoundary {
	return transaction.ConsistencyBoundary{
		BoundaryID:    "boundary:cell-1",
		Tenant:        values.TenantId("acme-eu"),
		CellID:        "cell-1",
		CoordinatorID: "coordinator:cell-1-primary",
		Admitted: []transaction.AdmissionSelector{
			{StorageClass: "LOCAL_POSTGRES", StreamPrefix: "people."},
		},
		Isolation:                transaction.IsolationSerializable,
		Protocol:                 transaction.CommitProtocolSingleDatabaseACID,
		CoordinatorEpoch:         7,
		CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects,
	}
}

// samplePlan builds a minimal intent.TransactionPlan directly: TX-002
// resolves against PlanID/Tenant/Participants alone, so the test does not
// need the full registry/proposal machinery TX-001's own tests exercise.
func samplePlan(tenant values.TenantId, participants ...intent.PlanParticipant) intent.TransactionPlan {
	return intent.TransactionPlan{
		PlanID:       "plan:1",
		Tenant:       tenant,
		Participants: participants,
	}
}

func localParticipant(id, stream, storageClass string) intent.PlanParticipant {
	return intent.PlanParticipant{ParticipantID: id, StreamID: stream, StorageClass: storageClass, Local: true}
}

func remoteParticipant(id, stream, storageClass string) intent.PlanParticipant {
	return intent.PlanParticipant{ParticipantID: id, StreamID: stream, StorageClass: storageClass, Local: false}
}

// TestTodo_TX_002 is the PRIMARY test for resolving and fencing the
// ConsistencyBoundary.
//
// RED: a participant outside the tenant/cell/database boundary, an
// unsupported stream, a stale coordinator epoch, or a remote effect is
// admitted to the local ACID set.
//
// GREEN: the boundary identifies admitted streams/storage/cell/tenant/
// protocol/coordinator fence; external participants become durable effects.
func TestTodo_TX_002(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		t.Run("invalid boundary: no admission selector", func(t *testing.T) {
			b := sampleBoundary()
			b.Admitted = nil
			plan := samplePlan("acme-eu", localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"))
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrInvalidBoundary) {
				t.Fatalf("err=%v, want ErrInvalidBoundary", err)
			}
		})

		t.Run("participant outside the tenant boundary", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("other-tenant", localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"))
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrTenantMismatch) {
				t.Fatalf("err=%v, want ErrTenantMismatch", err)
			}
		})

		t.Run("unsupported stream: storage class matches no selector", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("acme-eu", localParticipant("p1", "people.employment.9001", "REMOTE_MONGO"))
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrUnsupportedStream) {
				t.Fatalf("err=%v, want ErrUnsupportedStream", err)
			}
		})

		t.Run("unsupported stream: prefix matches no selector", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("acme-eu", localParticipant("p1", "rewards.compensation.9001", "LOCAL_POSTGRES"))
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrUnsupportedStream) {
				t.Fatalf("err=%v, want ErrUnsupportedStream", err)
			}
		})

		t.Run("stale coordinator epoch", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("acme-eu", localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"))
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 6)
			if !errors.Is(err, transaction.ErrStaleCoordinatorEpoch) {
				t.Fatalf("err=%v, want ErrStaleCoordinatorEpoch", err)
			}
		})

		t.Run("no participant", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("acme-eu")
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrInvalidResolution) {
				t.Fatalf("err=%v, want ErrInvalidResolution", err)
			}
		})

		t.Run("duplicate participant id", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("acme-eu",
				localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"),
				localParticipant("p1", "people.employment.9002", "LOCAL_POSTGRES"),
			)
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrInvalidResolution) {
				t.Fatalf("err=%v, want ErrInvalidResolution", err)
			}
		})

		t.Run("boundary over capacity", func(t *testing.T) {
			b := sampleBoundary()
			b.MaxParticipants = 1
			plan := samplePlan("acme-eu",
				localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"),
				localParticipant("p2", "people.employment.9002", "LOCAL_POSTGRES"),
			)
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrBoundaryOverCapacity) {
				t.Fatalf("err=%v, want ErrBoundaryOverCapacity", err)
			}
		})

		t.Run("only non-local participants admits nothing", func(t *testing.T) {
			b := sampleBoundary()
			plan := samplePlan("acme-eu", remoteParticipant("p1", "provider.notify", "PROVIDER_API"))
			_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
			if !errors.Is(err, transaction.ErrInvalidResolution) {
				t.Fatalf("err=%v, want ErrInvalidResolution", err)
			}
		})
	})

	t.Run("a remote effect is never admitted to the local ACID set", func(t *testing.T) {
		b := sampleBoundary()
		// This participant's storage class and stream WOULD match the
		// boundary's admission selector, but the plan itself declares it
		// non-local: it must still land in Effects, never Admitted.
		remote := remoteParticipant("p-remote", "people.employment.9002", "LOCAL_POSTGRES")
		local := localParticipant("p-local", "people.employment.9001", "LOCAL_POSTGRES")
		plan := samplePlan("acme-eu", local, remote)

		res, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		for _, p := range res.Admitted {
			if p.ParticipantID == remote.ParticipantID {
				t.Fatalf("remote participant %q was admitted to the local ACID set", remote.ParticipantID)
			}
		}
		foundEffect := false
		for _, p := range res.Effects {
			if p.ParticipantID == remote.ParticipantID {
				foundEffect = true
			}
		}
		if !foundEffect {
			t.Fatalf("remote participant %q did not become a durable effect", remote.ParticipantID)
		}
	})

	t.Run("GREEN", func(t *testing.T) {
		b := sampleBoundary()
		local1 := localParticipant("p-local-1", "people.employment.9001", "LOCAL_POSTGRES")
		local2 := localParticipant("p-local-2", "people.assignment.9001", "LOCAL_POSTGRES")
		remote := remoteParticipant("p-remote", "provider.hris.notify", "PROVIDER_API")
		plan := samplePlan("acme-eu", local1, local2, remote)

		res, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if res.PlanID != plan.PlanID {
			t.Fatalf("resolution does not name the plan it resolved")
		}
		if len(res.Admitted) != 2 {
			t.Fatalf("admitted %d participant(s), want 2", len(res.Admitted))
		}
		if len(res.Effects) != 1 || res.Effects[0].ParticipantID != remote.ParticipantID {
			t.Fatalf("effects = %+v, want exactly %q", res.Effects, remote.ParticipantID)
		}
		wantLockOrder := []string{local1.StreamID, local2.StreamID}
		sort.Strings(wantLockOrder)
		if len(res.LockOrder) != 2 || res.LockOrder[0] != wantLockOrder[0] || res.LockOrder[1] != wantLockOrder[1] {
			t.Fatalf("lock order = %v, want %v", res.LockOrder, wantLockOrder)
		}
		if res.Fence.BoundaryID != b.BoundaryID || res.Fence.CoordinatorID != b.CoordinatorID || res.Fence.Epoch != b.CoordinatorEpoch {
			t.Fatalf("fence = %+v, does not bind the exact boundary/coordinator/epoch", res.Fence)
		}
		if res.Fence.Token() == "" {
			t.Fatalf("fence carries no token")
		}
		if res.CrossBoundaryDisposition != b.CrossBoundaryDisposition {
			t.Fatalf("resolution does not carry the boundary's cross-boundary disposition")
		}
		if res.Digest == "" {
			t.Fatalf("resolution carries no digest")
		}
		if err := res.VerifyDigest(); err != nil {
			t.Fatalf("digest does not verify: %v", err)
		}
	})
}

// TestTodo_TX_002_Race resolves the same plan and boundary concurrently and
// requires one stable digest and lock order: a resolution that depended on
// goroutine scheduling or slice construction order would diverge here.
func TestTodo_TX_002_Race(t *testing.T) {
	b := sampleBoundary()
	plan := samplePlan("acme-eu",
		localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"),
		localParticipant("p2", "people.assignment.9001", "LOCAL_POSTGRES"),
		remoteParticipant("p3", "provider.hris.notify", "PROVIDER_API"),
	)

	const n = 24
	digests := make([]string, n)
	lockOrders := make([][]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Reorder participants across goroutines: admission and lock
			// order must not depend on plan construction order.
			p := plan
			if i%2 == 1 {
				reordered := make([]intent.PlanParticipant, len(plan.Participants))
				for j, part := range plan.Participants {
					reordered[len(plan.Participants)-1-j] = part
				}
				p.Participants = reordered
			}
			res, err := transaction.ResolveConsistencyBoundary(b, p, 7)
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			digests[i] = res.Digest
			lockOrders[i] = res.LockOrder
		}(i)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("goroutine %d produced a different resolution digest for equivalent input", i)
		}
		if len(lockOrders[i]) != len(lockOrders[0]) {
			t.Fatalf("goroutine %d produced a different lock order length", i)
		}
		for j := range lockOrders[0] {
			if lockOrders[i][j] != lockOrders[0][j] {
				t.Fatalf("goroutine %d produced a different lock order: %v vs %v", i, lockOrders[i], lockOrders[0])
			}
		}
	}
}

// TestTodo_TX_002_Security proves tenant isolation cannot be bypassed by a
// participant that otherwise looks admissible: a plan from a foreign tenant
// is rejected outright, and a storage class that merely resembles an
// admitted one (a different cell's convention) is never treated as a match.
func TestTodo_TX_002_Security(t *testing.T) {
	b := sampleBoundary()

	t.Run("a foreign tenant plan is never resolved against this boundary", func(t *testing.T) {
		plan := samplePlan("intruder-tenant", localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"))
		res, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
		if !errors.Is(err, transaction.ErrTenantMismatch) {
			t.Fatalf("err=%v, want ErrTenantMismatch", err)
		}
		if len(res.Admitted) != 0 {
			t.Fatalf("a rejected resolution admitted %d participant(s)", len(res.Admitted))
		}
	})

	t.Run("a look-alike storage class from another cell is not admitted", func(t *testing.T) {
		// "LOCAL_POSTGRES_CELL_2" is not "LOCAL_POSTGRES": exact equality,
		// never a prefix or substring match, decides admission.
		plan := samplePlan("acme-eu", localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES_CELL_2"))
		_, err := transaction.ResolveConsistencyBoundary(b, plan, 7)
		if !errors.Is(err, transaction.ErrUnsupportedStream) {
			t.Fatalf("err=%v, want ErrUnsupportedStream", err)
		}
	})
}

// TestTodo_TX_002_Mutation perturbs one semantic element of a resolution at a
// time and requires the digest to move. A resolution digest that ignored an
// element would let one resolution be substituted for another downstream.
func TestTodo_TX_002_Mutation(t *testing.T) {
	b := sampleBoundary()
	baselinePlan := samplePlan("acme-eu",
		localParticipant("p1", "people.employment.9001", "LOCAL_POSTGRES"),
		localParticipant("p2", "people.assignment.9001", "LOCAL_POSTGRES"),
		remoteParticipant("p3", "provider.hris.notify", "PROVIDER_API"),
	)
	baseline, err := transaction.ResolveConsistencyBoundary(b, baselinePlan, 7)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(intent.TransactionPlan, transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary)
	}{
		{"an added admitted participant", func(p intent.TransactionPlan, b transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary) {
			p.Participants = append(append([]intent.PlanParticipant(nil), p.Participants...),
				localParticipant("p4", "people.compensation.9001", "LOCAL_POSTGRES"))
			return p, b
		}},
		{"a different remote participant", func(p intent.TransactionPlan, b transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary) {
			p.Participants = append([]intent.PlanParticipant(nil), p.Participants...)
			p.Participants[2] = remoteParticipant("p3", "provider.other.notify", "PROVIDER_API")
			return p, b
		}},
		{"the coordinator id", func(p intent.TransactionPlan, b transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary) {
			b.CoordinatorID = "coordinator:cell-1-standby"
			return p, b
		}},
		{"the boundary id", func(p intent.TransactionPlan, b transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary) {
			b.BoundaryID = "boundary:cell-2"
			return p, b
		}},
		{"the coordinator epoch", func(p intent.TransactionPlan, b transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary) {
			b.CoordinatorEpoch = 8
			return p, b
		}},
		{"the cross-boundary disposition", func(p intent.TransactionPlan, b transaction.ConsistencyBoundary) (intent.TransactionPlan, transaction.ConsistencyBoundary) {
			b.CrossBoundaryDisposition = transaction.CrossBoundaryDispositionChildTransaction
			return p, b
		}},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			// Resolve under the boundary's own (possibly mutated) epoch: this
			// suite targets the resolution's digest sensitivity, not
			// staleness (TestTodo_TX_002 already covers that RED case).
			p, boundary := m.mutate(baselinePlan, b)
			res, err := transaction.ResolveConsistencyBoundary(boundary, p, boundary.CoordinatorEpoch)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if res.Digest == baseline.Digest {
				t.Fatalf("changing %s left the resolution digest unchanged", m.name)
			}
			if err := res.VerifyDigest(); err != nil {
				t.Fatalf("the mutated resolution digest does not verify: %v", err)
			}
		})
	}

	t.Run("a tampered resolution fails its own digest check", func(t *testing.T) {
		tampered := baseline
		tampered.LockOrder = append([]string(nil), tampered.LockOrder...)
		tampered.LockOrder[0] = "tampered.stream"
		if err := tampered.VerifyDigest(); !errors.Is(err, transaction.ErrInvalidResolution) {
			t.Fatalf("a tampered resolution verified: %v", err)
		}
	})
}

// TestTodo_TX_002_Integration resolves the ConsistencyBoundary against a
// TransactionPlan produced by the real TX-001 CompilePlan, proving TX-002
// consumes actual compiled-plan output rather than a hand-rolled stand-in.
func TestTodo_TX_002_Integration(t *testing.T) {
	def := intent.Definition{
		Ref:          intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1},
		AllowedModes: []intent.Mode{intent.ModeSimulate},
	}
	rev := intent.ProposalRevision{
		ProposalRevisionID: "rev:1",
		IntentID:           "intent:1",
		Tenant:             values.TenantId("acme-eu"),
		MaterialDigest:     digest.Reference{Digest: "material-digest-1"},
	}
	key, err := values.NewResourceKey(values.TenantId("acme-eu"), values.Kind("assignment"), "employment", "9001", "primary")
	if err != nil {
		t.Fatalf("build resource key: %v", err)
	}
	expectedRev, err := values.NewSequenceRevision("people.employment.9001", 42)
	if err != nil {
		t.Fatalf("build revision token: %v", err)
	}
	deadline, err := values.NewInstantFromUnix(4102444800, 0) // 2100-01-01T00:00:00Z
	if err != nil {
		t.Fatalf("build deadline: %v", err)
	}

	in := intent.PlanInput{
		Proposal:   rev,
		Definition: def,
		Mode:       intent.ModeSimulate,
		Governance: intent.GovernanceSnapshot{
			SnapshotDigest: "governance-1", AuthZDecision: "PERMIT", LegalDecision: "PERMIT",
			PolicyDecision: "PERMIT", RiskDecision: "ACCEPT",
		},
		Conflict: intent.ConflictSnapshot{
			SnapshotDigest: "conflict-1", FenceToken: "fence-1", FootprintRef: "footprint-1",
		},
		Participants: []intent.PlanParticipant{
			{ParticipantID: "participant:people", StreamID: "people.employment.9001", StorageClass: "LOCAL_POSTGRES", Local: true},
		},
		Reads: []intent.PlannedRead{{ResourceKey: key, ExpectedRevision: expectedRev}},
		Appends: []intent.PlannedAppend{{
			StreamID: "people.employment.9001", ExpectedSequence: 43,
			EventType: "people.assignment_position_changed/v1", PayloadDigest: "append-payload-1",
		}},
		IdempotencyRecordRef:   "idempotency:promote:9001",
		ApprovalRequirementIDs: nil,
		RevalidationRuleRefs:   []string{"promotion_execution_revalidation/v1"},
		ExpiresAt:              deadline,
	}
	plan, err := intent.CompilePlan(in, nil)
	if err != nil {
		t.Fatalf("compile plan: %v", err)
	}

	b := sampleBoundary()
	res, err := transaction.ResolveConsistencyBoundary(b, plan, b.CoordinatorEpoch)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(res.Admitted) != 1 || res.Admitted[0].ParticipantID != "participant:people" {
		t.Fatalf("admitted = %+v, want exactly the compiled plan's local participant", res.Admitted)
	}
	if len(res.Effects) != 0 {
		t.Fatalf("effects = %+v, want none: the compiled plan declared every participant local", res.Effects)
	}
	if err := res.VerifyDigest(); err != nil {
		t.Fatalf("digest does not verify: %v", err)
	}
	if plan.Executable() {
		t.Fatalf("the underlying plan reports itself executable")
	}
}
