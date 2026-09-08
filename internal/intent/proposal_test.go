package intent_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// promoteProposal builds a complete, valid proposal specification for a
// simulated promotion: current and proposed state, one planned write with its
// authority decision and baseline, a child intent binding, a reservation, the
// required approval, the revalidation plan and the pinned control context.
func promoteProposal(t *testing.T, intentID string) intent.ProposalSpec {
	t.Helper()
	key := mustResourceKey(t, "employment", "9001", "primary")
	return intent.ProposalSpec{
		IntentID:            intentID,
		Revision:            1,
		Tenant:              values.TenantId("acme-eu"),
		OrganizationScopeID: "org:acme-eu:engineering",
		LegalEntityID:       "legal:acme-eu-gmbh",
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
		},
		EffectiveTime: mustInterval(t),
		CurrentState: []intent.StateAssertion{{
			Subject:     intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey: key, FieldPath: "assignment.position_ref",
			CanonicalText: "position:senior-engineer",
		}},
		ProposedState: []intent.StateAssertion{{
			Subject:     intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey: key, FieldPath: "assignment.position_ref",
			CanonicalText: "position:staff-engineer",
		}},
		Writes: []intent.PlannedWrite{{
			Subject:     intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"},
			ResourceKey: key, FieldPath: "assignment.position_ref",
			CurrentCanonicalText:    "position:senior-engineer",
			ProposedCanonicalText:   "position:staff-engineer",
			SourceAuthorityDecision: "authority.local_master/v1",
			ExpectedRevision:        mustSequenceRevision(t, "people.employment.9001", 42),
		}},
		Children: []intent.ChildIntentBinding{{
			Definition:          intent.Ref{TypeID: "hcmnext.rewards.change_base_pay", Version: 1},
			ChildIntentID:       "child:1",
			Ordinal:             1,
			MaterialInputDigest: "child-material-1",
		}},
		Reservations: []intent.Reservation{{
			ReservationID: "reservation:1", Kind: "BUDGET",
			Expiry: startOfNextYear(),
		}},
		RequiredApprovals: []intent.RequiredApproval{{
			RequirementID: "req.promotion_manager/v1", SeparationConstraint: "not_requester",
		}},
		SourceBaselines: []intent.SourceBaseline{{
			StreamID:         "people.employment.9001",
			ExpectedRevision: mustSequenceRevision(t, "people.employment.9001", 42),
		}},
		Attachments: []intent.AttachmentRef{{
			ArtifactID: "artifact:justification", AlgorithmID: "sha256", Digest: "abcd",
		}},
		Purpose: intent.PurposeDecision{
			Purpose:        "promotion.annual_cycle",
			RecipientRef:   "recipient:hr-ops",
			DestinationRef: "destination:internal",
			ResidencyRef:   "residency:eu",
		},
		Revalidation:     intent.RevalidationPlan{Rules: []string{"promotion_execution_revalidation/v1"}},
		ControlSnapshots: controlSnapshots(),
		CreatedBy:        principal(),
		DetectedChildRefs: []intent.Ref{
			{TypeID: "hcmnext.rewards.change_base_pay", Version: 1},
		},
	}
}

// TestTodo_INTENT_005 is the PRIMARY test for immutable ProposalRevision
// artifacts.
//
// RED: an in-place edit, a caller-provided digest, a missing control, source or
// reference version, and a hidden material child intent are all rejected.
//
// GREEN: each revision carries the exact current and proposed state, the child
// set, writes, effects, approvals, obligations, reservations, cost and
// revalidation, plus a canonical digest computed under the PROPOSAL profile.
func TestTodo_INTENT_005(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	t.Run("a caller cannot provide the digest", func(t *testing.T) {
		// The specification type has no digest field at all, so a
		// caller-provided digest is unrepresentable rather than merely
		// rejected. That is the strongest form this rule can take.
		specType := reflect.TypeOf(intent.ProposalSpec{})
		for i := 0; i < specType.NumField(); i++ {
			f := specType.Field(i)
			if f.Type == reflect.TypeOf(digest.Reference{}) {
				t.Fatalf("ProposalSpec carries a digest field %q; a caller must never supply one",
					f.Name)
			}
		}
	})

	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.ProposalSpec)
			cause  error
		}{
			{"missing control snapshots", func(s *intent.ProposalSpec) {
				s.ControlSnapshots = intent.ControlSnapshots{}
			}, intent.ErrInvalidProposal},
			{"partial control snapshots", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.ReferenceDataDigest = ""
			}, intent.ErrInvalidProposal},
			{"planned write with no source-authority decision", func(s *intent.ProposalSpec) {
				s.Writes[0].SourceAuthorityDecision = ""
			}, intent.ErrInvalidProposal},
			{"planned write with no baseline revision", func(s *intent.ProposalSpec) {
				s.Writes[0].ExpectedRevision = values.UnspecifiedRevision()
			}, intent.ErrInvalidProposal},
			{"child binding with no material input digest", func(s *intent.ProposalSpec) {
				s.Children[0].MaterialInputDigest = ""
			}, intent.ErrInvalidProposal},
			{"reservation that never expires", func(s *intent.ProposalSpec) {
				s.Reservations[0].Expiry = values.Instant{}
			}, intent.ErrInvalidProposal},
			{"no effective time", func(s *intent.ProposalSpec) {
				s.EffectiveTime = values.EffectiveInterval{}
			}, intent.ErrInvalidProposal},
			{"no creator", func(s *intent.ProposalSpec) {
				s.CreatedBy = intent.PrincipalReference{}
			}, intent.ErrInvalidInstance},
			{"revision numbered from zero", func(s *intent.ProposalSpec) {
				s.Revision = 0
			}, intent.ErrInvalidProposal},
			{"hidden material child intent", func(s *intent.ProposalSpec) {
				s.DetectedChildRefs = append(s.DetectedChildRefs,
					intent.Ref{TypeID: "hcmnext.rewards.reserve_compensation_budget", Version: 1})
			}, intent.ErrHiddenChildIntent},
			{"declared effect with no compensation", func(s *intent.ProposalSpec) {
				s.Effects = []intent.PlannedEffect{{
					EffectID: "effect:notify", Kind: "EXTERNAL", ObservationRef: "observe:1",
				}}
			}, intent.ErrInvalidProposal},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec := promoteProposal(t, "intent:1")
				tc.break_(&spec)
				if _, err := intent.NewProposalRevision(spec, def, d, nil, fixedClock()); !errors.Is(err, tc.cause) {
					t.Fatalf("invalid revision accepted, or wrong cause: err=%v want %v", err, tc.cause)
				}
			})
		}

		t.Run("an in-place edit is refused by the ledger", func(t *testing.T) {
			ledger := intent.NewProposalLedger("intent:1")
			first, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
				countingIDs("aaaaaaaa"), fixedClock())
			if err != nil {
				t.Fatalf("mint first revision: %v", err)
			}
			if err := ledger.Append(first); err != nil {
				t.Fatalf("append first revision: %v", err)
			}
			// Re-recording the same revision is a rewrite.
			if err := ledger.Append(first); !errors.Is(err, intent.ErrProposalImmutable) {
				t.Fatalf("the ledger accepted a rewrite: %v", err)
			}
			// A second revision must be numbered next and must link what it
			// supersedes; anything else is an edit wearing a new id.
			spec := promoteProposal(t, "intent:1")
			spec.Revision = 1
			edited, err := intent.NewProposalRevision(spec, def, d, countingIDs("bbbbbbbb"), fixedClock())
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			if err := ledger.Append(edited); !errors.Is(err, intent.ErrProposalImmutable) {
				t.Fatalf("the ledger accepted a re-numbered revision 1: %v", err)
			}
			spec.Revision = 2
			unlinked, err := intent.NewProposalRevision(spec, def, d, countingIDs("cccccccc"), fixedClock())
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			if err := ledger.Append(unlinked); !errors.Is(err, intent.ErrProposalImmutable) {
				t.Fatalf("the ledger accepted a revision that supersedes nothing: %v", err)
			}
			if ledger.Len() != 1 {
				t.Fatalf("the ledger holds %d revisions after rejected appends", ledger.Len())
			}
		})

		t.Run("a revision with no minted digest cannot be recorded", func(t *testing.T) {
			ledger := intent.NewProposalLedger("intent:1")
			rev, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
				countingIDs("dddddddd"), fixedClock())
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			rev.MaterialDigest = digest.Reference{}
			if err := ledger.Append(rev); !errors.Is(err, intent.ErrInvalidProposal) {
				t.Fatalf("the ledger accepted a revision with no digest: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		spec := promoteProposal(t, "intent:1")
		rev, err := intent.NewProposalRevision(spec, def, d, countingIDs("01234567"), fixedClock())
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if len(rev.CurrentState) != 1 || len(rev.ProposedState) != 1 {
			t.Fatalf("the revision does not carry the exact current and proposed state")
		}
		if rev.CurrentState[0].CanonicalText == rev.ProposedState[0].CanonicalText {
			t.Fatalf("current and proposed state are identical")
		}
		for name, n := range map[string]int{
			"writes":       len(rev.Writes),
			"children":     len(rev.Children),
			"approvals":    len(rev.RequiredApprovals),
			"reservations": len(rev.Reservations),
			"baselines":    len(rev.SourceBaselines),
			"revalidation": len(rev.Revalidation.Rules),
		} {
			if n == 0 {
				t.Fatalf("the revision carries no %s", name)
			}
		}
		if rev.MaterialDigest.Digest == "" {
			t.Fatalf("the revision carries no material digest")
		}
		if got, want := rev.MaterialDigest.ProfileID, digest.ProfileProposal; got != want {
			t.Fatalf("material digest minted under profile %q, want %q", got, want)
		}
		if err := d.VerifyProposalDigest(rev); err != nil {
			t.Fatalf("the minted material digest does not verify: %v", err)
		}
		if rev.MaterialDigest.ProposalRevisionID == nil ||
			*rev.MaterialDigest.ProposalRevisionID != rev.ProposalRevisionID {
			t.Fatalf("the digest is not scope-bound to its own revision")
		}
		if len(rev.MaterialPayload().WireBytes) == 0 {
			t.Fatalf("the material payload is empty")
		}
	})

	t.Run("control snapshots are revalidated context, not material", func(t *testing.T) {
		// This is the materiality rule: republishing a policy bundle, a
		// classification taxonomy or a reference dataset changes the recorded
		// context but not the digest an approval bound to, so a compensation
		// cycle with a thousand pending approvals survives a policy republish.
		spec := promoteProposal(t, "intent:1")
		before, err := intent.NewProposalRevision(spec, def, d, countingIDs("aaaaaaaa"), fixedClock())
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		republished := spec
		republished.ControlSnapshots.PolicyBundleDigest = "policy-bundle-2"
		republished.ControlSnapshots.ClassificationTaxonomyDigest = "taxonomy-2"
		republished.ControlSnapshots.ReferenceDataDigest = "reference-2"
		republished.ControlSnapshots.DLPDecisionDigest = "dlp-2"
		after, err := intent.NewProposalRevision(republished, def, d, countingIDs("aaaaaaaa"), fixedClock())
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if !intent.MaterialEqual(before, after) {
			t.Fatalf("a control-snapshot republish changed the material content")
		}
		if before.MaterialDigest.Digest != after.MaterialDigest.Digest {
			t.Fatalf("a control-snapshot republish moved the material proposal digest")
		}
	})

	t.Run("large inputs travel as immutable references", func(t *testing.T) {
		spec := promoteProposal(t, "intent:1")
		rev, err := intent.NewProposalRevision(spec, def, d, nil, fixedClock())
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		for _, a := range rev.Attachments {
			if a.Digest == "" || a.AlgorithmID == "" {
				t.Fatalf("attachment %q is not content-addressed", a.ArtifactID)
			}
		}
	})
}

// TestTodo_INTENT_005_Golden pins the material digest and the material byte
// length of a fixed proposal. Any change to the material encoding — which would
// invalidate every stored approval — shows up here.
func TestTodo_INTENT_005_Golden(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")
	rev, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
		countingIDs("01234567"), fixedClock())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	goldenText(t, "intent_005_material.bin", rev.MaterialPayload().WireBytes)
	goldenJSON(t, "intent_005_digest.json", rev.MaterialDigest)
}

// TestTodo_INTENT_005_Mutation perturbs one material field at a time and
// requires the digest to move, then perturbs the revalidated context and
// requires it to stay put. A material field the digest ignores would let an
// approval survive the very change it exists to catch.
func TestTodo_INTENT_005_Mutation(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	baseline, err := intent.NewProposalRevision(promoteProposal(t, "intent:1"), def, d,
		countingIDs("aaaaaaaa"), fixedClock())
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	material := []struct {
		name   string
		mutate func(*intent.ProposalSpec)
	}{
		{"the proposed value", func(s *intent.ProposalSpec) {
			s.Writes[0].ProposedCanonicalText = "position:principal-engineer"
			s.ProposedState[0].CanonicalText = "position:principal-engineer"
		}},
		{"the written field", func(s *intent.ProposalSpec) {
			s.Writes[0].FieldPath = "assignment.job_profile_ref"
		}},
		{"the subject set", func(s *intent.ProposalSpec) {
			s.Subjects = append(s.Subjects, intent.SubjectReference{
				Kind: "POSITION", SubjectID: "position:staff-engineer", AuthorityDomain: "POSITION",
			})
		}},
		{"the effective time", func(s *intent.ProposalSpec) {
			iv, err := values.NewOpenInstantInterval(startOfNextYear())
			if err != nil {
				t.Fatalf("interval: %v", err)
			}
			s.EffectiveTime = iv
		}},
		{"the child set", func(s *intent.ProposalSpec) {
			s.Children = nil
			s.DetectedChildRefs = nil
		}},
		{"a reservation", func(s *intent.ProposalSpec) {
			s.Reservations[0].ReservationID = "reservation:2"
		}},
		{"the required approvals", func(s *intent.ProposalSpec) {
			s.RequiredApprovals = append(s.RequiredApprovals, intent.RequiredApproval{
				RequirementID: "req.compensation_committee/v1",
			})
		}},
		{"a source-authority decision", func(s *intent.ProposalSpec) {
			s.Writes[0].SourceAuthorityDecision = "authority.external_master/v1"
		}},
		{"an expected stream sequence", func(s *intent.ProposalSpec) {
			s.SourceBaselines[0].ExpectedRevision = mustSequenceRevision(t, "people.employment.9001", 43)
		}},
		{"a material attachment hash", func(s *intent.ProposalSpec) {
			s.Attachments[0].Digest = "ffff"
		}},
		{"the destination and residency decision", func(s *intent.ProposalSpec) {
			s.Purpose.DestinationRef = "destination:external-provider"
			s.Purpose.ResidencyRef = "residency:us"
		}},
		{"the revalidation plan", func(s *intent.ProposalSpec) {
			s.Revalidation.Rules = nil
		}},
	}
	for _, m := range material {
		t.Run("material: "+m.name, func(t *testing.T) {
			spec := promoteProposal(t, "intent:1")
			m.mutate(&spec)
			got, err := intent.NewProposalRevision(spec, def, d, countingIDs("aaaaaaaa"), fixedClock())
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			if intent.MaterialEqual(baseline, got) {
				t.Fatalf("changing %s left the material content unchanged", m.name)
			}
			if got.MaterialDigest.Digest == baseline.MaterialDigest.Digest {
				t.Fatalf("changing %s left the material digest unchanged", m.name)
			}
		})
	}

	revalidated := []struct {
		name   string
		mutate func(*intent.ProposalSpec)
	}{
		{"the policy bundle", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-9"
		}},
		{"the capability registry", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.CapabilityRegistryDigest = "cap-registry-9"
		}},
		{"the classification taxonomy", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.ClassificationTaxonomyDigest = "taxonomy-9"
			s.ControlSnapshots.ClassificationLabelSetDigest = "labels-9"
		}},
		{"the reference dataset", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.ReferenceDataDigest = "reference-9"
		}},
		{"the DLP decision digest", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.DLPDecisionDigest = "dlp-9"
		}},
		{"the invalidator bookkeeping", func(s *intent.ProposalSpec) {
			s.InvalidatorRefs = []string{"invalidator:1"}
		}},
	}
	for _, m := range revalidated {
		t.Run("revalidated context: "+m.name, func(t *testing.T) {
			spec := promoteProposal(t, "intent:1")
			m.mutate(&spec)
			got, err := intent.NewProposalRevision(spec, def, d, countingIDs("aaaaaaaa"), fixedClock())
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			if !intent.MaterialEqual(baseline, got) {
				t.Fatalf("changing %s changed the material content", m.name)
			}
			if got.MaterialDigest.Digest != baseline.MaterialDigest.Digest {
				t.Fatalf("changing %s moved the material digest; it is revalidated context", m.name)
			}
		})
	}
}

// TestTodo_CONFLICT_003 proves the intent-layer portion of the write-fence
// contract. Commit-time enforcement belongs to the transaction coordinator;
// this package supplies the closed operation and effective interval material
// that coordinator will consume.
func TestTodo_CONFLICT_003(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	legacy := promoteProposal(t, "intent:1")
	legacyRev, err := intent.NewProposalRevision(legacy, def, d, countingIDs("aaaaaaaa"), fixedClock())
	if err != nil {
		t.Fatalf("mint legacy proposal: %v", err)
	}

	t.Run("typed write is material and complete", func(t *testing.T) {
		spec := promoteProposal(t, "intent:1")
		spec.Writes[0].Operation = intent.WriteOperationUpdate
		spec.Writes[0].EffectiveInterval = mustInterval(t)
		rev, err := intent.NewProposalRevision(spec, def, d, countingIDs("bbbbbbbb"), fixedClock())
		if err != nil {
			t.Fatalf("mint typed proposal: %v", err)
		}
		if rev.Writes[0].Operation != intent.WriteOperationUpdate || rev.Writes[0].EffectiveInterval != mustInterval(t) {
			t.Fatalf("typed write fields were not retained")
		}
		if intent.MaterialEqual(legacyRev, rev) || rev.MaterialDigest.Digest == legacyRev.MaterialDigest.Digest {
			t.Fatalf("typed write semantics did not change material identity")
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*intent.PlannedWrite)
	}{
		{name: "operation without interval", mutate: func(w *intent.PlannedWrite) { w.Operation = intent.WriteOperationUpdate }},
		{name: "interval without operation", mutate: func(w *intent.PlannedWrite) { w.EffectiveInterval = mustInterval(t) }},
		{name: "unknown operation", mutate: func(w *intent.PlannedWrite) { w.Operation = "PATCH"; w.EffectiveInterval = mustInterval(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := promoteProposal(t, "intent:1")
			tc.mutate(&spec.Writes[0])
			if _, err := intent.NewProposalRevision(spec, def, d, countingIDs("cccccccc"), fixedClock()); !errors.Is(err, intent.ErrInvalidProposal) {
				t.Fatalf("invalid typed write accepted: %v", err)
			}
		})
	}
}
