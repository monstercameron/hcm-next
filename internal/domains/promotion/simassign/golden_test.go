package simassign_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
)

// update rewrites the golden tables instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the assignment simulation golden files")

// TestTodo_PROMO_002_Golden pins the whole artifact over the PROMO-001 golden
// fixture: the same harborcare-demo snapshot ready-promotion.txt records, and
// every effect, change, refusal and digest this simulation derives from it.
//
// It is a text golden rather than a set of assertions because the point of the
// simulation is that it is one exact, reproducible artifact: an assertion suite
// proves the fields it thought to name, while a golden proves that nothing at
// all changed without somebody saying so.
func TestTodo_PROMO_002_Golden(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	snap := h.build(t, fixtureRequest(t))

	// The snapshot this simulation is golden over is byte-for-byte the one
	// PROMO-001's own golden records; if that ever stops being true, the two
	// goldens have drifted apart and this fails first.
	assertSnapshotMatchesPromo001Golden(t, snap)

	result, err := simassign.Simulate(simulateRequest(t, snap))
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	assertGolden(t, "assignment-ready.txt", renderGolden(result))
}

// TestTodo_PROMO_002_Golden_Withheld pins the same artifact for a caller who
// may not see the subject's placement. It sits beside the ready table so that
// what a denial changes -- and what it must not change -- is a diff between two
// committed files.
func TestTodo_PROMO_002_Golden_Withheld(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Worker = withheldPlacement(t, req)
	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with a withheld placement was accepted")
	}
	result, err := simassign.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	assertGolden(t, "assignment-withheld.txt", renderGolden(result))
}

// withheldPlacement denies exactly the placement fields, leaving the rest of
// the worker read authorized. It is the disclosure shape a caller entitled to
// know that a worker exists but not where they sit produces.
func withheldPlacement(t testing.TB, req promosnapshot.Request) people.AuthorizationDecision {
	t.Helper()
	return fixtures.DenyFields(req.Authorization.Worker, "policy:no_placement_disclosure",
		people.FieldJobCode, people.FieldGrade, people.FieldOrgUnit,
		people.FieldPositionID, people.FieldPayZone)
}

// assertSnapshotMatchesPromo001Golden reads PROMO-001's own golden table and
// checks that this fixture still produces the material digest recorded there.
func assertSnapshotMatchesPromo001Golden(t testing.TB, snap promosnapshot.PromotionInputSnapshot) {
	t.Helper()
	path := filepath.Join("..", "snapshot", "testdata", "golden", "ready-promotion.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the PROMO-001 golden at %s: %v", path, err)
	}
	const marker = "material digest: "
	for line := range strings.SplitSeq(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, marker) {
			continue
		}
		if want := strings.TrimPrefix(line, marker); want != snap.Digest {
			t.Fatalf("this lane's fixture snapshot digests to %s, but the PROMO-001 golden records %s",
				snap.Digest, want)
		}
		return
	}
	t.Fatalf("the PROMO-001 golden at %s records no material digest", path)
}

// renderGolden writes the simulation as a stable, diffable table.
func renderGolden(result simassign.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tenant: %s\n", result.Tenant)
	fmt.Fprintf(&b, "subject: %s\n", result.Subject)
	fmt.Fprintf(&b, "target position: %s\n", result.TargetPosition)
	fmt.Fprintf(&b, "effective on: %s\n", result.EffectiveOn)
	fmt.Fprintf(&b, "prior assignment ends: %s\n", result.PriorAssignmentEnd)
	fmt.Fprintf(&b, "snapshot digest: %s\n", result.SnapshotDigest)
	fmt.Fprintf(&b, "result digest: %s\n", result.Digest)
	fmt.Fprintf(&b, "executable: %t\n", result.Executable())

	b.WriteString("\nmanager chain:\n")
	fmt.Fprintf(&b, "    verdict:                %s\n", result.Chain.Verdict)
	fmt.Fprintf(&b, "    current relationship:   %s\n", orEmpty(result.Chain.CurrentRelationshipID))
	fmt.Fprintf(&b, "    current manager:        %s\n", orEmpty(result.Chain.CurrentManagerID))
	fmt.Fprintf(&b, "    proposed relationship:  %s\n", orEmpty(result.Chain.ProposedRelationshipID))
	fmt.Fprintf(&b, "    proposed manager:       %s\n", orEmpty(result.Chain.ProposedManagerID))
	fmt.Fprintf(&b, "    changed:                %t\n", result.Chain.Changed)
	fmt.Fprintf(&b, "    current depth:          %d\n", result.Chain.CurrentDepth)
	fmt.Fprintf(&b, "    proposed depth:         %d\n", result.Chain.ProposedDepth)
	fmt.Fprintf(&b, "    depth bound:            %d\n", result.Chain.DepthBound)
	for _, hop := range result.Chain.Hops {
		fmt.Fprintf(&b, "    hop %d: %s/%s -> %s (withheld=%t)\n",
			hop.Level, orEmpty(hop.RelationshipID), orEmpty(hop.Type), orEmpty(hop.ManagerID), hop.Withheld)
	}

	b.WriteString("\nposition occupancy:\n")
	fmt.Fprintf(&b, "    capacity:      %s\n", orEmpty(result.Occupancy.Capacity))
	fmt.Fprintf(&b, "    verdict:       %s\n", result.Occupancy.Verdict)
	fmt.Fprintf(&b, "    vacant after:  %s\n", vacancyText(result))
	fmt.Fprintf(&b, "    reservation:   planned=%t\n", result.ReservationPlanned)
	if result.ReservationPlanned {
		fmt.Fprintf(&b, "    reservation heads: %d\n", result.Reservation.Heads)
		fmt.Fprintf(&b, "    reservation fte:   %s\n", result.Reservation.FTE)
		fmt.Fprintf(&b, "    reservation key:   %s\n", result.Reservation.IdempotencyKey)
	}

	b.WriteString("\neffects:\n")
	for _, effect := range result.Effects {
		fmt.Fprintf(&b, "- %s\n", effect.Kind)
		fmt.Fprintf(&b, "    effect id:      %s\n", effect.EffectID)
		fmt.Fprintf(&b, "    participant:    %s (%s, local=%t)\n", effect.Participant, effect.StorageClass, effect.Local)
		fmt.Fprintf(&b, "    destination:    %s\n", effect.DestinationRef)
		fmt.Fprintf(&b, "    reversibility:  %s\n", effect.Reversibility)
		fmt.Fprintf(&b, "    compensation:   %s (%s)\n", effect.CompensationRef, effect.CompensationStrategy)
		fmt.Fprintf(&b, "    observation:    %s\n", effect.ObservationRef)
		fmt.Fprintf(&b, "    idempotency:    %s\n", effect.IdempotencyKey)
		fmt.Fprintf(&b, "    baseline:       %s\n", effect.ExpectedRevision)
		fmt.Fprintf(&b, "    derived from:   %s\n", strings.Join(effect.DerivedFrom, ","))
		for _, change := range effect.Changes {
			fmt.Fprintf(&b, "    change %s: %s -> %s (changed=%t, from %s)\n",
				change.Field, orEmpty(change.Before), orEmpty(change.After), change.Changed, change.SourceInput)
		}
	}

	b.WriteString("\nrefusals:\n")
	for _, refusal := range result.Refusals {
		fmt.Fprintf(&b, "- %s: %s\n", refusal.Kind, refusal.Reason)
		fmt.Fprintf(&b, "    input:        %s\n", orEmpty(refusal.InputName))
		fmt.Fprintf(&b, "    availability: %s\n", orEmpty(string(refusal.Availability)))
		fmt.Fprintf(&b, "    detail:       %s\n", refusal.Detail)
	}
	return b.String()
}

func vacancyText(result simassign.Result) string {
	if !result.Occupancy.HasVacantAfter {
		return "(none)"
	}
	return result.Occupancy.VacantAfter.String()
}

func orEmpty(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// assertGolden compares rendered against the committed file, or rewrites it
// under -update.
func assertGolden(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if got := rendered; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden %s does not match.\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
