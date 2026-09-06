package snapshot_test

import (
	"testing"

	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_PROMO_001_Mutation is the digest's whole claim, stated as a table:
// every material change moves the digest, and the one change that is evidence
// rather than material does not.
//
// It mutates the *sources*, not the snapshot: a test that edited a built
// snapshot and re-digested it would only prove that sha256 is a function.
func TestTodo_PROMO_001_Mutation(t *testing.T) {
	t.Parallel()
	baseline := newHarness(t).build(t, fixtureRequest(t))

	for _, tc := range []struct {
		name    string
		arrange func(*harness, *promosnapshot.Request)
		// material says whether the change is material: a material change must
		// move the snapshot digest, an evidential one must not.
		material bool
	}{
		{
			name: "a different current base pay",
			arrange: func(h *harness, _ *promosnapshot.Request) {
				h.Compensation.set.Fact.BasePay = mustMoney(t, "94000.00", fixtureCurrency)
			},
			material: true,
		},
		{
			name: "a different desired base pay",
			arrange: func(_ *harness, req *promosnapshot.Request) {
				req.DesiredBasePay = mustMoney(t, "99000.00", fixtureCurrency)
			},
			material: true,
		},
		{
			name: "the same value read at a later revision",
			arrange: func(h *harness, _ *promosnapshot.Request) {
				h.Compensation.set.Watermark = mustRevision(t, "rewards.package."+fixtureWorkerKey, 12)
			},
			material: true,
		},
		{
			name: "a different manager",
			arrange: func(h *harness, _ *promosnapshot.Request) {
				h.Org.fact.Manager = values.EntityRef{
					Tenant: h.Org.fact.Manager.Tenant,
					Kind:   h.Org.fact.Manager.Kind,
					Id:     "66666666-6666-4666-8666-666666666666",
				}
			},
			material: true,
		},
		{
			name: "a target position with no available head",
			arrange: func(h *harness, _ *promosnapshot.Request) {
				h.Position.revision.Capacity.CapacityHeads = 0
			},
			material: true,
		},
		{
			name: "a different budget observation",
			arrange: func(h *harness, _ *promosnapshot.Request) {
				h.Budget.ref.AvailableQuantity = mustDecimal(t, "40000.00", 2)
			},
			material: true,
		},
		{
			name: "a different effective date",
			arrange: func(_ *harness, req *promosnapshot.Request) {
				req.EffectiveOn = mustLocalDate(t, "2026-07-01")
			},
			material: true,
		},
		{
			name: "the same reads taken over a different connection",
			arrange: func(_ *harness, req *promosnapshot.Request) {
				req.SourceConnection = "replica"
			},
			material: false,
		},
		{
			name: "the same reads under a different reference version",
			arrange: func(_ *harness, req *promosnapshot.Request) {
				req.ReferenceVersion = "harborcare.promotion.reference/2026.2"
			},
			material: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			req := fixtureRequest(t)
			tc.arrange(h, &req)
			// Some mutations legitimately refuse the build (a full position
			// with no vacancy date); the snapshot is still returned and still
			// digests, which is exactly what has to be compared.
			mutated, _ := promosnapshot.Build(t.Context(), h.readers(), req)

			switch {
			case tc.material && mutated.Digest == baseline.Digest:
				t.Fatalf("a material change left the digest at %s:\n%s", baseline.Digest, mutated.Explain())
			case !tc.material && mutated.Digest != baseline.Digest:
				t.Fatalf("an evidential change moved the digest from %s to %s:\n%s",
					baseline.Digest, mutated.Digest, mutated.Explain())
			}
		})
	}

	t.Run("an evidential change still moves the read digest", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.ReferenceVersion = "harborcare.promotion.reference/2026.2"
		mutated := h.build(t, req)

		if mutated.Reads.Digest == baseline.Reads.Digest {
			t.Fatal("a changed reference version left the read snapshot digest unchanged")
		}
		if mutated.Digest != baseline.Digest {
			t.Fatal("a changed reference version moved the material digest")
		}
	})

	t.Run("a disclosure change alone moves the digest", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		req := fixtureRequest(t)
		req.Authorization.BudgetDisclosable = false
		req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"
		mutated, _ := promosnapshot.Build(t.Context(), h.readers(), req)

		if mutated.Digest == baseline.Digest {
			t.Fatal("withholding an input left the digest unchanged")
		}
		budgetInput, ok := mutated.Lookup(promosnapshot.InputBudgetAvailability)
		if !ok || budgetInput.CanonicalText != "" {
			t.Fatal("the withheld budget input still carries a value")
		}
	})
}
