package abuse_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

func TestExplainMatchesDeclaredInput(t *testing.T) {
	s := validActivitySignal()
	v := validDetectorVersion()
	exp := abuse.Explain(s, v)
	if !exp.Applicable {
		t.Fatalf("Explain() did not consider a declared-input signal applicable: %+v", exp)
	}
	if exp.MatchedInput != s.Kind {
		t.Fatalf("MatchedInput = %q, want %q", exp.MatchedInput, s.Kind)
	}
	if exp.Reason != abuse.ExplainReasonDeclaredInput {
		t.Fatalf("Reason = %q, want %q", exp.Reason, abuse.ExplainReasonDeclaredInput)
	}
	if exp.DetectorID != v.DetectorID || exp.Semver != v.Semver || exp.SignalID != s.ID {
		t.Fatalf("Explain() lost identity: %+v", exp)
	}
}

func TestExplainDoesNotMatchUndeclaredInput(t *testing.T) {
	s := validActivitySignal()
	s.Kind = abuse.SignalKindAuthAnomaly
	v := validDetectorVersion() // only declares SignalKindSensitiveRead
	exp := abuse.Explain(s, v)
	if exp.Applicable {
		t.Fatalf("Explain() matched a signal kind the detector never declared: %+v", exp)
	}
	if exp.Reason != abuse.ExplainReasonNotDeclaredInput {
		t.Fatalf("Reason = %q, want %q", exp.Reason, abuse.ExplainReasonNotDeclaredInput)
	}
}

func TestExplainRefusesInvalidSignal(t *testing.T) {
	s := validActivitySignal()
	s.Tenant = ""
	exp := abuse.Explain(s, validDetectorVersion())
	if exp.Applicable {
		t.Fatal("Explain() considered an invalid signal applicable")
	}
	if exp.Reason != abuse.ExplainReasonSignalInvalid {
		t.Fatalf("Reason = %q, want %q", exp.Reason, abuse.ExplainReasonSignalInvalid)
	}
}

func TestExplainRefusesInvalidDetectorVersion(t *testing.T) {
	v := validDetectorVersion()
	v.ActivatedAt = time.Time{}
	exp := abuse.Explain(validActivitySignal(), v)
	if exp.Applicable {
		t.Fatal("Explain() considered an unpublishable detector version applicable")
	}
	if exp.Reason != abuse.ExplainReasonDetectorVersionInvalid {
		t.Fatalf("Reason = %q, want %q", exp.Reason, abuse.ExplainReasonDetectorVersionInvalid)
	}
}
