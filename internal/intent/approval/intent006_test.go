package approval_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// assessed records one decision on the fixture's first revision, mints a
// successor with the given mutation, and assesses whether the decision survives.
type assessed struct {
	Fixture    *approval.Fixture
	Decision   approval.ApprovalDecision
	From       intent.ProposalRevision
	To         intent.ProposalRevision
	Assessment approval.MaterialityAssessment
}

func assess(t *testing.T, mutate func(*intent.ProposalSpec), denies ...approval.MandatoryDeny) assessed {
	t.Helper()
	f := mustFixture(t)
	d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
		humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	from := f.Current()
	to, err := f.NextRevision(mutate)
	if err != nil {
		t.Fatalf("next revision: %v", err)
	}
	a, err := approval.Assess(approval.AssessRequest{
		DefinitionRef: f.Definition.Ref,
		From:          from,
		To:            to,
		Decisions:     []approval.ApprovalDecision{d},
		Denies:        denies,
		Clock:         approval.FixtureClock(),
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	return assessed{Fixture: f, Decision: d, From: from, To: to, Assessment: a}
}

// TestTodo_INTENT_006 is the PRIMARY test for invalidating approvals on a
// material proposal change and only then.
//
// RED: changing an amount, a field, an effective date, the child set, a
// reservation, the required approvals or a source-authority decision must not
// leave a prior approval valid; and republishing a policy bundle, a taxonomy or
// a reference dataset must not invalidate an approval whose material result is
// unchanged.
//
// GREEN: a new material revision invalidates the bindings for the old digest; a
// control-snapshot change produces exactly one of APPROVAL_STANDS, NEW_REVISION
// or BLOCKED; and a thousand pending approvals survive a policy republish with
// zero spurious invalidations.
func TestTodo_INTENT_006(t *testing.T) {
	t.Run("RED: a material change never leaves an approval valid", func(t *testing.T) {
		cases := []struct {
			name   string
			field  string
			mutate func(*intent.ProposalSpec)
		}{
			{"amount", "cost", func(s *intent.ProposalSpec) {
				m, err := values.NewMoney("21000.00", "EUR", 2, values.RoundingHalfEven)
				if err != nil {
					panic(err)
				}
				s.Cost = &m
			}},
			{"written field", "writes", func(s *intent.ProposalSpec) {
				s.Writes[0].FieldPath = "assignment.grade_ref"
			}},
			{"proposed value", "writes", func(s *intent.ProposalSpec) {
				s.Writes[0].ProposedCanonicalText = "position:senior-engineering-manager"
			}},
			{"effective date", "effective_time", func(s *intent.ProposalSpec) {
				iv, err := values.NewOpenInstantInterval(
					values.NewInstant(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
				if err != nil {
					panic(err)
				}
				s.EffectiveTime = iv
			}},
			{"child set", "children", func(s *intent.ProposalSpec) {
				child := intent.Ref{TypeID: "hcmnext.people.change_manager", Version: 1}
				s.Children = append(s.Children, intent.ChildIntentBinding{
					Definition: child, ChildIntentID: "child:manager", Ordinal: 2,
					MaterialInputDigest: "child-material-manager",
				})
				s.DetectedChildRefs = append(s.DetectedChildRefs, child)
			}},
			{"reservation", "reservations", func(s *intent.ProposalSpec) {
				s.Reservations[0].Expiry = values.NewInstant(
					time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
			}},
			{"required approvals", "required_approvals", func(s *intent.ProposalSpec) {
				s.RequiredApprovals = append(s.RequiredApprovals, intent.RequiredApproval{
					RequirementID:        humanwork.RequirementExecutiveCommittee,
					SeparationConstraint: "policy.promotion_separation_of_duties/2026.1",
				})
			}},
			{"source-authority decision", "writes", func(s *intent.ProposalSpec) {
				s.Writes[0].SourceAuthorityDecision = "authority.external_observation/v1"
			}},
			{"subjects", "subjects", func(s *intent.ProposalSpec) {
				s.Subjects = append(s.Subjects, intent.SubjectReference{
					Kind: "POSITION", SubjectID: "position:engineering-manager",
					AuthorityDomain: "POSITION",
				})
			}},
			{"purpose and residency", "purpose", func(s *intent.ProposalSpec) {
				s.Purpose.ResidencyRef = "residency:us"
			}},
			{"revalidation plan", "revalidation_rules", func(s *intent.ProposalSpec) {
				s.Revalidation.Rules = []string{"promotion_execution_revalidation/v2"}
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := assess(t, tc.mutate)
				a := got.Assessment
				if !a.MaterialChange {
					t.Fatalf("changing the %s was not material\n%s",
						tc.name, strings.Join(a.Explanation, "\n"))
				}
				if a.Verdict != approval.VerdictNewRevision {
					t.Fatalf("verdict = %s, want NEW_REVISION\n%s",
						a.Verdict, strings.Join(a.Explanation, "\n"))
				}
				if !reflect.DeepEqual(a.InvalidatedDecisionIDs, []string{got.Decision.DecisionID}) {
					t.Fatalf("invalidated = %v, want [%s]",
						a.InvalidatedDecisionIDs, got.Decision.DecisionID)
				}
				if len(a.StandingDecisionIDs) != 0 || len(a.Revalidations) != 0 {
					t.Fatalf("an approval stood through a material change: %v", a.StandingDecisionIDs)
				}
				// The explanation names the material field that moved, and it
				// is a field the PROPOSAL material list actually contains.
				if !contains(a.ChangedMaterialFields, tc.field) {
					t.Fatalf("changed fields = %v, want %s", a.ChangedMaterialFields, tc.field)
				}
				if !contains(approval.MaterialFieldNames(), tc.field) {
					t.Fatalf("%s is not in the material list", tc.field)
				}
			})
		}
	})

	t.Run("RED: revalidated context alone never invalidates", func(t *testing.T) {
		cases := []struct {
			name   string
			field  string
			mutate func(*intent.ProposalSpec)
		}{
			{"policy bundle republish", "policy_bundle_digest", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-2"
			}},
			{"taxonomy republish", "classification_taxonomy_digest", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.ClassificationTaxonomyDigest = "taxonomy-2"
			}},
			{"reference dataset republish", "reference_data_digest", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.ReferenceDataDigest = "reference-2"
			}},
			{"entitlement republish", "entitlement_digest", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.EntitlementDigest = "entitlement-2"
			}},
			{"DLP re-decision", "dlp_decision_digest", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.DLPDecisionDigest = "dlp-2"
			}},
			{"legal context republish", "legal_context_digest", func(s *intent.ProposalSpec) {
				s.ControlSnapshots.LegalContextDigest = "legal-2"
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := assess(t, tc.mutate)
				a := got.Assessment
				if a.MaterialChange {
					t.Fatalf("%s was treated as material: %v", tc.name, a.ChangedMaterialFields)
				}
				if a.Verdict != approval.VerdictApprovalStands {
					t.Fatalf("verdict = %s, want APPROVAL_STANDS\n%s",
						a.Verdict, strings.Join(a.Explanation, "\n"))
				}
				if len(a.InvalidatedDecisionIDs) != 0 {
					t.Fatalf("spurious invalidation: %v", a.InvalidatedDecisionIDs)
				}
				if !reflect.DeepEqual(a.StandingDecisionIDs, []string{got.Decision.DecisionID}) {
					t.Fatalf("standing = %v", a.StandingDecisionIDs)
				}
				if !contains(a.ChangedControlSnapshots, tc.field) {
					t.Fatalf("changed control snapshots = %v, want %s",
						a.ChangedControlSnapshots, tc.field)
				}
				// The revalidation is recorded, not merely implied.
				if len(a.Revalidations) != 1 {
					t.Fatalf("%d revalidations recorded", len(a.Revalidations))
				}
				r := a.Revalidations[0]
				if r.DecisionID != got.Decision.DecisionID ||
					r.FromDigest != got.From.MaterialDigest.Digest ||
					r.ToDigest != got.To.MaterialDigest.Digest ||
					!r.RevalidatedAt.IsSet() ||
					r.RuleID != approval.RuleApprovalStands {
					t.Fatalf("revalidation = %+v", r)
				}
			})
		}
	})

	t.Run("RED: a note or a new creator is not a material change", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*intent.ProposalSpec)
		}{
			{"nothing at all", nil},
			{"an invalidator note", func(s *intent.ProposalSpec) {
				s.InvalidatorRefs = []string{"note:reviewed-by-hr-ops"}
			}},
			{"a different creator", func(s *intent.ProposalSpec) {
				s.CreatedBy.PrincipalID = humanwork.PrincipalCompensationPartner
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				a := assess(t, tc.mutate).Assessment
				if a.MaterialChange || a.Verdict != approval.VerdictApprovalStands {
					t.Fatalf("verdict = %s, changed = %v",
						a.Verdict, a.ChangedMaterialFields)
				}
			})
		}
	})

	t.Run("GREEN: a mandatory deny blocks and invalidates", func(t *testing.T) {
		got := assess(t, func(s *intent.ProposalSpec) {
			s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-3"
		}, approval.MandatoryDeny{
			RuleID:     "legal.eu.works_council_consultation_required/v3",
			ControlRef: "policy-bundle-3",
			Reason:     "works council consultation is now mandatory for management promotions",
		})
		a := got.Assessment
		if a.Verdict != approval.VerdictBlocked {
			t.Fatalf("verdict = %s, want BLOCKED\n%s", a.Verdict, strings.Join(a.Explanation, "\n"))
		}
		if a.MaterialChange {
			t.Fatal("a mandatory deny was reported as a material change")
		}
		if !reflect.DeepEqual(a.InvalidatedDecisionIDs, []string{got.Decision.DecisionID}) {
			t.Fatalf("invalidated = %v", a.InvalidatedDecisionIDs)
		}
		if !strings.Contains(strings.Join(a.Explanation, "\n"), approval.RuleMandatoryDeny) {
			t.Fatalf("explanation does not cite the deny rule:\n%s", strings.Join(a.Explanation, "\n"))
		}
	})

	t.Run("GREEN: exactly one verdict, every time", func(t *testing.T) {
		verdicts := map[approval.Verdict]bool{}
		for _, tc := range []struct {
			mutate func(*intent.ProposalSpec)
			denies []approval.MandatoryDeny
		}{
			{func(s *intent.ProposalSpec) { s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-4" }, nil},
			{func(s *intent.ProposalSpec) { s.Writes[0].FieldPath = "assignment.grade_ref" }, nil},
			{nil, []approval.MandatoryDeny{{RuleID: "r", ControlRef: "c", Reason: "why"}}},
		} {
			a := assess(t, tc.mutate, tc.denies...).Assessment
			switch a.Verdict {
			case approval.VerdictApprovalStands, approval.VerdictNewRevision, approval.VerdictBlocked:
			default:
				t.Fatalf("verdict = %q", a.Verdict)
			}
			verdicts[a.Verdict] = true
		}
		if len(verdicts) != 3 {
			t.Fatalf("the three verdicts are not all reachable: %v", verdicts)
		}
	})

	t.Run("GREEN: a thousand pending approvals survive a policy republish", func(t *testing.T) {
		// The compensation-cycle case. A tenant-wide policy bundle republish
		// touches every pending proposal's control context and none of their
		// material results, and the number that may be invalidated is zero.
		f := mustFixture(t)
		base, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
			humanwork.PrincipalHRBP, approval.OutcomeApproved))
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		const n = 1000
		pending := make([]approval.ApprovalDecision, 0, n)
		for i := 0; i < n; i++ {
			d := base
			d.DecisionID = fmt.Sprintf("decision:pending-%04d", i)
			pending = append(pending, d)
		}
		from := f.Current()
		to, err := f.NextRevision(func(s *intent.ProposalSpec) {
			s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-cycle-2027"
		})
		if err != nil {
			t.Fatalf("next revision: %v", err)
		}
		a, err := approval.Assess(approval.AssessRequest{
			DefinitionRef: f.Definition.Ref,
			From:          from,
			To:            to,
			Decisions:     pending,
			Clock:         approval.FixtureClock(),
		})
		if err != nil {
			t.Fatalf("Assess: %v", err)
		}
		if a.Verdict != approval.VerdictApprovalStands {
			t.Fatalf("verdict = %s", a.Verdict)
		}
		if len(a.InvalidatedDecisionIDs) != 0 {
			t.Fatalf("%d spurious invalidations", len(a.InvalidatedDecisionIDs))
		}
		if len(a.StandingDecisionIDs) != n || len(a.Revalidations) != n {
			t.Fatalf("standing = %d, revalidations = %d, want %d each",
				len(a.StandingDecisionIDs), len(a.Revalidations), n)
		}
	})

	t.Run("GREEN: a decision bound to neither revision is reported, not dropped", func(t *testing.T) {
		f := mustFixture(t)
		d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
			humanwork.PrincipalHRBP, approval.OutcomeApproved))
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		stray := d
		stray.DecisionID = "decision:stray"
		stray.Binding.ProposalDigest.Digest = strings.Repeat("e", 64)

		from := f.Current()
		to, err := f.NextRevision(nil)
		if err != nil {
			t.Fatalf("next revision: %v", err)
		}
		a, err := approval.Assess(approval.AssessRequest{
			DefinitionRef: f.Definition.Ref,
			From:          from,
			To:            to,
			Decisions:     []approval.ApprovalDecision{d, stray},
			Clock:         approval.FixtureClock(),
		})
		if err != nil {
			t.Fatalf("Assess: %v", err)
		}
		if !reflect.DeepEqual(a.UnboundDecisionIDs, []string{"decision:stray"}) {
			t.Fatalf("unbound = %v", a.UnboundDecisionIDs)
		}
		if !strings.Contains(strings.Join(a.Explanation, "\n"), approval.RuleBindingNotCurrent) {
			t.Fatalf("explanation does not cite the unbound rule:\n%s",
				strings.Join(a.Explanation, "\n"))
		}
	})

	t.Run("GREEN: the assessment proves it changed nothing", func(t *testing.T) {
		got := assess(t, func(s *intent.ProposalSpec) {
			s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-5"
		})
		r := got.Assessment.Receipt
		if err := r.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if !r.Counters.IsZero() {
			t.Fatalf("assessment counted effects: %v", r.Counters.NonZero())
		}
		if r.InputsDigest == "" || r.ResultDigest == "" || len(r.Controls) == 0 {
			t.Fatalf("receipt = %+v", r)
		}
		if r.ExecutionState != "NOT_PLANNED" {
			t.Fatalf("execution state = %q", r.ExecutionState)
		}
	})

	t.Run("GREEN: assessment is deterministic", func(t *testing.T) {
		first := assess(t, func(s *intent.ProposalSpec) {
			s.Writes[0].FieldPath = "assignment.grade_ref"
		}).Assessment
		second := assess(t, func(s *intent.ProposalSpec) {
			s.Writes[0].FieldPath = "assignment.grade_ref"
		}).Assessment
		if first.Receipt.InputsDigest != second.Receipt.InputsDigest ||
			first.Receipt.ResultDigest != second.Receipt.ResultDigest {
			t.Fatal("two identical assessments produced different digests")
		}
		if !reflect.DeepEqual(first.Explanation, second.Explanation) {
			t.Fatalf("explanations diverged:\n%v\n---\n%v", first.Explanation, second.Explanation)
		}
	})

	t.Run("REFACTOR: the material list decides and nothing else does", func(t *testing.T) {
		// MaterialResultEqual is the whole decision procedure. Anything the
		// kernel's own MaterialEqual calls equal, it must call equal too: it is
		// deliberately weaker only on revision lineage, which is identity
		// rather than result.
		f := mustFixture(t)
		from := f.Current()
		to, err := f.NextRevision(func(s *intent.ProposalSpec) {
			s.ControlSnapshots.PolicyBundleDigest = "policy-bundle-6"
		})
		if err != nil {
			t.Fatalf("next revision: %v", err)
		}
		if intent.MaterialEqual(from, to) && !approval.MaterialResultEqual(from, to) {
			t.Fatal("MaterialResultEqual is stricter than the kernel's MaterialEqual")
		}
		if !approval.MaterialResultEqual(from, to) {
			t.Fatal("a control-snapshot republish changed the material result")
		}
		// And the digests still differ, because revision identity is material:
		// materiality of the result is not the same question as identity of
		// the revision.
		if from.MaterialDigest.Digest == to.MaterialDigest.Digest {
			t.Fatal("two revisions share a material digest")
		}
	})
}

// TestTodo_INTENT_006_Mutation proves the split between the material list and
// revalidated context is exactly where it claims to be.
//
// A mutation that moved a control snapshot into the material encoding would
// make a policy republish invalidate every pending approval; one that moved a
// material field out would let a changed amount keep an old approval. Each case
// below changes one field and requires the verdict to be the one that field's
// side of the line demands.
func TestTodo_INTENT_006_Mutation(t *testing.T) {
	material := []struct {
		name   string
		mutate func(*intent.ProposalSpec)
	}{
		{"tenant scope", func(s *intent.ProposalSpec) { s.OrganizationScopeID = "org:acme-eu:platform" }},
		{"legal entity", func(s *intent.ProposalSpec) { s.LegalEntityID = "legal:acme-us-inc" }},
		{"current state", func(s *intent.ProposalSpec) {
			s.CurrentState[0].CanonicalText = "position:principal-engineer"
		}},
		{"proposed state", func(s *intent.ProposalSpec) {
			s.ProposedState[0].CanonicalText = "position:director"
		}},
		{"expected baseline", func(s *intent.ProposalSpec) {
			rev, err := values.NewSequenceRevision(approval.FixtureEmploymentStream, 43)
			if err != nil {
				panic(err)
			}
			s.Writes[0].ExpectedRevision = rev
			s.SourceBaselines[0].ExpectedRevision = rev
		}},
		{"attachment hash", func(s *intent.ProposalSpec) { s.Attachments[0].Digest = "beef" }},
		{"separation constraint", func(s *intent.ProposalSpec) {
			s.RequiredApprovals[0].SeparationConstraint = "policy.other_sod/v1"
		}},
		{"approved purpose", func(s *intent.ProposalSpec) { s.Purpose.Purpose = "promotion.off_cycle" }},
		{"destination", func(s *intent.ProposalSpec) { s.Purpose.DestinationRef = "destination:external" }},
	}
	for _, tc := range material {
		t.Run("material/"+tc.name, func(t *testing.T) {
			a := assess(t, tc.mutate).Assessment
			if !a.MaterialChange || a.Verdict != approval.VerdictNewRevision {
				t.Fatalf("changing the %s produced %s (material=%v); it belongs to the material list",
					tc.name, a.Verdict, a.MaterialChange)
			}
			if len(a.ChangedMaterialFields) == 0 {
				t.Fatalf("the %s moved the encoding but no named material field: %v",
					tc.name, a.Explanation)
			}
		})
	}

	context := []struct {
		name   string
		mutate func(*intent.ProposalSpec)
	}{
		{"capability registry", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.CapabilityRegistryDigest = "cap-registry-2"
		}},
		{"classification label set", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.ClassificationLabelSetDigest = "labels-2"
		}},
		{"workflow definition", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.WorkflowDefinitionDigest = "workflow-2"
		}},
		{"connector configuration", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.ConnectorConfigurationDigest = "connector-2"
		}},
		{"destination trust", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.DestinationTrustDigest = "trust-2"
		}},
		{"propagation watermark", func(s *intent.ProposalSpec) {
			s.ControlSnapshots.ClassificationPropagationWatermark = "watermark-2"
		}},
	}
	for _, tc := range context {
		t.Run("context/"+tc.name, func(t *testing.T) {
			a := assess(t, tc.mutate).Assessment
			if a.MaterialChange || a.Verdict != approval.VerdictApprovalStands {
				t.Fatalf("changing the %s produced %s (material=%v, fields=%v); "+
					"it is revalidated context, not material",
					tc.name, a.Verdict, a.MaterialChange, a.ChangedMaterialFields)
			}
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
