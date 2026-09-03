package bufprotovalidatekit_test

import (
	"errors"
	"reflect"
	"testing"

	kit "github.com/monstercameron/hcm-next/tools/quality/bufprotovalidatekit"
	"google.golang.org/protobuf/proto"
)

// TestBufProtovalidateQualificationCannotBecomeBusinessAuthority is LIB-019's
// primary test. The candidate boundary may inspect request shape, but cannot
// decide authorization, legality, eligibility, or mutations.
func TestBufProtovalidateQualificationCannotBecomeBusinessAuthority(t *testing.T) {
	validator, descriptor := fixtureValidator(t)
	message := requestMessage(descriptor, "worker-1", "ok", "type.googleapis.com/qualification.v1.Attachment", []byte{0x0a, 0x00})
	before := proto.Clone(message)
	if violations := validator.Validate(message); len(violations) != 0 {
		t.Fatalf("valid structural message rejected: %+v", violations)
	}
	if !proto.Equal(before, message) {
		t.Fatal("structural validation mutated its input")
	}

	for _, scope := range []kit.Scope{kit.ScopeBusiness, kit.ScopeAuthorization, kit.ScopeLegal, kit.ScopeEligibility, kit.ScopeMutation} {
		spec := fixtureSpec(t)
		spec.Rules = append(spec.Rules, kit.Rule{
			RuleRef: "forbidden.authority", FieldPath: "worker_id", Kind: kit.RuleRequired, Scope: scope,
		})
		_, err := kit.Compile(descriptor, spec)
		if !errors.Is(err, kit.ErrAuthorityScope) {
			t.Errorf("scope %q: error=%v, want ErrAuthorityScope", scope, err)
		}
	}

	spec := fixtureSpec(t)
	spec.Rules = append(spec.Rules, kit.Rule{
		RuleRef: "invalid.cel", FieldPath: "worker_id", Kind: kit.RuleCEL, Scope: kit.ScopeStructural,
		Expression: "this..is invalid",
	})
	if _, err := kit.Compile(descriptor, spec); !errors.Is(err, kit.ErrUnsupportedRule) {
		t.Fatalf("CEL rule published: %v", err)
	}

	diagnostics := kit.LintFile(descriptor.ParentFile())
	if len(diagnostics) != 0 {
		t.Fatalf("Buf-compatible descriptor fixture did not lint: %+v", diagnostics)
	}

	got := validator.Validate(requestMessage(descriptor, "", "123456789", "type.googleapis.com/evil.v1.Payload", []byte{1}))
	wantCodes := []string{"REQUIRED", "MAX_BYTES", "ANY_TYPE_NOT_ALLOWED"}
	codes := make([]string, len(got))
	for i := range got {
		codes[i] = got[i].Code
	}
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("violation codes=%v want=%v; violations=%+v", codes, wantCodes, got)
	}
}
