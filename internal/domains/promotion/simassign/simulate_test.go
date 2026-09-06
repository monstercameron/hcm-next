package simassign_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/fixtures"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	"github.com/monstercameron/hcm-next/internal/domains/promotion/simassign"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestRequestValidateNamesTheBrokenContract proves each malformed request is
// refused for its own stated reason rather than folded into one error.
func TestRequestValidateNamesTheBrokenContract(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))

	for _, tc := range []struct {
		name   string
		damage func(*simassign.Request)
		want   string
	}{
		{"no target job", func(r *simassign.Request) { r.Target.JobCode = "" }, "target job code"},
		{"no target grade", func(r *simassign.Request) { r.Target.Grade = "" }, "target grade"},
		{"no target org unit", func(r *simassign.Request) { r.Target.OrgUnit = "" }, "target organizational unit"},
		{"no target pay zone", func(r *simassign.Request) { r.Target.PayZone = "" }, "target pay zone"},
		{"manager in another tenant", func(r *simassign.Request) {
			r.ProposedManager.Tenant = "another-tenant"
		}, "worker in the snapshot's tenant"},
		{"zero depth bound", func(r *simassign.Request) { r.ChainDepthBound = 0 }, "depth bound"},
		{"no proposal revision", func(r *simassign.Request) { r.ProposalRevisionID = "" }, "proposal revision id"},
		{"zero occupancy", func(r *simassign.Request) { r.Occupancy.Heads = 0 }, "heads must be positive"},
		{"expired reservation", func(r *simassign.Request) {
			r.ReservationExpiry = mustInstant(t, "2020-01-01T00:00:00Z")
		}, "known-at horizon"},
		{"no authority digest", func(r *simassign.Request) { r.AuthorityDigest = "" }, "authority digest"},
		{"no authority decision", func(r *simassign.Request) { r.AuthorityDecision = "" }, "source-authority decision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := simulateRequest(t, snap)
			tc.damage(&req)
			_, err := simassign.Simulate(req)
			if !errors.Is(err, simassign.ErrRequestInvalid) {
				t.Fatalf("Simulate returned %v, want ErrRequestInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Simulate said %q, which does not name %q", err, tc.want)
			}
		})
	}
}

// TestSimulateReadsTheVacancyOnlyWhenTheHeadIsGone proves the conditional
// vacancy input is honoured the way the snapshot's own specification declares
// it: irrelevant while a head is free, decisive once it is not.
func TestSimulateReadsTheVacancyOnlyWhenTheHeadIsGone(t *testing.T) {
	t.Parallel()

	free := simulate(t, newHarness(t), nil)
	if free.Occupancy.Verdict != simassign.OccupancyHeadAvailable {
		t.Fatalf("occupancy verdict is %s, want HEAD_AVAILABLE", free.Occupancy.Verdict)
	}
	occupancy, ok := free.Lookup(simassign.EffectPositionOccupancy)
	if !ok {
		t.Fatal("no occupancy transition was proposed")
	}
	for _, name := range occupancy.DerivedFrom {
		if name == promosnapshot.InputTargetPositionVacancy {
			t.Fatal("the occupancy effect cites the vacancy input while a head is free")
		}
	}

	// With the only head taken by an occupant whose placement ends after the
	// promotion date, the position is full and the date it frees up is exactly
	// what the refusal has to name.
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Occupants = []position.Occupant{{
		Worker:    managerRef(),
		FTE:       mustDecimal(t, "1.0000", 4),
		Effective: mustClosedInterval(t, "2026-01-01", "2026-08-01"),
		Exclusive: true,
	}}
	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr != nil {
		t.Fatalf("Build with a full position: %v", buildErr)
	}
	full, err := simassign.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if full.Occupancy.Verdict != simassign.OccupancyAtCapacity {
		t.Fatalf("occupancy verdict is %s, want AT_CAPACITY", full.Occupancy.Verdict)
	}
	if !full.Occupancy.HasVacantAfter || full.Occupancy.VacantAfter.String() != "2026-08-01" {
		t.Fatalf("the vacancy date is %v, want 2026-08-01", full.Occupancy.VacantAfter)
	}
	if _, ok := full.Lookup(simassign.EffectPositionOccupancy); ok {
		t.Fatal("an occupancy transition was proposed into a full position")
	}
	if !hasRefusal(full, simassign.ReasonVacancyAfterEffectiveDate) {
		t.Fatalf("no vacancy refusal:\n%s", full.Explain())
	}
}

// TestExplainCarriesNoWithheldValue proves the explanation is safe to log: it
// reports what was refused and why, and never what it could not disclose.
func TestExplainCarriesNoWithheldValue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Worker = fixtures.WithheldSubject(req.Authorization.Worker, "policy:no_subject_disclosure")
	partial, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr == nil {
		t.Fatal("a build with a withheld subject was accepted")
	}
	result, err := simassign.Simulate(simulateRequest(t, partial))
	if err != nil {
		t.Fatalf("Simulate over a refused snapshot: %v", err)
	}
	explanation := result.Explain()
	for _, leaked := range []string{"OPS-HRBP2", "P2", "POS-HRBP-204"} {
		if strings.Contains(explanation, leaked) {
			t.Fatalf("Explain leaks the withheld value %q:\n%s", leaked, explanation)
		}
	}
	if !strings.Contains(explanation, string(simassign.ReasonInputWithheld)) {
		t.Fatalf("Explain does not report the refusal:\n%s", explanation)
	}
	if !strings.Contains(explanation, promosnapshot.InputCurrentPlacement) {
		t.Fatalf("Explain does not name the refused input:\n%s", explanation)
	}
}

// TestSimulateIsPureUnderConcurrency proves the function holds no state: many
// goroutines simulating the same request agree on the digest, byte for byte.
func TestSimulateIsPureUnderConcurrency(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))
	req := simulateRequest(t, snap)

	const runs = 32
	digests := make(chan string, runs)
	for range runs {
		go func() {
			result, err := simassign.Simulate(req)
			if err != nil {
				digests <- "error: " + err.Error()
				return
			}
			digests <- result.Digest
		}()
	}
	first := <-digests
	for range runs - 1 {
		if got := <-digests; got != first {
			t.Fatalf("concurrent simulations disagreed: %s vs %s", got, first)
		}
	}
	if strings.HasPrefix(first, "error:") {
		t.Fatalf("concurrent simulation failed: %s", first)
	}
}

// TestManagerRelationIsCarriedOnBothSides proves the assignment revision points
// at the relationship the org effect proposes, so the two halves of one manager
// move cannot name different relationships.
func TestManagerRelationIsCarriedOnBothSides(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)
	assignment, ok := result.Lookup(simassign.EffectAssignmentRevision)
	if !ok {
		t.Fatal("no assignment revision was proposed")
	}
	manager, ok := result.Lookup(simassign.EffectManagerRelationship)
	if !ok {
		t.Fatal("no manager relationship change was proposed")
	}

	var assignmentAfter string
	for _, change := range assignment.Changes {
		if change.Field == string(people.FieldManagerRelation) {
			assignmentAfter = change.After
		}
	}
	var managerAfter string
	for _, change := range manager.Changes {
		if change.Field == "org.manager_relationship.relationship_id" {
			managerAfter = change.After
		}
	}
	if assignmentAfter == "" || assignmentAfter != managerAfter {
		t.Fatalf("the assignment points at relationship %q while the org effect proposes %q",
			assignmentAfter, managerAfter)
	}
	if assignmentAfter != result.Chain.ProposedRelationshipID {
		t.Fatalf("neither half matches the chain projection's %q", result.Chain.ProposedRelationshipID)
	}
}

// mustClosedInterval builds a closed local-date interval on the fixture
// calendar.
func mustClosedInterval(t testing.TB, start, end string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewLocalDateInterval(mustLocalDate(t, start), mustLocalDate(t, end), mustCalendar(t))
	if err != nil {
		t.Fatalf("NewLocalDateInterval(%q, %q): %v", start, end, err)
	}
	return interval
}
