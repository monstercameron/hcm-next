package humanwork_test

import (
	"reflect"
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork"
)

// mustRequirement returns one compiled requirement from a scenario.
func mustRequirement(t *testing.T, sc humanwork.PromotionScenario, id string) humanwork.ApprovalRequirement {
	t.Helper()
	req, ok := sc.Requirements.Find(id)
	if !ok {
		t.Fatalf("scenario has no requirement %q", id)
	}
	return req
}

// excludedBy returns the rule ids the resolution recorded for principalID.
func excludedBy(res humanwork.Resolution, principalID string) []string {
	var out []string
	for _, e := range res.Excluded {
		if e.PrincipalID == principalID {
			out = append(out, e.RuleID)
		}
	}
	return out
}

func hasRule(res humanwork.Resolution, principalID, ruleID string) bool {
	for _, r := range excludedBy(res, principalID) {
		if r == ruleID {
			return true
		}
	}
	return false
}

// TestTodo_APPROVAL_004 is the PRIMARY test for separation of duties,
// delegation and fallback during approver resolution.
//
// RED: a requester approving their own request, one principal filling two
// distinct roles, an expired or out-of-scope delegate deciding, and a fallback
// that would broaden authority are each refused with the rule that refused them.
//
// GREEN: the candidate set excludes every invalid principal, records
// resolution and effective-time evidence, and returns NO_AUTHORIZED_APPROVER
// when the quorum cannot be formed.
func TestTodo_APPROVAL_004(t *testing.T) {
	t.Run("RED: the requester cannot approve their own request", func(t *testing.T) {
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementHRBP)
		in := sc.Resolution
		in.RequesterPrincipalID = humanwork.PrincipalHRBP

		res, err := humanwork.Resolve(req, in, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if _, ok := res.Authorizes(humanwork.PrincipalHRBP); ok {
			t.Fatalf("requester is a candidate on their own request\n%s", res.Explain())
		}
		if !hasRule(res, humanwork.PrincipalHRBP, humanwork.RuleRequesterIsApprover) {
			t.Fatalf("exclusion rules = %v, want %s",
				excludedBy(res, humanwork.PrincipalHRBP), humanwork.RuleRequesterIsApprover)
		}
		// The requirement is still fillable, but only by escalation reaching an
		// authorized deputy: excluding the requester never leaves the decision
		// to somebody who lacks the authority.
		deputy, ok := res.Authorizes(humanwork.PrincipalDeputy)
		if !ok || deputy.Via != humanwork.SourceFallback {
			t.Fatalf("expected the authorized deputy by escalation\n%s", res.Explain())
		}

		// With the deputy out too, there is nobody left and the resolver says
		// so rather than lowering the bar.
		sc.Directory.SetAvailable(humanwork.PrincipalDeputy, false)
		res, err = humanwork.Resolve(req, in, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != humanwork.OutcomeNoAuthorizedApprover {
			t.Fatalf("outcome = %s, want NO_AUTHORIZED_APPROVER\n%s", res.Outcome, res.Explain())
		}
	})

	t.Run("RED: the requester cannot approve through their own delegate", func(t *testing.T) {
		// The manager raises the promotion. Excluding the manager but keeping
		// their delegate would launder the requester's own authority straight
		// back into the decision.
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementCurrentManager)
		in := sc.Resolution
		in.RequesterPrincipalID = humanwork.PrincipalManager

		res, err := humanwork.Resolve(req, in, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		for _, id := range []string{humanwork.PrincipalManager, humanwork.PrincipalDelegate} {
			if _, ok := res.Authorizes(id); ok {
				t.Fatalf("%s is a candidate on the requester's own request\n%s", id, res.Explain())
			}
			if !hasRule(res, id, humanwork.RuleRequesterIsApprover) {
				t.Fatalf("%s exclusion rules = %v, want %s",
					id, excludedBy(res, id), humanwork.RuleRequesterIsApprover)
			}
		}
	})

	t.Run("RED: one principal cannot fill two distinct roles", func(t *testing.T) {
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementHRBP)
		in := sc.Resolution
		in.ClaimedBy = map[string]string{humanwork.PrincipalHRBP: humanwork.RequirementCurrentManager}

		res, err := humanwork.Resolve(req, in, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if _, ok := res.Authorizes(humanwork.PrincipalHRBP); ok {
			t.Fatalf("a principal already filling another requirement is still a candidate\n%s",
				res.Explain())
		}
		if !hasRule(res, humanwork.PrincipalHRBP, humanwork.RuleDualRole) {
			t.Fatalf("exclusion rules = %v, want %s",
				excludedBy(res, humanwork.PrincipalHRBP), humanwork.RuleDualRole)
		}
	})

	t.Run("RED: an expired or out-of-scope delegate cannot decide", func(t *testing.T) {
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementCurrentManager)
		res, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		cases := []struct{ principal, rule string }{
			{humanwork.PrincipalExpiredDelegate, humanwork.RuleDelegationExpired},
			{humanwork.PrincipalOverbroadDelegate, humanwork.RuleDelegationOutOfScope},
		}
		for _, tc := range cases {
			if _, ok := res.Authorizes(tc.principal); ok {
				t.Fatalf("%s is a candidate\n%s", tc.principal, res.Explain())
			}
			if !hasRule(res, tc.principal, tc.rule) {
				t.Fatalf("%s exclusion rules = %v, want %s",
					tc.principal, excludedBy(res, tc.principal), tc.rule)
			}
		}
	})

	t.Run("RED: fallback never broadens authority", func(t *testing.T) {
		// With both the manager and their delegate out, the requirement cannot
		// be filled and escalation reaches the deputy group. That group holds
		// two people: one who carries the required authority and one who
		// carries none. Escalation moves attention, so only the first is a
		// candidate.
		sc := financeScenario(t)
		sc.Directory.SetAvailable(humanwork.PrincipalManager, false).
			SetAvailable(humanwork.PrincipalDelegate, false)
		req := mustRequirement(t, sc, humanwork.RequirementCurrentManager)

		res, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.FallbackUsed {
			t.Fatalf("escalation fallback was not reached\n%s", res.Explain())
		}
		if _, ok := res.Authorizes(humanwork.PrincipalIntern); ok {
			t.Fatalf("a principal with no authority reached the candidate set\n%s", res.Explain())
		}
		if !hasRule(res, humanwork.PrincipalIntern, humanwork.RuleFallbackNoBroadening) {
			t.Fatalf("%s exclusion rules = %v, want %s", humanwork.PrincipalIntern,
				excludedBy(res, humanwork.PrincipalIntern), humanwork.RuleFallbackNoBroadening)
		}
		deputy, ok := res.Authorizes(humanwork.PrincipalDeputy)
		if !ok {
			t.Fatalf("the authorized deputy did not reach the candidate set\n%s", res.Explain())
		}
		if deputy.Via != humanwork.SourceFallback {
			t.Fatalf("deputy Via = %s, want FALLBACK", deputy.Via)
		}
		if res.Outcome != humanwork.OutcomeResolved {
			t.Fatalf("outcome = %s\n%s", res.Outcome, res.Explain())
		}
	})

	t.Run("RED: a manager-chain conflict costs the quorum", func(t *testing.T) {
		// The manager raises the promotion; their own manager sits on the
		// compensation committee. Excluding that executive leaves one of the
		// two the committee needs, and the honest answer is that there is no
		// authorized approver set, not that one vote will do.
		sc := executiveScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementExecutiveCommittee)
		in := sc.Resolution
		in.RequesterPrincipalID = humanwork.PrincipalManager

		res, err := humanwork.Resolve(req, in, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !hasRule(res, humanwork.PrincipalExecutiveA, humanwork.RuleManagerChainConflict) {
			t.Fatalf("%s exclusion rules = %v, want %s", humanwork.PrincipalExecutiveA,
				excludedBy(res, humanwork.PrincipalExecutiveA), humanwork.RuleManagerChainConflict)
		}
		if got := principalIDs(res); !reflect.DeepEqual(got, []string{humanwork.PrincipalExecutiveB}) {
			t.Fatalf("candidates = %v\n%s", got, res.Explain())
		}
		if res.Outcome != humanwork.OutcomeNoAuthorizedApprover {
			t.Fatalf("outcome = %s with quorum %d and %d candidates\n%s",
				res.Outcome, res.QuorumRequired, len(res.Candidates), res.Explain())
		}
	})

	t.Run("RED: an inactive or unavailable holder is not a candidate", func(t *testing.T) {
		sc := financeScenario(t)
		sc.Directory.SetActive(humanwork.PrincipalHRBP, false)
		req := mustRequirement(t, sc, humanwork.RequirementHRBP)
		res, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !hasRule(res, humanwork.PrincipalHRBP, humanwork.RulePrincipalInactive) {
			t.Fatalf("exclusion rules = %v, want %s",
				excludedBy(res, humanwork.PrincipalHRBP), humanwork.RulePrincipalInactive)
		}
	})

	t.Run("GREEN: a bounded, in-scope delegation decides and is evidenced", func(t *testing.T) {
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementCurrentManager)
		res, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		want := []string{humanwork.PrincipalDelegate, humanwork.PrincipalManager}
		if got := principalIDs(res); !reflect.DeepEqual(got, want) {
			t.Fatalf("candidates = %v, want %v\n%s", got, want, res.Explain())
		}
		delegate, _ := res.Authorizes(humanwork.PrincipalDelegate)
		if delegate.Via != humanwork.SourceDelegated {
			t.Fatalf("delegate Via = %s, want DELEGATED", delegate.Via)
		}
		if delegate.DelegationID != "delegation:manager-holiday" {
			t.Fatalf("delegate cites delegation %q", delegate.DelegationID)
		}
		if delegate.DelegatedFrom != humanwork.PrincipalManager {
			t.Fatalf("delegate borrows from %q", delegate.DelegatedFrom)
		}
		if !delegate.DelegationExpiry.IsSet() {
			t.Fatal("a delegated candidate records no delegation expiry")
		}
		manager, _ := res.Authorizes(humanwork.PrincipalManager)
		if manager.Via != humanwork.SourceDirect {
			t.Fatalf("manager Via = %s, want DIRECT", manager.Via)
		}
		// Resolution and effective-time evidence.
		if !res.ResolvedAt.IsSet() || res.EffectiveAt != sc.Resolution.EffectiveAt {
			t.Fatalf("resolution times = %s / %s", res.ResolvedAt, res.EffectiveAt)
		}
		if res.DirectoryVersion != humanwork.ScenarioDirectoryVersion {
			t.Fatalf("directory version = %q", res.DirectoryVersion)
		}
		if res.RequirementDigest == "" || res.ExpressionDigest == "" {
			t.Fatal("resolution cites no requirement or expression digest")
		}
	})

	t.Run("GREEN: a passed deadline widens routing without lowering authority", func(t *testing.T) {
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementHRBP)
		in := sc.Resolution
		in.DeadlinePassed = true

		res, err := humanwork.Resolve(req, in, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.FallbackUsed {
			t.Fatalf("a passed deadline did not reach escalation\n%s", res.Explain())
		}
		// The primary approver keeps their place and the deputy joins them;
		// the intern, who holds no authority, still does not.
		want := []string{humanwork.PrincipalDeputy, humanwork.PrincipalHRBP}
		if got := principalIDs(res); !reflect.DeepEqual(got, want) {
			t.Fatalf("candidates = %v, want %v\n%s", got, want, res.Explain())
		}
		if !hasRule(res, humanwork.PrincipalIntern, humanwork.RuleFallbackNoBroadening) {
			t.Fatalf("%s exclusion rules = %v", humanwork.PrincipalIntern,
				excludedBy(res, humanwork.PrincipalIntern))
		}
	})

	t.Run("GREEN: resolution is deterministic", func(t *testing.T) {
		sc := financeScenario(t)
		req := mustRequirement(t, sc, humanwork.RequirementCurrentManager)
		first, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		for i := 0; i < 32; i++ {
			again, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(first, again) {
				t.Fatalf("resolution %d differs from the first\n%s\n---\n%s",
					i, first.Explain(), again.Explain())
			}
		}
	})

	t.Run("REFACTOR: routing and decision authority stay separate", func(t *testing.T) {
		// Every candidate, however they were reached - directly, by delegation
		// or by escalation - had to clear the same authority floor and the same
		// separation constraints. There is no path that produces a candidate by
		// routing alone.
		sc := financeScenario(t)
		sc.Directory.SetAvailable(humanwork.PrincipalManager, false).
			SetAvailable(humanwork.PrincipalDelegate, false)
		req := mustRequirement(t, sc, humanwork.RequirementCurrentManager)
		res, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		for _, c := range res.Candidates {
			facts, ok, err := sc.Directory.Facts(c.PrincipalID, sc.Resolution.EffectiveAt)
			if err != nil || !ok {
				t.Fatalf("candidate %s has no facts", c.PrincipalID)
			}
			if c.Via != humanwork.SourceDelegated && !facts.HoldsAll(req.AuthorityFloor) {
				t.Fatalf("candidate %s reached the set without %v", c.PrincipalID, req.AuthorityFloor)
			}
			if c.PrincipalID == sc.Resolution.RequesterPrincipalID {
				t.Fatalf("the requester reached the set as %s", c.Via)
			}
		}
	})
}

// TestTodo_APPROVAL_004_Mutation proves each separation constraint is
// independently load-bearing.
//
// A mutation that hardwired one of the four flags - always on, or always off -
// would be invisible to a test that only ever ran the fully-constrained policy.
// Each case here resolves the same requirement twice, once with the flag off
// and once with it on, and requires exactly that flag to move the principal in
// and out of the candidate set.
func TestTodo_APPROVAL_004_Mutation(t *testing.T) {
	org := humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}

	cases := []struct {
		name      string
		principal string
		rule      string
		// spec builds the requirement under test.
		spec func() humanwork.RequirementSpec
		// input supplies the resolution context that makes the flag bite.
		input func(humanwork.ResolutionInput) humanwork.ResolutionInput
		// enable turns the one flag under test on.
		enable func(*humanwork.SeparationConstraints)
		// seed adjusts the directory so the principal is otherwise eligible.
		seed func(*humanwork.MemoryDirectory)
	}{
		{
			name:      "requester",
			principal: humanwork.PrincipalHRBP,
			rule:      humanwork.RuleRequesterIsApprover,
			spec: func() humanwork.RequirementSpec {
				s := validSpec()
				s.Candidates = humanwork.Relationship(humanwork.RelationshipHRBPFor, org)
				return s
			},
			input: func(in humanwork.ResolutionInput) humanwork.ResolutionInput {
				in.RequesterPrincipalID = humanwork.PrincipalHRBP
				return in
			},
			enable: func(c *humanwork.SeparationConstraints) { c.RequesterMayNotApprove = true },
		},
		{
			name:      "subject",
			principal: humanwork.PrincipalSubject,
			rule:      humanwork.RuleSubjectIsApprover,
			spec: func() humanwork.RequirementSpec {
				s := validSpec()
				s.Candidates = humanwork.Named(humanwork.PrincipalSubject, "policy.pinned/v1", org)
				return s
			},
			input: func(in humanwork.ResolutionInput) humanwork.ResolutionInput { return in },
			enable: func(c *humanwork.SeparationConstraints) {
				c.SubjectMayNotApprove = true
			},
			seed: func(d *humanwork.MemoryDirectory) {
				d.WithPrincipal(humanwork.PrincipalFacts{
					PrincipalID:          humanwork.PrincipalSubject,
					Active:               true,
					Available:            true,
					Roles:                []string{humanwork.RoleHRBP},
					OrganizationScopeID:  humanwork.ScenarioOrganizationScopeID,
					IdentityAssuranceRef: "assurance.mfa_session/v1",
				})
			},
		},
		{
			name:      "dual role",
			principal: humanwork.PrincipalHRBP,
			rule:      humanwork.RuleDualRole,
			spec: func() humanwork.RequirementSpec {
				s := validSpec()
				s.Candidates = humanwork.Relationship(humanwork.RelationshipHRBPFor, org)
				return s
			},
			input: func(in humanwork.ResolutionInput) humanwork.ResolutionInput {
				in.ClaimedBy = map[string]string{humanwork.PrincipalHRBP: "req.other/v1"}
				return in
			},
			enable: func(c *humanwork.SeparationConstraints) { c.OneRequirementPerPrincipal = true },
		},
		{
			name:      "manager chain",
			principal: humanwork.PrincipalHRBP,
			rule:      humanwork.RuleManagerChainConflict,
			spec: func() humanwork.RequirementSpec {
				s := validSpec()
				s.Candidates = humanwork.Relationship(humanwork.RelationshipHRBPFor, org)
				return s
			},
			input: func(in humanwork.ResolutionInput) humanwork.ResolutionInput {
				in.RequesterPrincipalID = humanwork.PrincipalRequester
				return in
			},
			enable: func(c *humanwork.SeparationConstraints) { c.ForbidRequesterManagerChain = true },
			seed: func(d *humanwork.MemoryDirectory) {
				// The requester reports to the HR business partner.
				d.WithManager(humanwork.PrincipalRequester, humanwork.PrincipalHRBP)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := func(enabled bool) humanwork.Resolution {
				t.Helper()
				spec := tc.spec()
				// Only the flag under test is on, so nothing else can be
				// responsible for the difference.
				spec.Separation = humanwork.SeparationConstraints{RuleID: "policy.sod/v1"}
				if enabled {
					tc.enable(&spec.Separation)
				} else {
					// A requirement must constrain something to compile, so the
					// disabled run carries a constraint that cannot fire here.
					spec.Separation.SubjectMayNotApprove = true
					if tc.name == "subject" {
						spec.Separation = humanwork.SeparationConstraints{
							RequesterMayNotApprove: true,
							RuleID:                 "policy.sod/v1",
						}
					}
				}
				req, err := humanwork.Compile(spec)
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				dir := humanwork.PromotionDirectory()
				if tc.seed != nil {
					tc.seed(dir)
				}
				in := tc.input(resolutionInput())
				res, err := humanwork.Resolve(req, in, dir, humanwork.ScenarioClock())
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				return res
			}

			off := run(false)
			if _, ok := off.Authorizes(tc.principal); !ok {
				t.Fatalf("with the %s constraint off, %s is not a candidate; the case proves nothing\n%s",
					tc.name, tc.principal, off.Explain())
			}
			on := run(true)
			if _, ok := on.Authorizes(tc.principal); ok {
				t.Fatalf("with the %s constraint on, %s is still a candidate\n%s",
					tc.name, tc.principal, on.Explain())
			}
			if !hasRule(on, tc.principal, tc.rule) {
				t.Fatalf("exclusion rules = %v, want %s", excludedBy(on, tc.principal), tc.rule)
			}
		})
	}
}

func financeScenario(t *testing.T) humanwork.PromotionScenario {
	t.Helper()
	sc, err := humanwork.NewPromotionScenario(humanwork.PromotionInputFinance())
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}
	return sc
}

func executiveScenario(t *testing.T) humanwork.PromotionScenario {
	t.Helper()
	sc, err := humanwork.NewPromotionScenario(humanwork.PromotionInputExecutive())
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}
	return sc
}
