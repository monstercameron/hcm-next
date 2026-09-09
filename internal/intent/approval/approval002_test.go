package approval_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
)

// TestTodo_APPROVAL_002 is the PRIMARY test for binding an ApprovalDecision to
// the exact proposal and control context.
//
// RED: an altered proposal, policy, context, task version or argument set, and
// an unauthorized approver, return INVALID_PROPOSAL or AUTHORITY_CHANGED and no
// decision is accepted.
//
// GREEN: the decision binds the requirement, the proposal and control digests,
// the authority, the principal and session, the timestamp, the reason and the
// resolution evidence.
func TestTodo_APPROVAL_002(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*approval.Vote)
			cause  error
			code   string
		}{
			{"altered proposal revision", func(v *approval.Vote) {
				v.ProposalRevisionID = "revision:other"
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"altered proposal digest", func(v *approval.Vote) {
				v.ProposalDigest.Digest = strings.Repeat("a", 64)
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"altered digest profile", func(v *approval.Vote) {
				v.ProposalDigest.ProfileVersion++
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"altered digest schema", func(v *approval.Vote) {
				v.ProposalDigest.SchemaID = "hcmnext.other.Proposal"
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"altered digest algorithm", func(v *approval.Vote) {
				v.ProposalDigest.AlgorithmID = "md5"
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"replayed digest under another scope binding", func(v *approval.Vote) {
				v.ProposalDigest.ScopeBindingDigest = strings.Repeat("b", 64)
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"altered policy bundle", func(v *approval.Vote) {
				v.ControlSnapshots.PolicyBundleDigest = "policy-bundle-2"
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"altered legal context", func(v *approval.Vote) {
				v.ControlSnapshots.LegalContextDigest = "legal-2"
			}, approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"stale task version", func(v *approval.Vote) { v.TaskVersion = 2 },
				approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"no reason", func(v *approval.Vote) { v.Reason = "" },
				approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"no authorization decision", func(v *approval.Vote) { v.AuthorityDecisionRef = "" },
				approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"no outcome", func(v *approval.Vote) { v.Outcome = "" },
				approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"unassured approver", func(v *approval.Vote) { v.Approver.IdentityAssuranceRef = "" },
				approval.ErrInvalidProposal, approval.CodeInvalidProposal},
			{"unauthorized approver", func(v *approval.Vote) {
				v.Approver.PrincipalID = humanwork.PrincipalIntern
			}, approval.ErrAuthorityChanged, approval.CodeAuthorityChanged},
			{"approver claiming a delegated route they do not hold", func(v *approval.Vote) {
				v.Approver.Via = humanwork.SourceDelegated
				v.Approver.DelegationID = "delegation:invented"
			}, approval.ErrAuthorityChanged, approval.CodeAuthorityChanged},
			{"vote on a requirement outside the set", func(v *approval.Vote) {
				v.RequirementID = "req.invented/v1"
			}, approval.ErrUnknownRequirement, approval.CodeUnknownRequirement},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f := mustFixture(t)
				v := f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved)
				tc.break_(&v)
				_, err := f.Binder.Record(v)
				if !errors.Is(err, tc.cause) {
					t.Fatalf("Record = %v, want %v", err, tc.cause)
				}
				if got := approval.CodeOf(err); got != tc.code {
					t.Fatalf("code = %q, want %q", got, tc.code)
				}
				if n := len(f.Binder.Decisions()); n != 0 {
					t.Fatalf("%d decisions accepted after a refusal", n)
				}
			})
		}
	})

	t.Run("RED: a decision for the successor's arguments is refused by the old binder", func(t *testing.T) {
		// The proposal is superseded by one that raises the amount. A vote
		// carrying the new digest is not a vote on the proposal this binder
		// holds, however current that digest happens to be.
		f := mustFixture(t)
		stale := f.Binder
		next, err := f.NextRevision(func(s *intent.ProposalSpec) {
			s.Writes[0].ProposedCanonicalText = "position:senior-engineering-manager"
		})
		if err != nil {
			t.Fatalf("next revision: %v", err)
		}
		v := f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved)
		v.ProposalRevisionID = next.ProposalRevisionID
		v.ProposalDigest = next.MaterialDigest

		_, err = stale.Record(v)
		if !errors.Is(err, approval.ErrInvalidProposal) {
			t.Fatalf("Record = %v, want ErrInvalidProposal", err)
		}
		if n := len(stale.Decisions()); n != 0 {
			t.Fatalf("%d decisions accepted on the superseded revision", n)
		}
	})

	t.Run("GREEN: the decision binds everything it was decided against", func(t *testing.T) {
		f := mustFixture(t)
		rev := f.Current()
		v := f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved)
		d, err := f.Binder.Record(v)
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		req, _ := f.Scenario.Requirements.Find(humanwork.RequirementHRBP)
		var res humanwork.Resolution
		for _, r := range f.Resolutions {
			if r.RequirementID == humanwork.RequirementHRBP {
				res = r
			}
		}

		if d.DecisionID == "" {
			t.Fatal("decision has no id")
		}
		if d.Binding.RequirementID != req.RequirementID ||
			d.Binding.RequirementRevision != req.Revision {
			t.Fatalf("binding names requirement %s@%d",
				d.Binding.RequirementID, d.Binding.RequirementRevision)
		}
		if d.Binding.IntentID != rev.IntentID ||
			d.Binding.ProposalRevisionID != rev.ProposalRevisionID {
			t.Fatalf("binding names proposal %s/%s", d.Binding.IntentID, d.Binding.ProposalRevisionID)
		}
		if !reflect.DeepEqual(d.Binding.ProposalDigest, rev.MaterialDigest) {
			t.Fatalf("binding digest = %+v, want %+v", d.Binding.ProposalDigest, rev.MaterialDigest)
		}
		// The full canonical reference, not a naked hash: without profile,
		// schema and algorithm the digest cannot be re-derived later.
		for _, fld := range []struct{ name, value string }{
			{"profile", d.Binding.ProposalDigest.ProfileID},
			{"schema", d.Binding.ProposalDigest.SchemaID},
			{"algorithm", d.Binding.ProposalDigest.AlgorithmID},
			{"scope binding", d.Binding.ProposalDigest.ScopeBindingDigest},
		} {
			if fld.value == "" {
				t.Fatalf("binding records no %s", fld.name)
			}
		}
		if d.Binding.ControlSnapshots != rev.ControlSnapshots {
			t.Fatal("binding does not record the control context")
		}
		if d.Binding.LegalContextDigest() != rev.ControlSnapshots.LegalContextDigest {
			t.Fatal("binding does not record the LegalContext reference")
		}
		if d.Binding.TaskVersion != 1 ||
			d.Binding.RenderedProjectionDigest != approval.RenderedProjectionDigest(req.RequirementID, 1) {
			t.Fatalf("binding rendered-projection = %d/%q",
				d.Binding.TaskVersion, d.Binding.RenderedProjectionDigest)
		}
		if d.Binding.RequirementDigest != res.RequirementDigest ||
			d.Binding.ResolutionExpressionDigest != res.ExpressionDigest {
			t.Fatal("binding does not cite the resolution it was authorized by")
		}
		if d.Outcome != approval.OutcomeApproved {
			t.Fatalf("outcome = %s", d.Outcome)
		}
		if d.Approver.PrincipalID != humanwork.PrincipalHRBP ||
			d.Approver.IdentityAssuranceRef == "" || d.Approver.SessionRef == "" {
			t.Fatalf("approver = %+v", d.Approver)
		}
		if d.AuthorityDecisionRef == "" || d.Reason == "" {
			t.Fatal("decision records no authorization reference or reason")
		}
		if !d.DecidedAt.IsSet() {
			t.Fatal("decision records no decision time")
		}
		if d.Digest() == "" {
			t.Fatal("decision has no digest")
		}
	})

	t.Run("GREEN: a rejection is a decision like any other", func(t *testing.T) {
		f := mustFixture(t)
		d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
			humanwork.PrincipalHRBP, approval.OutcomeRejected))
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if d.Outcome != approval.OutcomeRejected {
			t.Fatalf("outcome = %s", d.Outcome)
		}
		if d.Binding.ProposalDigest.Digest != f.Current().MaterialDigest.Digest {
			t.Fatal("a rejection is bound less tightly than an approval")
		}
	})

	t.Run("GREEN: the whole approval graph can be decided", func(t *testing.T) {
		f := mustFixture(t)
		voters := map[string]string{
			humanwork.RequirementCurrentManager:      humanwork.PrincipalManager,
			humanwork.RequirementHRBP:                humanwork.PrincipalHRBP,
			humanwork.RequirementCompensationPartner: humanwork.PrincipalCompensationPartner,
			humanwork.RequirementFinancePartner:      humanwork.PrincipalFinancePartner,
		}
		for _, req := range f.Scenario.Requirements.Requirements {
			principal, ok := voters[req.RequirementID]
			if !ok {
				t.Fatalf("no voter for %s", req.RequirementID)
			}
			if _, err := f.Binder.Record(
				f.Vote(req.RequirementID, principal, approval.OutcomeApproved)); err != nil {
				t.Fatalf("%s: %v", req.RequirementID, err)
			}
		}
		if n := len(f.Binder.Decisions()); n != len(voters) {
			t.Fatalf("%d decisions, want %d", n, len(voters))
		}
	})

	t.Run("REFACTOR: an identical resubmission replays the same decision id", func(t *testing.T) {
		f := mustFixture(t)
		v := f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved)
		first, err := f.Binder.Record(v)
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		for i := 0; i < 4; i++ {
			again, err := f.Binder.Record(v)
			if err != nil {
				t.Fatalf("replay %d: %v", i, err)
			}
			if !reflect.DeepEqual(first, again) {
				t.Fatalf("replay %d produced a different decision", i)
			}
		}
		if n := len(f.Binder.Decisions()); n != 1 {
			t.Fatalf("%d decisions recorded for one vote", n)
		}
	})

	t.Run("REFACTOR: a differing second decision is a conflict, never a mutation", func(t *testing.T) {
		f := mustFixture(t)
		if _, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
			humanwork.PrincipalHRBP, approval.OutcomeApproved)); err != nil {
			t.Fatalf("Record: %v", err)
		}
		_, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
			humanwork.PrincipalHRBP, approval.OutcomeRejected))
		if !errors.Is(err, approval.ErrConflictingDecision) {
			t.Fatalf("Record = %v, want ErrConflictingDecision", err)
		}
		decisions := f.Binder.Decisions()
		if len(decisions) != 1 || decisions[0].Outcome != approval.OutcomeApproved {
			t.Fatalf("the prior vote was mutated: %+v", decisions)
		}
	})
}

// TestTodo_APPROVAL_002_Golden pins the canonical shape of a bound decision.
//
// A decision is evidence, and evidence whose shape drifts silently is not
// evidence. The golden covers the whole binding, so dropping, renaming or
// reordering any bound field is a visible diff rather than a quiet loss.
func TestTodo_APPROVAL_002_Golden(t *testing.T) {
	f := mustFixture(t)
	d, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
		humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	goldenJSON(t, "approval_002_decision.json", struct {
		Decision approval.ApprovalDecision
		Digest   string
	}{Decision: d, Digest: d.Digest()})
}

// TestTodo_APPROVAL_002_Security is the adversarial pass: every shape in which
// an attacker could try to make a decision mean something other than what it
// says.
func TestTodo_APPROVAL_002_Security(t *testing.T) {
	t.Run("a vote cannot supply the rendered-projection digest", func(t *testing.T) {
		// The server records the digest of what it rendered. Making that
		// unrepresentable in the submission is stronger than validating it,
		// because there is no field to lie in.
		vt := reflect.TypeOf(approval.Vote{})
		for i := 0; i < vt.NumField(); i++ {
			name := strings.ToLower(vt.Field(i).Name)
			if strings.Contains(name, "projection") || strings.Contains(name, "rendered") {
				t.Fatalf("Vote.%s lets a client supply the rendered projection", vt.Field(i).Name)
			}
		}
	})

	t.Run("a vote cannot supply the decision id or time", func(t *testing.T) {
		vt := reflect.TypeOf(approval.Vote{})
		for i := 0; i < vt.NumField(); i++ {
			name := strings.ToLower(vt.Field(i).Name)
			if name == "decisionid" || name == "decidedat" || name == "votedigest" {
				t.Fatalf("Vote.%s lets a client mint decision identity", vt.Field(i).Name)
			}
		}
	})

	t.Run("the binding is the server's copy, not the vote's assertions", func(t *testing.T) {
		// The vote asserts the truth; if it agrees, the decision still records
		// the binder's own values. Proved here by handing the binder a digest
		// reference that is field-for-field equal but carries an extra
		// caller-chosen artifact pointer: the recorded binding must not have it.
		f := mustFixture(t)
		v := f.Vote(humanwork.RequirementHRBP, humanwork.PrincipalHRBP, approval.OutcomeApproved)
		v.ProposalDigest.CanonicalBytesArtifactRef = digest.Ptr("artifact:attacker-controlled")
		v.ProposalDigest.MaterialProfileRef = digest.Ptr("profile:attacker-controlled")

		d, err := f.Binder.Record(v)
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		for _, ref := range []*string{
			d.Binding.ProposalDigest.CanonicalBytesArtifactRef,
			d.Binding.ProposalDigest.MaterialProfileRef,
		} {
			if ref != nil && strings.Contains(*ref, "attacker-controlled") {
				t.Fatalf("the recorded binding carried a caller-supplied reference %q", *ref)
			}
		}
		if !reflect.DeepEqual(d.Binding.ProposalDigest, f.Current().MaterialDigest) {
			t.Fatal("the recorded binding is not the server-held digest")
		}
	})

	t.Run("a candidate for one requirement cannot decide another", func(t *testing.T) {
		f := mustFixture(t)
		v := f.Vote(humanwork.RequirementFinancePartner,
			humanwork.PrincipalHRBP, approval.OutcomeApproved)
		_, err := f.Binder.Record(v)
		if !errors.Is(err, approval.ErrAuthorityChanged) {
			t.Fatalf("Record = %v, want ErrAuthorityChanged", err)
		}
	})

	t.Run("an excluded principal cannot decide", func(t *testing.T) {
		// The manager's lapsed delegate was excluded by rule during
		// resolution. Being named in the directory is not authority.
		f := mustFixture(t)
		v := f.Vote(humanwork.RequirementCurrentManager,
			humanwork.PrincipalExpiredDelegate, approval.OutcomeApproved)
		_, err := f.Binder.Record(v)
		if !errors.Is(err, approval.ErrAuthorityChanged) {
			t.Fatalf("Record = %v, want ErrAuthorityChanged", err)
		}
	})

	t.Run("a binder cannot be built over an unresolvable requirement", func(t *testing.T) {
		f := mustFixture(t)
		var broken []humanwork.Resolution
		for _, r := range f.Resolutions {
			if r.RequirementID == humanwork.RequirementHRBP {
				r.Outcome = humanwork.OutcomeNoAuthorizedApprover
				r.Candidates = nil
			}
			broken = append(broken, r)
		}
		_, err := approval.NewBinder(f.Current(), f.Scenario.Requirements, broken,
			f.Projections, approval.FixtureClock(), nil)
		if !errors.Is(err, approval.ErrInvalidBinder) {
			t.Fatalf("NewBinder = %v, want ErrInvalidBinder", err)
		}
	})

	t.Run("a binder cannot be built without a rendered projection", func(t *testing.T) {
		f := mustFixture(t)
		_, err := approval.NewBinder(f.Current(), f.Scenario.Requirements, f.Resolutions,
			f.Projections[:1], approval.FixtureClock(), nil)
		if !errors.Is(err, approval.ErrInvalidBinder) {
			t.Fatalf("NewBinder = %v, want ErrInvalidBinder", err)
		}
	})
}

// TestTodo_APPROVAL_002_Mutation proves every bound field reaches the decision
// digest.
//
// A mutation that dropped a field from ApprovalDecision.Canonical would let two
// decisions bound to different proposals, contexts, approvers or renderings
// share a digest. Each case changes one field and requires the digest to move.
func TestTodo_APPROVAL_002_Mutation(t *testing.T) {
	f := mustFixture(t)
	base, err := f.Binder.Record(f.Vote(humanwork.RequirementHRBP,
		humanwork.PrincipalHRBP, approval.OutcomeApproved))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*approval.ApprovalDecision)
	}{
		{"decision id", func(d *approval.ApprovalDecision) { d.DecisionID = "decision:other" }},
		{"requirement id", func(d *approval.ApprovalDecision) { d.Binding.RequirementID = "req.other/v1" }},
		{"requirement revision", func(d *approval.ApprovalDecision) { d.Binding.RequirementRevision = 2 }},
		{"intent id", func(d *approval.ApprovalDecision) { d.Binding.IntentID = "intent:other" }},
		{"proposal revision id", func(d *approval.ApprovalDecision) {
			d.Binding.ProposalRevisionID = "revision:other"
		}},
		{"proposal digest", func(d *approval.ApprovalDecision) {
			d.Binding.ProposalDigest.Digest = strings.Repeat("c", 64)
		}},
		{"digest profile", func(d *approval.ApprovalDecision) { d.Binding.ProposalDigest.ProfileVersion = 9 }},
		{"digest schema", func(d *approval.ApprovalDecision) { d.Binding.ProposalDigest.SchemaVersion = 9 }},
		{"digest algorithm", func(d *approval.ApprovalDecision) { d.Binding.ProposalDigest.AlgorithmID = "md5" }},
		{"digest scope binding", func(d *approval.ApprovalDecision) {
			d.Binding.ProposalDigest.ScopeBindingDigest = strings.Repeat("d", 64)
		}},
		{"task version", func(d *approval.ApprovalDecision) { d.Binding.TaskVersion = 2 }},
		{"rendered projection", func(d *approval.ApprovalDecision) {
			d.Binding.RenderedProjectionDigest = "projection:other"
		}},
		{"policy bundle", func(d *approval.ApprovalDecision) {
			d.Binding.ControlSnapshots.PolicyBundleDigest = "policy-bundle-2"
		}},
		{"legal context", func(d *approval.ApprovalDecision) {
			d.Binding.ControlSnapshots.LegalContextDigest = "legal-2"
		}},
		{"entitlement", func(d *approval.ApprovalDecision) {
			d.Binding.ControlSnapshots.EntitlementDigest = "entitlement-2"
		}},
		{"reference data", func(d *approval.ApprovalDecision) {
			d.Binding.ControlSnapshots.ReferenceDataDigest = "reference-2"
		}},
		{"dlp decision", func(d *approval.ApprovalDecision) {
			d.Binding.ControlSnapshots.DLPDecisionDigest = "dlp-2"
		}},
		{"requirement digest", func(d *approval.ApprovalDecision) {
			d.Binding.RequirementDigest = "requirement:other"
		}},
		{"resolution expression digest", func(d *approval.ApprovalDecision) {
			d.Binding.ResolutionExpressionDigest = "expression:other"
		}},
		{"outcome", func(d *approval.ApprovalDecision) { d.Outcome = approval.OutcomeRejected }},
		{"approver", func(d *approval.ApprovalDecision) {
			d.Approver.PrincipalID = humanwork.PrincipalCompensationPartner
		}},
		{"assurance", func(d *approval.ApprovalDecision) {
			d.Approver.IdentityAssuranceRef = "assurance.password/v1"
		}},
		{"session", func(d *approval.ApprovalDecision) { d.Approver.SessionRef = "session:other" }},
		{"delegated route", func(d *approval.ApprovalDecision) {
			d.Approver.Via = humanwork.SourceDelegated
			d.Approver.DelegationID = "delegation:other"
		}},
		{"authorization reference", func(d *approval.ApprovalDecision) {
			d.AuthorityDecisionRef = "authz:other"
		}},
		{"reason", func(d *approval.ApprovalDecision) { d.Reason = "reason.other/v1" }},
		{"decision time", func(d *approval.ApprovalDecision) { d.DecidedAt = shiftedInstant() }},
		{"vote digest", func(d *approval.ApprovalDecision) { d.VoteDigest = "vote:other" }},
	}

	seen := map[string]string{base.Digest(): "base"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := base
			tc.mutate(&got)
			d := got.Digest()
			if d == "" {
				t.Fatal("mutated decision has no digest")
			}
			if prev, ok := seen[d]; ok {
				t.Fatalf("changing the %s did not move the digest (collides with %s)", tc.name, prev)
			}
			seen[d] = tc.name
		})
	}
}
