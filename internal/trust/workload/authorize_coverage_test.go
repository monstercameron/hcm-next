package workload

import (
	"errors"
	"testing"
	"time"
)

func TestAuthorize_RequestValidationAndDecisionSurface(t *testing.T) {
	if ActionRead.valid() == false || ActionWrite.valid() == false || Action("delete").valid() {
		t.Fatal("action vocabulary validation is incorrect")
	}
	for _, tc := range []struct {
		effect Effect
		want   string
	}{
		{EffectAllow, "ALLOW"}, {EffectDeny, "DENY"}, {EffectUnspecified, "EFFECT_UNSPECIFIED"}, {Effect(99), "EFFECT_UNSPECIFIED"},
	} {
		if got := tc.effect.String(); got != tc.want {
			t.Errorf("Effect(%d).String() = %q, want %q", tc.effect, got, tc.want)
		}
	}
	if _, err := Authorize(Request{Resource: ResourceLedger, Action: "delete"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid action = %v, want ErrInvalidRequest", err)
	}
	if _, err := Authorize(Request{Action: ActionRead}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty resource = %v, want ErrInvalidRequest", err)
	}
	zero, err := Authorize(Request{Resource: ResourceLedger, Action: ActionRead, EvaluatedAt: time.Unix(1, 0)})
	if err != nil || zero.Effect != EffectDeny || zero.Reason != ReasonNoVerifiedIdentity || zero.DecisionID == "" {
		t.Fatalf("zero caller decision = %+v, %v", zero, err)
	}
	unknown := Identity{role: ProcessRole("forged"), cell: "cell", subject: "subject", issuedAt: time.Unix(1, 0), expiresAt: time.Unix(2, 0)}
	decision, err := Authorize(Request{Caller: unknown, Resource: ResourceLedger, Action: ActionRead, EvaluatedAt: time.Unix(1, 0)})
	if err != nil || decision.Effect != EffectDeny || decision.Reason != ReasonUnknownRole {
		t.Fatalf("unknown role decision = %+v, %v", decision, err)
	}
	expired := Identity{role: RoleWorker, cell: "cell", subject: "subject", issuedAt: time.Unix(1, 0), expiresAt: time.Unix(2, 0)}
	decision, err = Authorize(Request{Caller: expired, Resource: ResourceLedger, Action: ActionRead, EvaluatedAt: time.Unix(2, 0)})
	if err != nil || decision.Effect != EffectDeny || decision.Reason != ReasonIdentityExpired {
		t.Fatalf("expired decision = %+v, %v", decision, err)
	}
	allowed, err := Authorize(Request{Caller: expired, Resource: ResourceLedger, Action: ActionRead})
	if err != nil || allowed.Effect != EffectAllow || allowed.RuleID != "svc.worker.ledger.read" || allowed.DecisionID == "" {
		t.Fatalf("zero evaluated time allow = %+v, %v", allowed, err)
	}
	if allowed.Caller != RoleWorker || allowed.Resource != ResourceLedger || allowed.Action != ActionRead {
		t.Fatalf("decision did not retain request fields: %+v", allowed)
	}
	noRule, err := Authorize(Request{Caller: expired, Resource: ResourceSchema, Action: ActionRead})
	if err != nil || noRule.Effect != EffectDeny || noRule.Reason != ReasonNoMatchingRule {
		t.Fatalf("missing matrix rule decision = %+v, %v", noRule, err)
	}
	if allowed.DecisionID == noRule.DecisionID {
		t.Fatal("allow and deny decisions collided")
	}
}

func TestAuthorize_AllMatrixBranchesAndReservedRoles(t *testing.T) {
	roles := []ProcessRole{RoleWorker, RoleProjector, RoleHCMNext, RoleMigrate}
	for _, role := range roles {
		if _, ok := allowIndex[role]; !ok {
			t.Fatalf("allow index has no entry for %q", role)
		}
	}
	if _, ok := allowIndex[RoleScheduler]; ok {
		t.Fatal("reserved scheduler role unexpectedly has an allow index")
	}
	reserved := Identity{role: RoleAdmin, subject: "admin", issuedAt: time.Unix(1, 0), expiresAt: time.Unix(2, 0)}
	decision, err := Authorize(Request{Caller: reserved, Resource: ResourceSchema, Action: ActionWrite})
	if err != nil || decision.Effect != EffectDeny || decision.Reason != ReasonNoMatchingRule {
		t.Fatalf("reserved role decision = %+v, %v", decision, err)
	}
	for _, rule := range AllowMatrix {
		caller := Identity{role: rule.Caller, subject: string(rule.Caller), issuedAt: time.Unix(1, 0), expiresAt: time.Unix(2, 0)}
		decision, err := Authorize(Request{Caller: caller, Resource: rule.Resource, Action: rule.Action, EvaluatedAt: time.Unix(1, 0)})
		if err != nil || decision.Effect != EffectAllow || decision.RuleID != rule.RuleID {
			t.Fatalf("matrix rule %q produced %+v, %v", rule.RuleID, decision, err)
		}
	}
}

func TestAuthorize_DecisionIDsAndDenyHelperAreStable(t *testing.T) {
	base := Decision{Effect: EffectDeny, Reason: ReasonNoMatchingRule, Caller: RoleWorker, Resource: ResourceLedger, Action: ActionRead, EvaluatedAt: time.Unix(10, 20)}
	first := deny(base, ReasonNoMatchingRule)
	second := deny(base, ReasonNoMatchingRule)
	if first.DecisionID == "" || first.DecisionID != second.DecisionID || first.Effect != EffectDeny || first.Reason != ReasonNoMatchingRule {
		t.Fatalf("deny helper is not deterministic: first=%+v second=%+v", first, second)
	}
	if decisionID(first) != first.DecisionID {
		t.Fatal("deny helper did not use decisionID")
	}
	changed := first
	changed.EvaluatedAt = changed.EvaluatedAt.Add(time.Nanosecond)
	if decisionID(changed) == first.DecisionID {
		t.Fatal("decisionID ignored evaluated time")
	}
}
