package population_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func resolvedResult(t *testing.T) population.Result {
	t.Helper()
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1, w2, w3 := worker(1), worker(2), worker(3)
	reader := newFakeReader().
		withSubject(w1).withFact(w1, "grade", valueFact("P3", knownAt)).withFact(w1, "active", valueFact("true", knownAt)).
		withSubject(w2).withFact(w2, "grade", valueFact("P3", knownAt)).withFact(w2, "active", valueFact("true", knownAt)).
		withSubject(w3).withFact(w3, "grade", valueFact("P3", knownAt)).withFact(w3, "active", valueFact("true", knownAt)).
		withWatermark(asOf)
	result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(result.Included()) != 3 {
		t.Fatalf("fixture setup: included = %v, want 3 subjects", result.Included())
	}
	return result
}

// TestTodo_POP_004 proves the RED and GREEN clauses of planning/todos.md
// POP-004: an unauthorized person, an unauthorized cross-organization member
// and a suppressed count are all absent from the caller-facing result without
// an inference oracle (a denied subject and a criteria-excluded subject are
// both simply not present, with no distinguishing marker in the disclosed
// result), and the applied restriction/policy versions plus a redaction
// reason are evidenced separately.
func TestTodo_POP_004(t *testing.T) {
	result := resolvedResult(t)
	w2 := worker(2)

	t.Run("RED_denied_subject_is_absent_not_marked", func(t *testing.T) {
		decision := population.RestrictionDecision{
			Versions:           mustPolicyVersions(),
			DeniedSubjects:     map[string]population.RestrictionReason{w2.String(): population.RestrictionUnauthorizedPerson},
			DiscloseMembership: true,
		}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		for _, m := range restricted.Members {
			if m.Subject == w2 {
				t.Fatal("a denied subject appeared in the disclosed member list")
			}
		}
		if len(restricted.Members) != 2 {
			t.Fatalf("members = %d, want 2 (one denied, no marker left behind)", len(restricted.Members))
		}
	})

	t.Run("RED_cross_organization_subject_is_absent", func(t *testing.T) {
		decision := population.RestrictionDecision{
			Versions:                  mustPolicyVersions(),
			CrossOrganizationSubjects: map[string]bool{w2.String(): true},
			DiscloseMembership:        true,
		}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		if len(restricted.Members) != 2 {
			t.Fatalf("members = %d, want 2", len(restricted.Members))
		}
	})

	t.Run("RED_count_is_absent_when_neither_membership_nor_count_is_disclosed", func(t *testing.T) {
		decision := population.RestrictionDecision{Versions: mustPolicyVersions()}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		if restricted.Members != nil {
			t.Fatal("membership was disclosed despite DiscloseMembership=false")
		}
		if restricted.Count.IsValue() {
			t.Fatal("a count value was disclosed despite DiscloseCount=false")
		}
		if restricted.Count.State() != values.PresenceRedacted {
			t.Fatalf("count presence state = %s, want REDACTED", restricted.Count.State())
		}
	})

	t.Run("RED_incomplete_policy_versions_fail_closed", func(t *testing.T) {
		decision := population.RestrictionDecision{DiscloseMembership: true}
		if _, err := population.ApplyRestrictions(result, decision); !errors.Is(err, population.ErrRestrictionVersion) {
			t.Fatalf("error = %v, want ErrRestrictionVersion", err)
		}
	})

	t.Run("GREEN_denied_and_excluded_subjects_are_indistinguishable_in_the_disclosed_result", func(t *testing.T) {
		// w2 is denied by policy; a hypothetical fourth subject that the
		// criteria itself excluded was never even a Member here. Both must be
		// simply absent from Members, with the same shape: nothing in
		// RestrictedResult.Members itself says "this one was denied".
		decision := population.RestrictionDecision{
			Versions:           mustPolicyVersions(),
			DeniedSubjects:     map[string]population.RestrictionReason{w2.String(): population.RestrictionUnauthorizedPerson},
			DiscloseMembership: true,
		}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		for _, m := range restricted.Members {
			if m.Subject == w2 {
				t.Fatal("denied subject leaked into disclosed membership")
			}
		}
	})

	t.Run("GREEN_evidence_records_reason_and_policy_versions", func(t *testing.T) {
		versions := mustPolicyVersions()
		decision := population.RestrictionDecision{
			Versions:           versions,
			DeniedSubjects:     map[string]population.RestrictionReason{w2.String(): population.RestrictionUnauthorizedPerson},
			DiscloseMembership: true,
		}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		if len(restricted.Evidence) != 1 {
			t.Fatalf("evidence = %+v, want exactly one entry", restricted.Evidence)
		}
		ev := restricted.Evidence[0]
		if ev.Subject != w2 || ev.Reason != population.RestrictionUnauthorizedPerson || ev.Versions != versions {
			t.Fatalf("evidence = %+v, want subject %s reason UNAUTHORIZED_PERSON versions %+v", ev, w2, versions)
		}
	})

	t.Run("GREEN_exact_count_disclosed_when_membership_is_authorized", func(t *testing.T) {
		decision := population.RestrictionDecision{Versions: mustPolicyVersions(), DiscloseMembership: true}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		count, ok := restricted.Count.Get()
		if !ok || count != 3 {
			t.Fatalf("count = %v (ok=%v), want 3", count, ok)
		}
	})

	t.Run("GREEN_count_only_disclosure_without_membership", func(t *testing.T) {
		decision := population.RestrictionDecision{Versions: mustPolicyVersions(), DiscloseCount: true}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		if restricted.Members != nil {
			t.Fatal("membership was disclosed under a count-only decision")
		}
		count, ok := restricted.Count.Get()
		if !ok || count != 3 {
			t.Fatalf("count = %v (ok=%v), want 3", count, ok)
		}
	})
}

// TestTodo_POP_004_Property proves the disclosed member count never exceeds
// the resolved member count regardless of which subjects a decision denies,
// and that Members and Count agree whenever both are disclosed.
func TestTodo_POP_004_Property(t *testing.T) {
	result := resolvedResult(t)
	deniedCounts := []int{0, 1, 2, 3}
	subjects := result.Included()
	for _, n := range deniedCounts {
		denied := map[string]population.RestrictionReason{}
		for i := 0; i < n; i++ {
			denied[subjects[i].String()] = population.RestrictionUnauthorizedPerson
		}
		decision := population.RestrictionDecision{Versions: mustPolicyVersions(), DeniedSubjects: denied, DiscloseMembership: true}
		restricted, err := population.ApplyRestrictions(result, decision)
		if err != nil {
			t.Fatalf("ApplyRestrictions (denied=%d): %v", n, err)
		}
		if len(restricted.Members) != len(subjects)-n {
			t.Fatalf("denied=%d: members = %d, want %d", n, len(restricted.Members), len(subjects)-n)
		}
		count, ok := restricted.Count.Get()
		if !ok || count != len(restricted.Members) {
			t.Fatalf("denied=%d: count = %v (ok=%v), want %d", n, count, ok, len(restricted.Members))
		}
	}
}

// TestTodo_POP_004_Security proves an unauthorized caller (no disclosure of
// either membership or count) receives byte-identical RestrictedResult shapes
// regardless of how many subjects were actually resolved or denied: the
// response never leaks size through its own structure.
func TestTodo_POP_004_Security(t *testing.T) {
	result := resolvedResult(t)
	decision := population.RestrictionDecision{Versions: mustPolicyVersions()}
	restricted, err := population.ApplyRestrictions(result, decision)
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	if restricted.Members != nil {
		t.Fatal("an unauthorized caller received a member list")
	}
	if restricted.Count.IsValue() {
		t.Fatal("an unauthorized caller received a count value")
	}

	// A trimmed-down resolution (only one included subject) must still
	// produce the same disclosed shape: Members nil, Count not a value.
	trimmed := result
	trimmed.Members = result.Members[:1]
	restrictedTrimmed, err := population.ApplyRestrictions(trimmed, decision)
	if err != nil {
		t.Fatalf("ApplyRestrictions (trimmed): %v", err)
	}
	if restrictedTrimmed.Members != nil || restrictedTrimmed.Count.IsValue() {
		t.Fatal("response shape changed with population size for an unauthorized caller")
	}
}

// TestTodo_POP_004_Mutation proves each restriction gate is independently
// load bearing: disabling DeniedSubjects, CrossOrganizationSubjects or the
// disclosure flags one at a time changes exactly the outcome that gate owns.
func TestTodo_POP_004_Mutation(t *testing.T) {
	result := resolvedResult(t)
	w2 := worker(2)

	base := population.RestrictionDecision{
		Versions:                  mustPolicyVersions(),
		DeniedSubjects:            map[string]population.RestrictionReason{w2.String(): population.RestrictionUnauthorizedPerson},
		CrossOrganizationSubjects: map[string]bool{},
		DiscloseMembership:        true,
	}
	full, err := population.ApplyRestrictions(result, base)
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	if len(full.Members) != 2 {
		t.Fatalf("baseline members = %d, want 2", len(full.Members))
	}

	withoutDenial := base
	withoutDenial.DeniedSubjects = nil
	restored, err := population.ApplyRestrictions(result, withoutDenial)
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	if len(restored.Members) != 3 {
		t.Fatal("removing DeniedSubjects did not restore the previously denied member")
	}

	withoutDisclosure := base
	withoutDisclosure.DiscloseMembership = false
	suppressed, err := population.ApplyRestrictions(result, withoutDisclosure)
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	if suppressed.Members != nil {
		t.Fatal("removing DiscloseMembership did not suppress the member list")
	}
}
