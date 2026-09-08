package replan

import (
	"errors"
	"testing"
)

func reuseFixture() (ReusePolicy, SuccessorProposal, SuccessorProposal, []Decision) {
	return ReusePolicy{PolicyRef: "policy/approval-reuse/v3", PolicyDigest: "policy-digest-3", Version: 3, PermittedChangeKinds: map[ChangeKind]bool{ChangeView: true, ChangePresentation: true}},
		SuccessorProposal{TenantID: "tenant-1", RevisionID: "rev-1", Digest: "digest-1"}, SuccessorProposal{TenantID: "tenant-1", RevisionID: "rev-2", Digest: "digest-2"},
		[]Decision{{TenantID: "tenant-1", DecisionID: "decision-1", ProposalRevisionID: "rev-1", ProposalDigest: "digest-1", Receipt: DecisionReceipt{TenantID: "tenant-1", DecisionID: "decision-1", ProposalRevisionID: "rev-1", ProposalDigest: "digest-1", ReceiptDigest: "receipt-1"}}}
}

func TestTodo_REPLAN_002(t *testing.T) {
	policy, from, to, decisions := reuseFixture()
	result, err := EvaluateReuse(policy, from, to, decisions, []ProposalChange{{Kind: ChangePresentation}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Decisions) != 1 || result.Decisions[0].Status != RetainedByReference {
		t.Fatalf("result=%+v", result)
	}
	link := result.Decisions[0].Applicability
	if link == nil || link.ToRevisionID != to.RevisionID || link.ToProposalDigest != to.Digest || link.FromProposalDigest != from.Digest {
		t.Fatalf("applicability=%+v", link)
	}
	if result.Decisions[0].OriginalReceipt.ReceiptDigest != "receipt-1" || result.Digest == "" {
		t.Fatalf("receipt/digest missing: %+v", result)
	}
	if result.TenantID != from.TenantID || result.PolicyRef != policy.PolicyRef || result.PolicyDigest != policy.PolicyDigest || link.TenantID != from.TenantID {
		t.Fatalf("scope/policy binding missing: result=%+v applicability=%+v", result, link)
	}
}

func TestTodo_REPLAN_002_Property(t *testing.T) {
	policy, from, to, decisions := reuseFixture()
	for _, kind := range []ChangeKind{ChangeAmount, ChangeInterval, ChangeObligation} {
		result, err := EvaluateReuse(policy, from, to, decisions, []ProposalChange{{Kind: kind}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Decisions[0].Status != ReapprovalRequired || result.Decisions[0].Applicability != nil {
			t.Fatalf("kind %s result=%+v", kind, result.Decisions[0])
		}
	}
	result, err := EvaluateReuse(policy, from, to, decisions, []ProposalChange{{Kind: ChangeView}})
	if err != nil || result.Decisions[0].Status != RetainedByReference {
		t.Fatalf("permitted view result=%+v err=%v", result, err)
	}
	policy.PermittedChangeKinds = nil
	result, err = EvaluateReuse(policy, from, to, decisions, []ProposalChange{{Kind: ChangeView}})
	if err != nil || result.Decisions[0].Status != ReapprovalRequired {
		t.Fatalf("unpermitted view result=%+v err=%v", result, err)
	}
	second := decisions[0]
	second.DecisionID = "decision-2"
	second.Receipt.DecisionID = "decision-2"
	decisions = append(decisions, second)
	result, err = EvaluateReuse(policy, from, to, decisions, []ProposalChange{{DecisionID: "decision-1", Kind: ChangeAmount}})
	if err != nil || result.Decisions[0].Status != ReapprovalRequired || result.Decisions[1].Status != RetainedByReference {
		t.Fatalf("scoped change result=%+v err=%v", result, err)
	}
}

func TestTodo_REPLAN_002_Mutation(t *testing.T) {
	policy, from, to, decisions := reuseFixture()
	original := decisions[0]
	result, err := EvaluateReuse(policy, from, to, decisions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decisions[0] != original {
		t.Fatalf("decision was mutated: before=%+v after=%+v", original, decisions[0])
	}
	if result.Decisions[0].OriginalReceipt != original.Receipt {
		t.Fatalf("receipt was rebound: %+v", result.Decisions[0])
	}
	if _, err := EvaluateReuse(ReusePolicy{}, from, to, decisions, nil); !errors.Is(err, ErrInvalidReusePolicy) {
		t.Fatalf("invalid policy error=%v", err)
	}
	bad := decisions[0]
	bad.ProposalDigest = "forged"
	if _, err := EvaluateReuse(policy, from, to, []Decision{bad}, nil); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("forged binding error=%v", err)
	}
	bad = decisions[0]
	bad.TenantID = "tenant-2"
	if _, err := EvaluateReuse(policy, from, to, []Decision{bad}, nil); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("cross-tenant decision error=%v", err)
	}
	badTo := to
	badTo.TenantID = "tenant-2"
	if _, err := EvaluateReuse(policy, from, badTo, decisions, nil); !errors.Is(err, ErrInvalidSuccessor) {
		t.Fatalf("cross-tenant successor error=%v", err)
	}
	badPolicy := policy
	badPolicy.PolicyDigest = ""
	if _, err := EvaluateReuse(badPolicy, from, to, decisions, nil); !errors.Is(err, ErrInvalidReusePolicy) {
		t.Fatalf("unbound policy error=%v", err)
	}
	result, err = EvaluateReuse(policy, from, to, decisions, []ProposalChange{{Kind: ChangeKind("future")}})
	if err != nil || result.Decisions[0].Status != ReuseUnknown {
		t.Fatalf("unknown change result=%+v err=%v", result, err)
	}
}
