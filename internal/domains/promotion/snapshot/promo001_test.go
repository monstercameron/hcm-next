package snapshot_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	enginesnapshot "github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
)

// TestTodo_PROMO_001 is the primary contract: one Build over the
// harborcare-demo Promotion fixture binds every declared input with a complete
// descriptor set, a completeness verdict and a digest, and the same inputs
// build the same snapshot twice.
func TestTodo_PROMO_001(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	snap := h.build(t, req)

	t.Run("binds every declared input in declaration order", func(t *testing.T) {
		names := make([]string, 0, len(snap.Inputs()))
		for _, in := range snap.Inputs() {
			names = append(names, in.Name)
		}
		want := promosnapshot.InputNames()
		if len(names) != len(want) {
			t.Fatalf("bound %d input(s), want %d: %v", len(names), len(want), names)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("input %d is %q, want %q", i, names[i], want[i])
			}
		}
	})

	t.Run("every input carries a complete descriptor set", func(t *testing.T) {
		for _, in := range snap.Inputs() {
			if err := in.Entry.Validate(); err != nil {
				t.Fatalf("input %s has an incomplete entry: %v", in.Name, err)
			}
			if in.Entry.Owner != promosnapshot.OwnerOf(in.Name) {
				t.Fatalf("input %s is owned by %q, want %q", in.Name, in.Entry.Owner, promosnapshot.OwnerOf(in.Name))
			}
			if in.Entry.Tenant != fixtures.Tenant {
				t.Fatalf("input %s belongs to tenant %q, want %q", in.Name, in.Entry.Tenant, fixtures.Tenant)
			}
			if in.Entry.KnownAt.Instant().Compare(req.KnownAt.Instant()) != 0 {
				t.Fatalf("input %s was resolved at known-at %s, want the request horizon %s",
					in.Name, in.Entry.KnownAt, req.KnownAt)
			}
			if in.Entry.ReferenceVersion != fixtureReferenceVer {
				t.Fatalf("input %s pins reference version %q, want %q",
					in.Name, in.Entry.ReferenceVersion, fixtureReferenceVer)
			}
		}
	})

	t.Run("the read snapshot binds the same inputs under one horizon", func(t *testing.T) {
		if snap.Reads.Digest == "" {
			t.Fatal("the resolved read snapshot has no digest")
		}
		if len(snap.Reads.Entries) != len(promosnapshot.InputNames()) {
			t.Fatalf("the read snapshot carries %d entries, want %d",
				len(snap.Reads.Entries), len(promosnapshot.InputNames()))
		}
		for _, name := range promosnapshot.InputNames() {
			if _, ok := snap.Reads.Lookup(name); !ok {
				t.Fatalf("the read snapshot does not carry input %q", name)
			}
		}
	})

	t.Run("the fixture promotion is complete", func(t *testing.T) {
		if snap.Completeness.Overall != enginesnapshot.VerdictSatisfied {
			t.Fatalf("overall completeness is %s, want SATISFIED:\n%s", snap.Completeness.Overall, snap.Explain())
		}
	})

	t.Run("the material reads are disclosed with exact values", func(t *testing.T) {
		for _, probe := range []struct{ name, contains string }{
			{promosnapshot.InputSubjectWorkerFacts, "lifecycle_status=active"},
			{promosnapshot.InputCurrentPlacement, "job_code=OPS-HRBP2"},
			{promosnapshot.InputManagerChain, "rel_mgr_1002"},
			{promosnapshot.InputTargetPositionCapacity, promosnapshot.CapacityAvailable},
			{promosnapshot.InputPayBandPositionCurrent, "annualized=93000.00 USD"},
			{promosnapshot.InputPayBandPositionDesired, "annualized=98000.00 USD"},
			{promosnapshot.InputBudgetAvailability, "available=50000.00"},
		} {
			text, ok := snap.Disclosed(probe.name)
			if !ok {
				t.Fatalf("input %s is not disclosed:\n%s", probe.name, snap.Explain())
			}
			if !strings.Contains(text, probe.contains) {
				t.Fatalf("input %s reads %q, want it to contain %q", probe.name, text, probe.contains)
			}
		}
	})

	t.Run("an optional-by-condition input is absent without blocking", func(t *testing.T) {
		vacancy, ok := snap.Lookup(promosnapshot.InputTargetPositionVacancy)
		if !ok {
			t.Fatal("the vacancy input is not bound at all")
		}
		if vacancy.Availability != promosnapshot.AvailabilityAbsent {
			t.Fatalf("vacancy availability is %s, want ABSENT", vacancy.Availability)
		}
		if vacancy.CanonicalText != "" {
			t.Fatalf("an ABSENT input carries the value %q", vacancy.CanonicalText)
		}
	})

	t.Run("identical authoritative inputs produce an identical digest", func(t *testing.T) {
		again := newHarness(t).build(t, fixtureRequest(t))
		if again.Digest != snap.Digest {
			t.Fatalf("two builds over identical inputs digest differently:\n%s\n%s", snap.Digest, again.Digest)
		}
		if again.Reads.Digest != snap.Reads.Digest {
			t.Fatalf("two builds over identical inputs resolve different read digests:\n%s\n%s",
				snap.Reads.Digest, again.Reads.Digest)
		}
	})

	t.Run("the digest is the kernel's material encoding", func(t *testing.T) {
		projection := snap.MaterialInputs()
		if len(projection.CurrentState) != len(promosnapshot.InputNames()) {
			t.Fatalf("the material projection asserts %d input(s), want %d",
				len(projection.CurrentState), len(promosnapshot.InputNames()))
		}
		if len(projection.SourceBaselines) != len(promosnapshot.InputNames()) {
			t.Fatalf("the material projection binds %d baseline(s), want %d",
				len(projection.SourceBaselines), len(promosnapshot.InputNames()))
		}
		if len(projection.MaterialPayload().WireBytes) == 0 {
			t.Fatal("the material projection encodes to no bytes")
		}
		if !strings.HasPrefix(snap.Digest, "sha256:") {
			t.Fatalf("digest %q is not a sha256 reference", snap.Digest)
		}
		// The simulation-owned material fields stay empty: a read snapshot
		// knows what was read, never what will be written.
		if len(projection.Writes) != 0 || len(projection.Effects) != 0 || len(projection.ProposedState) != 0 {
			t.Fatal("the material projection asserts writes, effects or proposed state")
		}
	})

	t.Run("the snapshot is immutable through its accessors", func(t *testing.T) {
		got := snap.Inputs()
		got[0].CanonicalText = "tampered"
		if again, ok := snap.Lookup(got[0].Name); !ok || again.CanonicalText == "tampered" {
			t.Fatal("mutating the returned input slice mutated the snapshot")
		}
	})

	t.Run("Explain names every input and no value", func(t *testing.T) {
		explanation := snap.Explain()
		for _, name := range promosnapshot.InputNames() {
			if !strings.Contains(explanation, name) {
				t.Fatalf("Explain does not name input %q:\n%s", name, explanation)
			}
		}
		for _, secret := range []string{"93000.00", "98000.00", "50000.00"} {
			if strings.Contains(explanation, secret) {
				t.Fatalf("Explain leaks the value %q:\n%s", secret, explanation)
			}
		}
	})

	t.Run("the baseline snapshot carries the kernel's coordinates", func(t *testing.T) {
		baseline := snap.BaselineSnapshot()
		if baseline.SnapshotID != snap.Digest {
			t.Fatalf("baseline snapshot id %q, want the snapshot digest %q", baseline.SnapshotID, snap.Digest)
		}
		if len(baseline.Revisions) != len(promosnapshot.InputNames()) {
			t.Fatalf("baseline pins %d revision(s), want %d", len(baseline.Revisions), len(promosnapshot.InputNames()))
		}
		if len(baseline.PresentInputs) != len(promosnapshot.InputNames())-1 {
			t.Fatalf("baseline reports %d present input(s), want %d",
				len(baseline.PresentInputs), len(promosnapshot.InputNames())-1)
		}
		if len(baseline.ForbiddenFields) != 0 {
			t.Fatalf("baseline forbids %v on a fully authorized build", baseline.ForbiddenFields)
		}
		if baseline.ObservedAt.Compare(req.KnownAt.Instant()) != 0 {
			t.Fatalf("baseline observed-at %s, want the known-at horizon %s", baseline.ObservedAt, req.KnownAt)
		}
	})
}

// TestTodo_PROMO_001_Refusal is the typed-refusal half of the primary
// contract: a required input the record cannot supply refuses the build by
// name rather than producing a snapshot with a hole in it.
func TestTodo_PROMO_001_Refusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		arrange func(*harness)
		input   string
		verdict enginesnapshot.CompletenessVerdict
	}{
		{
			name:    "an absent compensation record refuses the current band position",
			arrange: func(h *harness) { h.Compensation.exists = false },
			input:   promosnapshot.InputPayBandPositionCurrent,
			verdict: enginesnapshot.VerdictMissing,
		},
		{
			name:    "an absent budget observation refuses the budget input",
			arrange: func(h *harness) { h.Budget.exists = false },
			input:   promosnapshot.InputBudgetAvailability,
			verdict: enginesnapshot.VerdictMissing,
		},
		{
			name:    "an unresolvable manager graph refuses the manager chain",
			arrange: func(h *harness) { h.Org.vacant = true },
			input:   promosnapshot.InputManagerChain,
			verdict: enginesnapshot.VerdictMissing,
		},
		{
			name:    "a position the record does not hold refuses the capacity input",
			arrange: func(h *harness) { h.Position.exists = false },
			input:   promosnapshot.InputTargetPositionCapacity,
			verdict: enginesnapshot.VerdictMissing,
		},
		{
			name: "a worker field the read does not cover refuses the placement",
			arrange: func(h *harness) {
				h.Worker.Missing[people.FieldPayZone] = true
			},
			input:   promosnapshot.InputCurrentPlacement,
			verdict: enginesnapshot.VerdictUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			tc.arrange(h)
			snap, err := promosnapshot.Build(context.Background(), h.readers(), fixtureRequest(t))
			if err == nil {
				t.Fatalf("Build accepted a snapshot missing %s:\n%s", tc.input, snap.Explain())
			}
			if !errors.Is(err, promosnapshot.ErrInputUnavailable) {
				t.Fatalf("Build returned %v, want an ErrInputUnavailable refusal", err)
			}
			if got := promosnapshot.InputNameOf(err); got != tc.input {
				t.Fatalf("the refusal names input %q, want %q (%v)", got, tc.input, err)
			}
			var refusal *promosnapshot.InputError
			if !errors.As(err, &refusal) {
				t.Fatalf("the refusal is not an *InputError: %v", err)
			}
			if refusal.Verdict != tc.verdict {
				t.Fatalf("the refusal verdict is %s, want %s", refusal.Verdict, tc.verdict)
			}
			// The refused snapshot is still returned for evidence, and it
			// still names every input.
			if len(snap.Inputs()) != len(promosnapshot.InputNames()) {
				t.Fatalf("the refused snapshot binds %d input(s), want %d",
					len(snap.Inputs()), len(promosnapshot.InputNames()))
			}
		})
	}
}

// TestTodo_PROMO_001_Conditional proves the conditional vacancy requirement is
// evaluated rather than assumed: a target position with no available head
// makes the vacancy date required, and a position with room does not.
func TestTodo_PROMO_001_Conditional(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.Position.revision.Capacity.CapacityHeads = 0

	snap, err := promosnapshot.Build(context.Background(), h.readers(), fixtureRequest(t))
	if err == nil {
		t.Fatalf("a full position with no known vacancy date built a complete snapshot:\n%s", snap.Explain())
	}
	if got := promosnapshot.InputNameOf(err); got != promosnapshot.InputTargetPositionVacancy {
		t.Fatalf("the refusal names %q, want the vacancy input (%v)", got, err)
	}
	capacity, ok := snap.Lookup(promosnapshot.InputTargetPositionCapacity)
	if !ok || capacity.CanonicalText != promosnapshot.CapacityExhausted {
		t.Fatalf("the capacity input reads %q, want %q", capacity.CanonicalText, promosnapshot.CapacityExhausted)
	}
}

// BenchmarkTodo_PROMO_001 measures one full build over the fixture. It exists
// so a later change that turns eight in-memory reads into eight round trips is
// visible as a number rather than as a support ticket.
func BenchmarkTodo_PROMO_001(b *testing.B) {
	h := newHarness(b)
	readers := h.readers()
	req := fixtureRequest(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := promosnapshot.Build(ctx, readers, req); err != nil {
			b.Fatalf("Build: %v", err)
		}
	}
}
