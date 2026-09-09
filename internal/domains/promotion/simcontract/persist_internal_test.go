package simcontract

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

// minimalValidInput builds the smallest AssembleInput that validates, tagged
// so two calls with different tags produce two genuinely different, each
// individually self-consistent artifacts. It lives in this internal
// white-box test file because the richer Promotion/Manager Change fixtures
// belong to package simcontract_test.
func minimalValidInput(t testing.TB, tag string) AssembleInput {
	t.Helper()
	tenant := values.TenantId("harborcare-demo")
	worker := values.EntityRef{Tenant: tenant, Kind: "worker", Id: fixtureUUID(tag)}
	key, err := values.NewResourceKey(tenant, "worker_assignment", worker.Id)
	if err != nil {
		t.Fatalf("NewResourceKey: %v", err)
	}
	revision, err := values.NewSequenceRevision("people.assignment."+worker.Id, 1)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	effectID := "people.assignment.revision@" + tag
	effect, err := NewSideEffect(effectID, "people.assignment.revision", "people.assignment",
		"people.assignment/"+worker.Id, "REVERSIBLE", "people.assignment.supersede", "observe.people.assignment_effective")
	if err != nil {
		t.Fatalf("NewSideEffect: %v", err)
	}
	return AssembleInput{
		Intent: IntentRef{
			IntentID:      "intent-" + tag,
			IntentType:    "hcmnext.people.promote_worker",
			IntentVersion: "v1",
		},
		Snapshot: SnapshotRef{
			SnapshotDigest: "sha256:fixture-snapshot-" + tag,
			Tenant:         tenant,
			Subject:        worker,
		},
		ProposalCandidateDigest: "sha256:fixture-candidate-" + tag,
		Reads:                   []intent.PlannedRead{{ResourceKey: key, ExpectedRevision: revision}},
		Writes: []intent.PlannedWrite{{
			Subject:                 intent.SubjectReference{Kind: "worker", SubjectID: worker.Id, AuthorityDomain: "people"},
			ResourceKey:             key,
			FieldPath:               "people.job_code",
			CurrentCanonicalText:    "A-" + tag,
			ProposedCanonicalText:   "B-" + tag,
			SourceAuthorityDecision: "PEOPLE_ASSIGNMENT_WRITE_ALLOWED",
			ExpectedRevision:        revision,
		}},
		Streams: []intent.PlanParticipant{
			{ParticipantID: "people.assignment", StreamID: "people.assignment." + worker.Id, StorageClass: "LOCAL_EVENT_STREAM", Local: true},
		},
		Conflicts:        []conflict.Candidate{},
		Approvals:        []decision.ApprovalRequirement{},
		Authority:        []evidence.SourceAuthority{{Kind: evidence.AuthorityLocal, System: "hcmnext.people", PolicyRef: "people.assignment.write/2026.1"}},
		LegalObligations: []decision.Obligation{},
		SideEffects:      []SideEffect{effect},
		Repair:           []intent.CompensationBinding{{EffectID: effectID, Strategy: "SUPERSEDING_REVISION", RepairPlanID: "people.assignment.supersede"}},
		Cost:             Cost{State: CostNone},
		Completion:       Completion{State: CompletionReady, Detail: "minimal fixture requires no approval"},
		Revalidation:     Revalidation{Rules: []string{"people.promotion.preflight.rules/1.0.0"}, ControlSnapshotDigest: "sha256:fixture-control-" + tag},
	}
}

// fixtureUUID builds a deterministic, canonically-shaped UUID from a short
// tag, so two calls with different tags address two distinct entities and
// [values.EntityRef.Validate] accepts the result.
func fixtureUUID(tag string) string {
	padded := (tag + "00000000")[:8]
	return padded + "-" + padded[:4] + "-4" + padded[:3] + "-8" + padded[:3] + "-" + padded + padded[:4]
}

// TestMemoryStoreDigestConflict exercises MemoryStore's digest-conflict
// branch directly. It seeds the store's unexported map with one valid,
// self-consistent artifact under a key that a second, genuinely different,
// equally self-consistent artifact is then made to carry -- the only way to
// present that shape at all, since Store's own VerifyDigest call refuses a
// result whose Digest field does not match its own content before the map is
// ever consulted (see contract_test.go's tampered-digest case for that
// guard). A digest collision between two *independently* honest artifacts is
// exactly the bug this branch exists to catch rather than paper over.
func TestMemoryStoreDigestConflict(t *testing.T) {
	a, err := Assemble(minimalValidInput(t, "a"))
	if err != nil {
		t.Fatalf("Assemble(a): %v", err)
	}
	b, err := Assemble(minimalValidInput(t, "b"))
	if err != nil {
		t.Fatalf("Assemble(b): %v", err)
	}
	if a.Digest == b.Digest {
		t.Fatal("the two fixtures accidentally share a digest before the test could force it")
	}

	// Seed the map directly under a's digest with b's genuinely different
	// content -- the shape a canonical-encoding bug would produce -- rather
	// than tampering with either artifact's own Digest field, which
	// VerifyDigest would refuse before Store ever consulted the map.
	store := NewMemoryStore()
	store.items[a.Digest] = b

	_, _, err = store.Store(context.Background(), a)
	if !errors.Is(err, ErrDigestConflict) {
		t.Fatalf("Store error = %v, want errors.Is ErrDigestConflict", err)
	}
	if string(store.items[a.Digest].Canonical()) != string(b.Canonical()) {
		t.Fatal("the conflict overwrote the originally seeded artifact")
	}
}
