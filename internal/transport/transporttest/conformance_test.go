package transporttest

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// fakeReporter is a minimal [Reporter] that records failures instead of
// calling testing.T.Fatal, so a test can assert on what AssertConformant
// reports without failing the outer test the moment a deliberate mismatch is
// fed to it.
type fakeReporter struct {
	messages []string
}

func (f *fakeReporter) Helper() {}
func (f *fakeReporter) Errorf(format string, args ...any) {
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

func TestRefusalMatrix_HasEightNamedRowsWithExactlyTwoSuccesses(t *testing.T) {
	matrix := RefusalMatrix()
	if len(matrix) != 8 {
		t.Fatalf("len(RefusalMatrix()) = %d, want 8", len(matrix))
	}
	seen := map[string]bool{}
	successes := 0
	for _, sc := range matrix {
		if sc.Name == "" {
			t.Error("a scenario has no name")
		}
		if seen[sc.Name] {
			t.Errorf("duplicate scenario name %q", sc.Name)
		}
		seen[sc.Name] = true
		if sc.WantSuccess {
			successes++
			continue
		}
		if sc.WantCode == envelope.CodeUnspecified {
			t.Errorf("%s: a refusal row must declare a non-zero WantCode", sc.Name)
		}
	}
	if successes != 2 {
		t.Fatalf("successes = %d, want 2 (deadline beyond cap, valid call)", successes)
	}
	for _, name := range []string{
		"missing credential", "wrong audience", "expired token",
		"caller-selected trusted field", "oversized message", "unknown field",
		"deadline beyond cap", "valid call",
	} {
		if !seen[name] {
			t.Errorf("RefusalMatrix is missing the %q row", name)
		}
	}
}

func TestConformanceScenario_EffectiveDeadline(t *testing.T) {
	withOverride := ConformanceScenario{Deadline: 5 * time.Second}
	if got := withOverride.EffectiveDeadline(time.Second); got != 5*time.Second {
		t.Errorf("EffectiveDeadline() = %v, want the scenario override", got)
	}
	withoutOverride := ConformanceScenario{}
	if got := withoutOverride.EffectiveDeadline(3 * time.Second); got != 3*time.Second {
		t.Errorf("EffectiveDeadline() = %v, want the caller default", got)
	}
}

func TestWrongAudienceClaims_OnlyAudienceDiffers(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	base := DefaultClaims(now)
	wrong := WrongAudienceClaims(now)
	if wrong.Audience == base.Audience {
		t.Fatal("WrongAudienceClaims did not change the audience")
	}
	wrong.Audience = base.Audience
	if !reflect.DeepEqual(wrong, base) {
		t.Errorf("WrongAudienceClaims changed more than the audience: %+v vs %+v", wrong, base)
	}
}

func TestExpiredClaims_WindowAlreadyClosed(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	claims := ExpiredClaims(now)
	if claims.ExpiresAtUnix >= now.Unix() {
		t.Errorf("ExpiresAtUnix = %d, want it before now (%d)", claims.ExpiresAtUnix, now.Unix())
	}
	if claims.IssuedAtUnix >= claims.ExpiresAtUnix {
		t.Errorf("IssuedAtUnix (%d) is not before ExpiresAtUnix (%d)", claims.IssuedAtUnix, claims.ExpiresAtUnix)
	}
}

func TestWithUnknownField_SetsUnrecognizedBytes(t *testing.T) {
	base := &intentsv1.GetIntentRequest{IntentId: KnownIntentID}
	tagged, ok := WithUnknownField(base, ConformanceUnknownFieldNumber).(*intentsv1.GetIntentRequest)
	if !ok {
		t.Fatalf("WithUnknownField returned %T, want *intentsv1.GetIntentRequest", tagged)
	}
	if len(tagged.ProtoReflect().GetUnknown()) == 0 {
		t.Fatal("WithUnknownField did not set any unknown bytes")
	}
	if tagged.GetIntentId() != KnownIntentID {
		t.Errorf("WithUnknownField changed a known field: IntentId = %q", tagged.GetIntentId())
	}
	if len(base.ProtoReflect().GetUnknown()) != 0 {
		t.Error("WithUnknownField mutated its input rather than cloning it")
	}
}

func TestAssertConformant_AgreeingOutcomesReportNothing(t *testing.T) {
	scenario := ConformanceScenario{Name: "valid call", WantSuccess: true}
	r := &fakeReporter{}
	AssertConformant(r, scenario, map[string]ConformanceOutcome{
		"grpc": {}, "edge": {}, "tunnel": {},
	})
	if len(r.messages) != 0 {
		t.Errorf("AssertConformant reported %v for three agreeing successes", r.messages)
	}
}

func TestAssertConformant_DetectsASuccessVsFailureMismatch(t *testing.T) {
	scenario := ConformanceScenario{Name: "missing credential", WantCode: envelope.CodeUnauthenticated}
	r := &fakeReporter{}
	AssertConformant(r, scenario, map[string]ConformanceOutcome{
		"grpc": {Err: envelope.New(envelope.CodeUnauthenticated, "authentication.missing_credential", "no valid authentication")},
		"edge": {}, // wrongly succeeded
	})
	if len(r.messages) == 0 {
		t.Fatal("AssertConformant did not flag a success-vs-failure mismatch")
	}
}

func TestAssertConformant_DetectsADifferingCode(t *testing.T) {
	scenario := ConformanceScenario{Name: "oversized message", WantCode: envelope.CodeInvalidArgument}
	r := &fakeReporter{}
	AssertConformant(r, scenario, map[string]ConformanceOutcome{
		"grpc": {Err: envelope.New(envelope.CodeInvalidArgument, "structural.request_rejected", "malformed")},
		"edge": {Err: envelope.New(envelope.CodeUnavailable, "structural.request_rejected", "malformed")},
	})
	if len(r.messages) == 0 {
		t.Fatal("AssertConformant did not flag a differing owned code")
	}
}

func TestAssertConformant_DetectsADifferingViolation(t *testing.T) {
	scenario := ConformanceScenario{Name: "unknown field", WantCode: envelope.CodeInvalidArgument}
	grpcErr := envelope.New(envelope.CodeInvalidArgument, "structural.request_rejected", "malformed").
		WithViolation("(request)", "the message carries unknown fields", "strict_decoding.unknown_field")
	edgeErr := envelope.New(envelope.CodeInvalidArgument, "structural.request_rejected", "malformed").
		WithViolation("intent_id", "the message carries unknown fields", "strict_decoding.unknown_field")
	r := &fakeReporter{}
	AssertConformant(r, scenario, map[string]ConformanceOutcome{"grpc": {Err: grpcErr}, "edge": {Err: edgeErr}})
	if len(r.messages) == 0 {
		t.Fatal("AssertConformant did not flag a differing violation field path")
	}
}

func TestAssertConformant_ChecksAgainstTheDeclaredExpectation(t *testing.T) {
	scenario := ConformanceScenario{Name: "valid call", WantSuccess: true}
	r := &fakeReporter{}
	AssertConformant(r, scenario, map[string]ConformanceOutcome{
		"grpc": {Err: envelope.New(envelope.CodeUnavailable, "x", "boom")},
		"edge": {Err: envelope.New(envelope.CodeUnavailable, "x", "boom")},
	})
	if len(r.messages) == 0 {
		t.Fatal("AssertConformant did not flag that a should-succeed row failed everywhere")
	}
}

func TestAssertConformant_NoOutcomesIsReported(t *testing.T) {
	r := &fakeReporter{}
	AssertConformant(r, ConformanceScenario{Name: "empty"}, map[string]ConformanceOutcome{})
	if len(r.messages) != 1 {
		t.Fatalf("messages = %v, want exactly one complaint about no edges", r.messages)
	}
}
