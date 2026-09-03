package bufprotovalidatekit_test

import (
	"errors"
	"strings"
	"testing"

	kit "github.com/monstercameron/hcm-next/tools/quality/bufprotovalidatekit"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TestTodo_LIB_019_Security proves that sensitive values are not reflected,
// Any is allow-listed, and semantic/authority scopes fail closed at publish.
func TestTodo_LIB_019_Security(t *testing.T) {
	validator, descriptor := fixtureValidator(t)
	var missing *dynamicpb.Message
	if got := validator.Validate(missing); len(got) != 1 || got[0].Code != "INVALID_MESSAGE" {
		t.Fatalf("typed-nil message did not fail closed: %+v", got)
	}
	secret := "payroll-secret-token"
	violations := validator.Validate(requestMessage(descriptor, "", secret, "type.googleapis.com/private.Payroll", []byte(secret)))
	if !hasViolation(violations, "ANY_TYPE_NOT_ALLOWED") {
		t.Fatalf("unregistered dynamic type accepted: %+v", violations)
	}
	for _, violation := range violations {
		if strings.Contains(violation.Description, secret) || strings.Contains(violation.Description, "private.Payroll") {
			t.Fatalf("violation leaks restricted input: %+v", violation)
		}
	}

	for _, tc := range []kit.Rule{
		{RuleRef: "authz", FieldPath: "worker_id", Kind: kit.RuleRequired, Scope: kit.ScopeAuthorization},
		{RuleRef: "legal", FieldPath: "worker_id", Kind: kit.RuleRequired, Scope: kit.ScopeLegal},
		{RuleRef: "eligibility", FieldPath: "worker_id", Kind: kit.RuleRequired, Scope: kit.ScopeEligibility},
		{RuleRef: "mutation", FieldPath: "worker_id", Kind: kit.RuleRequired, Scope: kit.ScopeMutation},
	} {
		spec := fixtureSpec(t)
		spec.Rules = []kit.Rule{tc}
		if _, err := kit.Compile(descriptor, spec); !errors.Is(err, kit.ErrAuthorityScope) {
			t.Errorf("%s scope published: %v", tc.RuleRef, err)
		}
	}

	spec := fixtureSpec(t)
	spec.AllowedAnyTypeURLs = append(spec.AllowedAnyTypeURLs, "type.googleapis.com/private.Payroll")
	if _, err := kit.Compile(descriptor, spec); !errors.Is(err, kit.ErrInvalidRule) {
		t.Fatalf("nonlocal Any allow-list entry published: %v", err)
	}
}
