package humanwork_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// validSpec returns a complete, compilable requirement specification. Each RED
// case below breaks exactly one thing in it, so a failure names the field that
// was removed rather than one of a dozen simultaneously-missing ones.
func validSpec() humanwork.RequirementSpec {
	at := humanwork.ScenarioAt().Time()
	return humanwork.RequirementSpec{
		RequirementID: "req.test/v1",
		Revision:      1,
		Stage:         1,
		Candidates: humanwork.Relationship(humanwork.RelationshipHRBPFor,
			humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}),
		AuthorityFloor: []string{humanwork.RoleHRBP},
		Quorum:         humanwork.Quorum{MinApprovals: 1},
		Deadline: humanwork.Deadline{
			DecideBy: values.NewInstant(at.Add(48 * time.Hour)),
			Expiry:   values.NewInstant(at.Add(30 * 24 * time.Hour)),
		},
		Escalation: humanwork.EscalationPolicy{
			OnDeadline: humanwork.EscalationNotify,
			RuleID:     "policy.escalate/v1",
		},
		Separation: humanwork.SeparationConstraints{
			RequesterMayNotApprove: true,
			RuleID:                 "policy.sod/v1",
		},
		Invalidators: []humanwork.Invalidator{
			{Kind: humanwork.InvalidatorMaterialProposalChange, RuleID: "policy.invalidate/v1"},
		},
		Source: humanwork.RequirementSource{
			Tier:                rules.ApprovalTierStandard,
			TableID:             rules.PromotionApprovalTableID,
			TableVersion:        rules.PromotionApprovalTableVersion,
			TableDigest:         "table-digest",
			MatchedRowID:        "otherwise-standard",
			GovernancePolicyRef: humanwork.ScenarioGovernancePolicyRef,
		},
	}
}

// TestTodo_APPROVAL_001 is the PRIMARY test for ApprovalRequirement and the
// bounded resolution expression language.
//
// RED: an unnamed or unscoped role, a pinned person with no policy behind them,
// and any requirement missing scope, cardinality, quorum, separation of duties,
// expiry or invalidators all fail compilation.
//
// GREEN: named, role, relationship, scoped, group, any, all and quorum
// expressions resolve candidates and carry the context and version evidence
// needed to replay the resolution.
func TestTodo_APPROVAL_001(t *testing.T) {
	t.Run("RED", func(t *testing.T) {
		org := humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}
		cases := []struct {
			name   string
			break_ func(*humanwork.RequirementSpec)
			cause  error
		}{
			{"unnamed role", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Role("", org)
			}, humanwork.ErrInvalidExpression},
			{"unscoped role", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Role(humanwork.RoleHRBP, humanwork.Scope{})
			}, humanwork.ErrInvalidExpression},
			{"role scoped to a kind with no reference", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Role(humanwork.RoleHRBP,
					humanwork.Scope{Kind: humanwork.ScopeOrganization})
			}, humanwork.ErrInvalidExpression},
			{"tenant scope carrying a reference", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Role(humanwork.RoleHRBP,
					humanwork.Scope{Kind: humanwork.ScopeTenant, Ref: "org:x"})
			}, humanwork.ErrInvalidExpression},
			{"pinned person with no policy", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Named(humanwork.PrincipalHRBP, "", org)
			}, humanwork.ErrInvalidExpression},
			{"unresolvable relationship", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Relationship("MANAGER_OF_SOMETHING", org)
			}, humanwork.ErrInvalidExpression},
			{"group with no id", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.Group("", org)
			}, humanwork.ErrInvalidExpression},
			{"leaf carrying another kind's payload", func(s *humanwork.RequirementSpec) {
				e := humanwork.Role(humanwork.RoleHRBP, org)
				e.PrincipalID = humanwork.PrincipalHRBP
				s.Candidates = e
			}, humanwork.ErrInvalidExpression},
			{"combinator with one child", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.AnyOf(humanwork.Role(humanwork.RoleHRBP, org))
			}, humanwork.ErrInvalidExpression},
			{"quorum larger than its child set", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.QuorumOf(3,
					humanwork.Role(humanwork.RoleHRBP, org),
					humanwork.Role(humanwork.RoleFinancePartner, org))
			}, humanwork.ErrInvalidExpression},
			{"quorum of zero children", func(s *humanwork.RequirementSpec) {
				s.Candidates = humanwork.QuorumOf(0,
					humanwork.Role(humanwork.RoleHRBP, org),
					humanwork.Role(humanwork.RoleFinancePartner, org))
			}, humanwork.ErrInvalidExpression},
			{"expression past the depth bound", func(s *humanwork.RequirementSpec) {
				e := humanwork.Role(humanwork.RoleHRBP, org)
				for i := 0; i <= humanwork.MaxExpressionDepth; i++ {
					e = humanwork.AnyOf(e, humanwork.Role(humanwork.RoleFinancePartner, org))
				}
				s.Candidates = e
			}, humanwork.ErrInvalidExpression},
			{"no ordering stage", func(s *humanwork.RequirementSpec) { s.Stage = 0 },
				humanwork.ErrInvalidRequirement},
			{"no required authority", func(s *humanwork.RequirementSpec) { s.AuthorityFloor = nil },
				humanwork.ErrInvalidRequirement},
			{"no cardinality", func(s *humanwork.RequirementSpec) { s.Quorum = humanwork.Quorum{} },
				humanwork.ErrInvalidRequirement},
			{"multi-approval quorum that will not say whether approvers differ",
				func(s *humanwork.RequirementSpec) {
					s.Quorum = humanwork.Quorum{MinApprovals: 2}
				}, humanwork.ErrInvalidRequirement},
			{"no decision deadline", func(s *humanwork.RequirementSpec) {
				s.Deadline.DecideBy = values.Instant{}
			}, humanwork.ErrInvalidRequirement},
			{"no expiry", func(s *humanwork.RequirementSpec) {
				s.Deadline.Expiry = values.Instant{}
			}, humanwork.ErrInvalidRequirement},
			{"expiry before the deadline", func(s *humanwork.RequirementSpec) {
				s.Deadline.Expiry = values.NewInstant(humanwork.ScenarioAt().Time())
			}, humanwork.ErrInvalidRequirement},
			{"no separation of duties", func(s *humanwork.RequirementSpec) {
				s.Separation = humanwork.SeparationConstraints{RuleID: "policy.sod/v1"}
			}, humanwork.ErrInvalidRequirement},
			{"separation with no rule id", func(s *humanwork.RequirementSpec) {
				s.Separation.RuleID = ""
			}, humanwork.ErrInvalidRequirement},
			{"no invalidators", func(s *humanwork.RequirementSpec) { s.Invalidators = nil },
				humanwork.ErrInvalidRequirement},
			{"invalidators that omit material proposal change", func(s *humanwork.RequirementSpec) {
				s.Invalidators = []humanwork.Invalidator{
					{Kind: humanwork.InvalidatorDeadlineExpired, RuleID: "policy.expire/v1"},
				}
			}, humanwork.ErrInvalidRequirement},
			{"escalation to a fallback that does not exist", func(s *humanwork.RequirementSpec) {
				s.Escalation = humanwork.EscalationPolicy{
					OnDeadline: humanwork.EscalationReassignToFallback,
					RuleID:     "policy.escalate/v1",
				}
			}, humanwork.ErrInvalidRequirement},
			{"provenance with no table version", func(s *humanwork.RequirementSpec) {
				s.Source.TableVersion = ""
			}, humanwork.ErrInvalidRequirement},
			{"provenance from a blocked tier", func(s *humanwork.RequirementSpec) {
				s.Source.Tier = rules.ApprovalTierUnknownBlocked
			}, humanwork.ErrTierBlocked},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec := validSpec()
				tc.break_(&spec)
				if _, err := humanwork.Compile(spec); !errors.Is(err, tc.cause) {
					t.Fatalf("Compile = %v, want %v", err, tc.cause)
				}
			})
		}
	})

	t.Run("GREEN: every expression form resolves candidates", func(t *testing.T) {
		org := humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}
		legal := humanwork.Scope{Kind: humanwork.ScopeLegalEntity, Ref: humanwork.ScenarioLegalEntityID}
		subject := humanwork.Scope{Kind: humanwork.ScopeSubject, Ref: humanwork.ScenarioWorkerID}
		dir := humanwork.PromotionDirectory()

		cases := []struct {
			name  string
			expr  humanwork.Expression
			floor []string
			want  []string
		}{
			{"named", humanwork.Named(humanwork.PrincipalHRBP, "policy.pinned_approver/v1", org),
				[]string{humanwork.RoleHRBP}, []string{humanwork.PrincipalHRBP}},
			{"role", humanwork.Role(humanwork.RoleExecutiveApprover, legal),
				[]string{humanwork.RoleExecutiveApprover},
				[]string{humanwork.PrincipalExecutiveA, humanwork.PrincipalExecutiveB}},
			{"relationship", humanwork.Relationship(humanwork.RelationshipCompensationPartnerFor, org),
				[]string{humanwork.RoleCompensationPartner},
				[]string{humanwork.PrincipalCompensationPartner}},
			{"scoped relationship on the subject",
				humanwork.Relationship(humanwork.RelationshipManagerOf, subject),
				[]string{humanwork.RolePeopleManager},
				// The manager's standing delegate does not qualify here: the
				// delegation names req.promotion.current_manager/v1, not this
				// requirement, and a delegation covers exactly what it names.
				// TestTodo_APPROVAL_004 resolves the requirement it does cover.
				[]string{humanwork.PrincipalManager}},
			{"group", humanwork.Group("group.compensation_committee", legal),
				[]string{humanwork.RoleExecutiveApprover},
				[]string{humanwork.PrincipalExecutiveA, humanwork.PrincipalExecutiveB}},
			{"any", humanwork.AnyOf(
				humanwork.Group("group.compensation_committee", legal),
				humanwork.Role(humanwork.RoleExecutiveApprover, legal)),
				[]string{humanwork.RoleExecutiveApprover},
				[]string{humanwork.PrincipalExecutiveA, humanwork.PrincipalExecutiveB}},
			{"all", humanwork.AllOf(
				humanwork.Group("group.compensation_committee", legal),
				humanwork.Named(humanwork.PrincipalExecutiveA, "policy.pinned_approver/v1", legal)),
				[]string{humanwork.RoleExecutiveApprover},
				[]string{humanwork.PrincipalExecutiveA}},
			{"quorum", humanwork.QuorumOf(2,
				humanwork.Group("group.compensation_committee", legal),
				humanwork.Role(humanwork.RoleExecutiveApprover, legal)),
				[]string{humanwork.RoleExecutiveApprover},
				[]string{humanwork.PrincipalExecutiveA, humanwork.PrincipalExecutiveB}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec := validSpec()
				spec.Candidates = tc.expr
				spec.AuthorityFloor = tc.floor
				req, err := humanwork.Compile(spec)
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				res, err := humanwork.Resolve(req, resolutionInput(), dir, humanwork.ScenarioClock())
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				if got := principalIDs(res); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("candidates = %v, want %v\n%s", got, tc.want, res.Explain())
				}
				if res.Outcome != humanwork.OutcomeResolved {
					t.Fatalf("outcome = %s, want RESOLVED", res.Outcome)
				}
				// Context and version evidence: without these a stored
				// candidate set cannot be replayed or audited.
				if res.DirectoryVersion != humanwork.ScenarioDirectoryVersion {
					t.Fatalf("directory version = %q", res.DirectoryVersion)
				}
				if res.ExpressionDigest != req.ExpressionDigest || res.ExpressionDigest == "" {
					t.Fatalf("expression digest = %q, want %q", res.ExpressionDigest, req.ExpressionDigest)
				}
				if res.RequirementDigest != req.Digest() || res.RequirementDigest == "" {
					t.Fatalf("requirement digest = %q", res.RequirementDigest)
				}
				if !res.EffectiveAt.IsSet() || !res.ResolvedAt.IsSet() {
					t.Fatal("resolution records no effective or resolution time")
				}
				if res.RequirementRevision != req.Revision {
					t.Fatalf("requirement revision = %d, want %d", res.RequirementRevision, req.Revision)
				}
			})
		}
	})

	t.Run("GREEN: the tier decides the graph and nothing else does", func(t *testing.T) {
		cases := []struct {
			name string
			in   rules.PromotionApprovalInput
			tier rules.ApprovalTier
			want []string
		}{
			{"standard", humanwork.PromotionInputStandard(), rules.ApprovalTierStandard,
				[]string{
					humanwork.RequirementCurrentManager,
					humanwork.RequirementHRBP,
					humanwork.RequirementCompensationPartner,
				}},
			{"finance", humanwork.PromotionInputFinance(), rules.ApprovalTierFinanceRequired,
				[]string{
					humanwork.RequirementCurrentManager,
					humanwork.RequirementHRBP,
					humanwork.RequirementCompensationPartner,
					humanwork.RequirementFinancePartner,
				}},
			{"executive", humanwork.PromotionInputExecutive(), rules.ApprovalTierExecutiveRequired,
				[]string{
					humanwork.RequirementCurrentManager,
					humanwork.RequirementHRBP,
					humanwork.RequirementCompensationPartner,
					humanwork.RequirementFinancePartner,
					humanwork.RequirementExecutiveCommittee,
				}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				sc, err := humanwork.NewPromotionScenario(tc.in)
				if err != nil {
					t.Fatalf("scenario: %v", err)
				}
				if sc.Tier.Tier != tc.tier {
					t.Fatalf("tier = %s, want %s", sc.Tier.Tier, tc.tier)
				}
				var got []string
				for _, r := range sc.Requirements.Requirements {
					got = append(got, r.RequirementID)
					if r.Source.TableDigest != sc.Tier.TableDigest || r.Source.TableDigest == "" {
						t.Fatalf("%s cites table digest %q", r.RequirementID, r.Source.TableDigest)
					}
					if r.Source.MatchedRowID != sc.Tier.MatchedRowID {
						t.Fatalf("%s cites row %q, want %q",
							r.RequirementID, r.Source.MatchedRowID, sc.Tier.MatchedRowID)
					}
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("requirements = %v, want %v", got, tc.want)
				}
				// Stages are strictly non-decreasing, which is what makes the
				// ordering a property of the set rather than of iteration.
				var prev uint32
				for _, r := range sc.Requirements.Requirements {
					if r.Stage < prev {
						t.Fatalf("stage %d follows %d", r.Stage, prev)
					}
					prev = r.Stage
				}
				resolutions, err := sc.ResolveAll()
				if err != nil {
					t.Fatalf("resolve all: %v", err)
				}
				for _, r := range resolutions {
					if r.Outcome != humanwork.OutcomeResolved {
						t.Fatalf("%s = %s\n%s", r.RequirementID, r.Outcome, r.Explain())
					}
				}
			})
		}
	})

	t.Run("GREEN: an unresolved input states no requirement", func(t *testing.T) {
		_, err := humanwork.NewPromotionScenario(humanwork.PromotionInputUnresolved())
		if !errors.Is(err, humanwork.ErrTierBlocked) {
			t.Fatalf("NewPromotionScenario = %v, want ErrTierBlocked", err)
		}
	})

	t.Run("REFACTOR: a candidate is not a task recipient", func(t *testing.T) {
		// The resolution carries no assignee, recipient, queue or task field,
		// so there is nowhere for routing to be mistaken for authority. The
		// only authority answer this type gives is Authorizes.
		forbidden := []string{"assignee", "recipient", "queue", "task", "workitem", "claim"}
		rt := reflect.TypeOf(humanwork.Resolution{})
		for i := 0; i < rt.NumField(); i++ {
			name := strings.ToLower(rt.Field(i).Name)
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Fatalf("Resolution.%s conflates routing with decision authority",
						rt.Field(i).Name)
				}
			}
		}
		ct := reflect.TypeOf(humanwork.Candidate{})
		for i := 0; i < ct.NumField(); i++ {
			name := strings.ToLower(ct.Field(i).Name)
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Fatalf("Candidate.%s conflates routing with decision authority",
						ct.Field(i).Name)
				}
			}
		}
	})
}

// TestTodo_APPROVAL_001_Mutation proves that every field the requirement claims
// to bind actually reaches its canonical encoding.
//
// A mutation that dropped a field from ApprovalRequirement.Canonical would let
// two materially different requirements share a digest, and a resolution
// citing that digest would then prove nothing. Each case below changes exactly
// one field and requires the digest to move.
func TestTodo_APPROVAL_001_Mutation(t *testing.T) {
	base, err := humanwork.Compile(validSpec())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	org := humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}
	at := humanwork.ScenarioAt().Time()

	cases := []struct {
		name   string
		mutate func(*humanwork.RequirementSpec)
	}{
		{"requirement id", func(s *humanwork.RequirementSpec) { s.RequirementID = "req.other/v1" }},
		{"revision", func(s *humanwork.RequirementSpec) { s.Revision = 2 }},
		{"stage", func(s *humanwork.RequirementSpec) { s.Stage = 7 }},
		{"candidate expression", func(s *humanwork.RequirementSpec) {
			s.Candidates = humanwork.Role(humanwork.RoleFinancePartner, org)
		}},
		{"authority floor", func(s *humanwork.RequirementSpec) {
			s.AuthorityFloor = []string{humanwork.RoleHRBP, humanwork.RoleFinancePartner}
		}},
		{"quorum size", func(s *humanwork.RequirementSpec) {
			s.Quorum = humanwork.Quorum{MinApprovals: 2, RequireDistinctPrincipals: true}
		}},
		{"quorum distinctness", func(s *humanwork.RequirementSpec) {
			s.Quorum = humanwork.Quorum{MinApprovals: 1, RequireDistinctPrincipals: true}
		}},
		{"decision deadline", func(s *humanwork.RequirementSpec) {
			s.Deadline.DecideBy = values.NewInstant(at.Add(72 * time.Hour))
		}},
		{"decision expiry", func(s *humanwork.RequirementSpec) {
			s.Deadline.Expiry = values.NewInstant(at.Add(60 * 24 * time.Hour))
		}},
		{"escalation action", func(s *humanwork.RequirementSpec) {
			s.Escalation.OnDeadline = humanwork.EscalationBlock
		}},
		{"escalation fallback", func(s *humanwork.RequirementSpec) {
			fb := humanwork.Group(humanwork.ScenarioFallbackGroupID, org)
			s.Escalation = humanwork.EscalationPolicy{
				OnDeadline: humanwork.EscalationReassignToFallback,
				Fallback:   &fb,
				RuleID:     s.Escalation.RuleID,
			}
		}},
		{"escalation rule id", func(s *humanwork.RequirementSpec) {
			s.Escalation.RuleID = "policy.escalate/v2"
		}},
		{"separation: subject", func(s *humanwork.RequirementSpec) { s.Separation.SubjectMayNotApprove = true }},
		{"separation: dual role", func(s *humanwork.RequirementSpec) {
			s.Separation.OneRequirementPerPrincipal = true
		}},
		{"separation: manager chain", func(s *humanwork.RequirementSpec) {
			s.Separation.ForbidRequesterManagerChain = true
		}},
		{"separation rule id", func(s *humanwork.RequirementSpec) { s.Separation.RuleID = "policy.sod/v2" }},
		{"invalidator set", func(s *humanwork.RequirementSpec) {
			s.Invalidators = append(s.Invalidators, humanwork.Invalidator{
				Kind: humanwork.InvalidatorMandatoryDeny, RuleID: "policy.deny/v1",
			})
		}},
		{"tier", func(s *humanwork.RequirementSpec) { s.Source.Tier = rules.ApprovalTierFinanceRequired }},
		{"table version", func(s *humanwork.RequirementSpec) { s.Source.TableVersion = "2027.1" }},
		{"table digest", func(s *humanwork.RequirementSpec) { s.Source.TableDigest = "other-digest" }},
		{"matched row", func(s *humanwork.RequirementSpec) { s.Source.MatchedRowID = "other-row" }},
		{"governance policy", func(s *humanwork.RequirementSpec) {
			s.Source.GovernancePolicyRef = "policy.other/2026.1"
		}},
	}

	seen := map[string]string{base.Digest(): "base"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(&spec)
			got, err := humanwork.Compile(spec)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			d := got.Digest()
			if d == "" {
				t.Fatal("mutated requirement has no digest")
			}
			if prev, ok := seen[d]; ok {
				t.Fatalf("changing the %s did not move the digest (collides with %s)", tc.name, prev)
			}
			seen[d] = tc.name
		})
	}
}

// FuzzTodo_APPROVAL_001 fuzzes the expression compiler.
//
// The invariants are the ones that make the language bounded rather than a
// scripting surface: validation always terminates and never panics on
// arbitrary structure, a validating expression always canonicalizes to a
// non-empty digest, its leaf terms never exceed the node bound, and resolving
// it against an empty directory yields a stated NO_AUTHORIZED_APPROVER rather
// than an empty success.
func FuzzTodo_APPROVAL_001(f *testing.F) {
	f.Add([]byte{0}, uint8(1))
	f.Add([]byte{1, 2, 3, 4, 5}, uint8(3))
	f.Add([]byte{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}, uint8(2))
	f.Add([]byte{5, 0, 6, 1, 4, 2, 3}, uint8(9))

	empty := humanwork.NewMemoryDirectory("directory.fuzz/v1")
	f.Fuzz(func(t *testing.T, seed []byte, minDistinct uint8) {
		expr := buildExpression(seed, uint32(minDistinct), 0)
		err := expr.Validate()
		if err != nil {
			if expr.Digest() != "" {
				t.Fatalf("invalid expression produced a digest")
			}
			return
		}
		if expr.Digest() == "" {
			t.Fatalf("valid expression produced no digest")
		}
		if n := len(expr.Terms()); n > humanwork.MaxExpressionNodes {
			t.Fatalf("terms = %d, past the %d-node bound", n, humanwork.MaxExpressionNodes)
		}
		spec := validSpec()
		spec.Candidates = expr
		req, err := humanwork.Compile(spec)
		if err != nil {
			t.Fatalf("compile a valid expression: %v", err)
		}
		res, err := humanwork.Resolve(req, resolutionInput(), empty, humanwork.ScenarioClock())
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Outcome != humanwork.OutcomeNoAuthorizedApprover {
			t.Fatalf("an empty directory resolved %s with %d candidates",
				res.Outcome, len(res.Candidates))
		}
	})
}

// buildExpression decodes a seed into an expression tree. It deliberately can
// produce invalid shapes - over-deep nesting, unnamed roles, quorums past their
// child count - because those are the inputs the compiler has to refuse.
func buildExpression(seed []byte, minDistinct uint32, depth int) humanwork.Expression {
	org := humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}
	if len(seed) == 0 {
		return humanwork.Role(humanwork.RoleHRBP, org)
	}
	head, rest := seed[0], seed[1:]
	switch head % 8 {
	case 0:
		return humanwork.Named(humanwork.PrincipalHRBP, "policy.pinned/v1", org)
	case 1:
		return humanwork.Named(humanwork.PrincipalHRBP, "", org)
	case 2:
		return humanwork.Role(humanwork.RoleHRBP, org)
	case 3:
		return humanwork.Role("", org)
	case 4:
		return humanwork.Group("group.x", humanwork.Scope{Kind: humanwork.ScopeTenant})
	case 5:
		return humanwork.AnyOf(
			buildExpression(rest, minDistinct, depth+1),
			humanwork.Role(humanwork.RoleFinancePartner, org))
	case 6:
		return humanwork.AllOf(
			buildExpression(rest, minDistinct, depth+1),
			humanwork.Role(humanwork.RoleFinancePartner, org))
	default:
		return humanwork.QuorumOf(minDistinct,
			buildExpression(rest, minDistinct, depth+1),
			humanwork.Role(humanwork.RoleFinancePartner, org))
	}
}

func resolutionInput() humanwork.ResolutionInput {
	return humanwork.ResolutionInput{
		RequesterPrincipalID: humanwork.PrincipalRequester,
		SubjectPrincipalIDs:  []string{humanwork.PrincipalSubject},
		EffectiveAt:          humanwork.ScenarioAt(),
	}
}

func principalIDs(r humanwork.Resolution) []string {
	var out []string
	for _, c := range r.Candidates {
		out = append(out, c.PrincipalID)
	}
	return out
}
