package simcomp_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcomp"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// referenceWorkflowDoc is the promote-into-management reference workflow, which
// is where the effect set the two simulations must jointly produce is declared.
const referenceWorkflowDoc = "../../../../planning/reference-workflows/promote-into-management.md"

// commitBoundaryEffects maps each participant the reference workflow names
// inside its one ACID commit onto the effect kind that proposes it.
//
// The mapping is written out rather than derived from the token, so that
// renaming an effect kind cannot silently re-point it at a different
// participant, and so that a participant added to the reference workflow shows
// up here as an unmapped bullet rather than as a quietly missing effect.
var commitBoundaryEffects = map[string]simassign.EffectKind{
	"employment/assignment job and level":                                       simassign.EffectAssignmentRevision,
	"position reservation consumption and occupancy":                            simassign.EffectPositionOccupancy,
	"manager/organization relationship":                                         simassign.EffectManagerRelationship,
	"compensation effective interval":                                           simcomp.EffectCompensationRevision,
	"budget reservation consumption (only if locally authoritative/co-located)": simcomp.EffectBudgetReservation,
}

// TestTodo_PROMO_002_PROMO_003_ReferenceWorkflowEffectSet proves the two halves
// of the promotion simulation are jointly complete and jointly minimal: the
// union of the effects PROMO-002 and PROMO-003 propose over one snapshot is
// exactly the participant set the Promotion reference workflow declares inside
// its commit boundary -- no participant unaccounted for, and nothing invented
// that the reference workflow does not name.
//
// The expected set is read out of the reference workflow document itself rather
// than restated here, so a change to the declared boundary fails this test
// instead of silently diverging from the two implementations.
//
// (internal/workflow/conformance publishes no Promotion suite; the Promotion
// reference workflow's own compiled definition lives in internal/workflow, and
// its P1A slice declares zero executed effects, which this test also pins.)
func TestTodo_PROMO_002_PROMO_003_ReferenceWorkflowEffectSet(t *testing.T) {
	t.Parallel()

	declared := declaredCommitBoundary(t)
	if got, want := len(declared), len(commitBoundaryEffects); got != want {
		t.Fatalf("the reference workflow declares %d commit participants, this lane maps %d: %v",
			got, want, declared)
	}

	want := make([]simassign.EffectKind, 0, len(declared))
	for _, bullet := range declared {
		kind, ok := commitBoundaryEffects[bullet]
		if !ok {
			t.Fatalf("the reference workflow declares commit participant %q, which no simulation proposes", bullet)
		}
		want = append(want, kind)
	}
	slices.Sort(want)

	assignment := simulateAssignment(t)
	compensation := simulate(t, newHarness(t), nil)

	got := make([]simassign.EffectKind, 0, len(want))
	for _, effect := range assignment.Effects {
		got = append(got, effect.Kind)
	}
	for _, effect := range compensation.Effects {
		got = append(got, effect.Kind)
	}
	slices.Sort(got)

	if len(got) != len(want) {
		t.Fatalf("the two simulations jointly propose %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the two simulations jointly propose %v, want %v", got, want)
		}
	}

	// The two halves must also agree on one effect identity space: no kind is
	// proposed by both, and no effect id collides.
	ids := map[string]simassign.EffectKind{}
	for _, effect := range append(slices.Clone(assignment.Effects), compensation.Effects...) {
		if previous, dup := ids[effect.EffectID]; dup {
			t.Fatalf("effect id %q is proposed by both %s and %s", effect.EffectID, previous, effect.Kind)
		}
		ids[effect.EffectID] = effect.Kind
	}

	// The reference workflow qualifies the budget participant with "only if
	// locally authoritative"; the fixture's pool is an external observation, so
	// that one effect -- and only that one -- must fall outside the local
	// commit.
	local, external := 0, 0
	for _, effect := range append(slices.Clone(assignment.Effects), compensation.Effects...) {
		if effect.Local {
			local++
			continue
		}
		external++
		if effect.Kind != simcomp.EffectBudgetReservation {
			t.Fatalf("effect %s is outside the local commit boundary; only the budget reservation may be",
				effect.Kind)
		}
	}
	if local != 4 || external != 1 {
		t.Fatalf("%d local and %d external effects, want 4 and 1", local, external)
	}
}

// TestJointApprovalTokensMatchTheReferenceWorkflow proves the approval a band
// finding triggers is the same requirement the reference workflow's approval
// graph names, so the domain simulation and the workflow cannot drift into two
// different finance-partner approvals.
func TestJointApprovalTokensMatchTheReferenceWorkflow(t *testing.T) {
	t.Parallel()
	if got, want := simcomp.ApprovalFinancePartner, workflow.PromotionApprovalFinance; got != want {
		t.Fatalf("this lane triggers %q, the reference workflow declares %q", got, want)
	}

	definition := workflow.PromotionReferenceDefinition()
	if definition.WorkflowID != workflow.PromotionWorkflowID {
		t.Fatalf("the reference workflow identifies as %q", definition.WorkflowID)
	}
	// The P1A slice of the reference workflow is simulate-only, which is what
	// makes a zero-effect simulation the correct shape for both halves.
	if len(definition.DeclaredModes) != 1 || definition.DeclaredModes[0] != workflow.ModeSimulate {
		t.Fatalf("the reference workflow declares modes %v, want simulate only", definition.DeclaredModes)
	}
	found := false
	for _, approval := range definition.ApprovalRequirements {
		if approval.ID == workflow.PromotionApprovalFinance {
			found = true
		}
	}
	if !found {
		t.Fatal("the reference workflow no longer declares the finance partner approval")
	}
}

// declaredCommitBoundary reads the reference workflow's authoritative commit
// boundary block and returns its participant bullets, in document order.
func declaredCommitBoundary(t testing.TB) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(referenceWorkflowDoc))
	if err != nil {
		t.Fatalf("read the Promotion reference workflow at %s: %v", referenceWorkflowDoc, err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	const heading = "## Authoritative commit boundary"
	start := strings.Index(text, heading)
	if start < 0 {
		t.Fatalf("%s declares no authoritative commit boundary", referenceWorkflowDoc)
	}
	section := text[start:]
	if end := strings.Index(section[len(heading):], "\n## "); end >= 0 {
		section = section[:len(heading)+end]
	}

	var out []string
	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "+-- ") {
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, "+-- ")))
	}
	if len(out) == 0 {
		t.Fatalf("%s lists no commit participants", referenceWorkflowDoc)
	}
	return out
}

// simulateAssignment runs the PROMO-002 half over the same fixture snapshot, so
// the union compared above is the union of one promotion's two simulations
// rather than of two unrelated ones.
func simulateAssignment(t testing.TB) simassign.Result {
	t.Helper()
	snap := newHarness(t).build(t, fixtureRequest(t))
	result, err := simassign.Simulate(simassign.Request{
		Snapshot: snap,
		Target: simassign.Target{
			JobCode: fixtureTargetJob,
			Grade:   fixtureTargetGrade,
			OrgUnit: fixtureTargetOrg,
			PayZone: fixtureTargetZone,
		},
		ProposedManager:    proposedManagerRef(),
		ChainDepthBound:    3,
		ProposalRevisionID: fixtureRevisionID,
		ProposalDigest:     fixtureProposalDigest,
		Occupancy:          simassign.Occupancy{FTE: mustDecimal(t, "1.0000", 4), Heads: 1},
		ReservationExpiry:  mustInstant(t, fixtureReservationExpiry),
		AuthorityDigest:    fixtureAuthorityDigest,
		AuthorityDecision:  "PEOPLE_ASSIGNMENT_WRITE_ALLOWED",
	})
	if err != nil {
		t.Fatalf("simassign.Simulate: %v", err)
	}
	if !result.Executable() {
		t.Fatalf("the assignment half was refused: %v", result.Err())
	}
	return result
}
