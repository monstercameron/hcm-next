package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_EP_PROMO_001_Unit is the in-package half of EP-PROMO-001: the
// durable-candidate helpers derive stable identities, build the canonical
// bodies the snapshot and simulation rows store, and refuse to record a
// member the schema cannot express - while an engine composed with no
// durable database records nothing and fails nothing.
func TestTodo_EP_PROMO_001_Unit(t *testing.T) {
	tenantID := uuid.MustParse("aaaaaaaa-1111-4222-8333-444444444444")
	intentID := uuid.MustParse("bbbbbbbb-2222-4333-8444-555555555555")
	otherIntentID := uuid.MustParse("cccccccc-3333-4333-8444-666666666666")

	t.Run("candidate identities are derived, not allocated", func(t *testing.T) {
		first := proposalCandidateUUID(tenantID, intentID, "snapshot:"+intentcontrol.PurposeSimulation)
		again := proposalCandidateUUID(tenantID, intentID, "snapshot:"+intentcontrol.PurposeSimulation)
		if first != again {
			t.Fatalf("the same derivation minted two ids: %s vs %s", first, again)
		}
		if other := proposalCandidateUUID(tenantID, intentID, "simulation"); other == first {
			t.Fatal("the snapshot and simulation candidates share one id")
		}
		if other := proposalCandidateUUID(tenantID, otherIntentID, "snapshot:"+intentcontrol.PurposeSimulation); other == first {
			t.Fatal("two intents share one snapshot id")
		}
	})

	t.Run("the candidate digest is the body hash", func(t *testing.T) {
		body := []byte(`{"k":"v"}`)
		want := sha256.Sum256(body)
		if got := candidateDigest(body); got != hex.EncodeToString(want[:]) {
			t.Fatalf("candidateDigest = %q, want %q", got, hex.EncodeToString(want[:]))
		}
	})

	t.Run("the snapshot body carries the baselines the read produced", func(t *testing.T) {
		token, err := values.NewSequenceRevision("stream:worker:omar-reyes", 7)
		if err != nil {
			t.Fatalf("build the baseline token: %v", err)
		}
		rev := intent.ProposalRevision{
			ProposalRevisionID: "rev-1",
			Revision:           1,
			SourceBaselines: []intent.SourceBaseline{
				{StreamID: "stream:worker:omar-reyes", ExpectedRevision: token},
			},
		}
		inst := intent.Instance{IntentID: "i-1", Purpose: "P1A", CanonicalRequestDigest: digest.Reference{
			AlgorithmID: "sha256",
			Digest:      strings.Repeat("ab", 32),
		}}
		doc := proposalSnapshotDocument{
			IntentID:               inst.IntentID,
			Revision:               rev.Revision,
			RequestDigestAlgorithm: inst.CanonicalRequestDigest.AlgorithmID,
			RequestDigest:          inst.CanonicalRequestDigest.Digest,
			Purpose:                inst.Purpose,
			Subjects:               proposalSnapshotSubjects(inst),
			SourceBaselines:        proposalSnapshotBaselines(rev),
			ControlDigest:          controlSnapshotDigest(rev.ControlSnapshots),
		}
		body, err := doc.marshal()
		if err != nil {
			t.Fatalf("marshal the snapshot body: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("the snapshot body is not JSON: %v", err)
		}
		baselines, ok := decoded["source_baselines"].([]any)
		if !ok || len(baselines) != 1 {
			t.Fatalf("the snapshot body carries %v baselines, want the one the read produced", decoded["source_baselines"])
		}
		baseline := baselines[0].(map[string]any)
		if baseline["stream_id"] != "stream:worker:omar-reyes" || baseline["expected_revision"] != token.String() {
			t.Fatalf("the recorded baseline = %v, want (%s, %s)", baseline, "stream:worker:omar-reyes", token.String())
		}
	})

	t.Run("the simulation body names what the result claims", func(t *testing.T) {
		artifact := &intentsv1.SimulationArtifact{
			PlannedWrites: []*intentsv1.PlannedWrite{
				{Operation: "WRITE_SET", TargetRef: "worker:omar-reyes.job"},
			},
			Findings: []*intentsv1.Finding{{Code: "F-1"}, {Code: "F-2"}},
		}
		if got := proposalPlannedWrites(artifact); len(got) != 1 || got[0] != "WRITE_SET worker:omar-reyes.job" {
			t.Fatalf("planned writes = %v", got)
		}
		if got := proposalFindingCodes(artifact); len(got) != 2 || got[0] != "F-1" || got[1] != "F-2" {
			t.Fatalf("finding codes = %v", got)
		}
		if proposalPlannedWrites(nil) != nil || proposalFindingCodes(nil) != nil {
			t.Fatal("a nil artifact produced non-nil members")
		}
	})

	t.Run("set mapping keeps every member the schema can express", func(t *testing.T) {
		token, err := values.NewSequenceRevision("stream:x", 1)
		if err != nil {
			t.Fatalf("build the token: %v", err)
		}
		resourceKey, err := values.NewResourceKey(values.TenantId("tenant-1"), values.Kind("worker"), "job")
		if err != nil {
			t.Fatalf("build the resource key: %v", err)
		}
		interval, err := values.NewOpenInstantInterval(values.NewInstant(time.Unix(1_700_000_000, 0)))
		if err != nil {
			t.Fatalf("build the interval: %v", err)
		}
		rev := intent.ProposalRevision{
			ProposalRevisionID: "rev-1",
			Writes: []intent.PlannedWrite{{
				Subject:                 intent.SubjectReference{Kind: "WORKER", SubjectID: "omar-reyes", AuthorityDomain: "workforce"},
				ResourceKey:             resourceKey,
				FieldPath:               "job",
				CurrentCanonicalText:    "eng",
				ProposedCanonicalText:   "senior eng",
				SourceAuthorityDecision: "authority.local_master/v1",
				ExpectedRevision:        token,
				Operation:               intent.WriteOperationUpdate,
				EffectiveInterval:       interval,
			}},
			RequiredApprovals: []intent.RequiredApproval{
				{RequirementID: "req-1", SeparationConstraint: "not_requester"},
			},
		}
		sets, err := proposalCandidateSets(rev)
		if err != nil {
			t.Fatalf("map the sets: %v", err)
		}
		if len(sets.Writes) != 1 || len(sets.Approvals) != 1 {
			t.Fatalf("sets = %+v", sets)
		}
		if sets.Approvals[0].MaterialityClass != intentcontrol.Material {
			t.Fatalf("the approval is %q materiality", sets.Approvals[0].MaterialityClass)
		}
	})

	t.Run("an obligation the schema cannot hold is an error, not a dropped row", func(t *testing.T) {
		rev := intent.ProposalRevision{
			ProposalRevisionID: "rev-ob",
			Obligations:        []intent.Obligation{{ObligationID: "ob-1", Kind: "notice"}},
		}
		if _, err := proposalCandidateSets(rev); err == nil || !strings.Contains(err.Error(), "obligation") {
			t.Fatalf("proposalCandidateSets = %v, want an obligation refusal", err)
		}
	})

	t.Run("nothing records without a minted revision or a database", func(t *testing.T) {
		engine := newJourneyEngine(nil, nil, "", nil, nil)
		if err := engine.recordProposalCandidates(context.Background(), nil, intent.Instance{}, simulationResult{}); err != nil {
			t.Fatalf("a blocked simulation recorded: %v", err)
		}
		rev := &intent.ProposalRevision{ProposalRevisionID: "rev-1", Revision: 1}
		if err := engine.recordProposalCandidates(context.Background(), nil, intent.Instance{}, simulationResult{Revision: rev}); err != nil {
			t.Fatalf("a database-free engine failed rather than recording nothing: %v", err)
		}
	})
}
