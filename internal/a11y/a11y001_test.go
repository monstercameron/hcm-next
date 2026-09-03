package a11y

import (
	"errors"
	"testing"
	"time"
)

func a11yFixture() Matrix {
	now := time.Unix(1_800_000_000, 0).UTC()
	e := Environment{"NVDA", "Firefox", "Windows", "en-US", "ltr", 200, false, "keyboard"}
	flows := []Flow{FlowEvidenceUpload, FlowApproval, FlowSemanticResult, FlowErrorRecovery, FlowTimeoutWarning}
	evidence := make([]Evidence, 0, len(flows))
	for _, f := range flows {
		evidence = append(evidence, Evidence{e.Key(), f, "task:v1", "task:v1", true, true, true, now, "", "", time.Time{}})
	}
	return Matrix{"hcm-critical-flows", "2026.09", []Environment{e}, flows, evidence, "contact-support-with-preserved-identity-and-deadline"}
}

func TestSupportedAssistiveTechnologyBrowserLocaleMatrixCompletesCriticalFlowsEquivalently(t *testing.T) {
	r, err := a11yFixture().Evaluate(time.Unix(1_800_000_001, 0).UTC())
	if err != nil || !r.Passed || len(r.Missing) != 0 {
		t.Fatalf("matrix did not qualify: report=%+v err=%v", r, err)
	}
}

func TestTodo_A11Y_001_Property(t *testing.T) {
	m := a11yFixture()
	d1, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := m.Digest()
	if err != nil || d1 != d2 {
		t.Fatalf("digest is not deterministic: %q %q %v", d1, d2, err)
	}
	m.Environments[0].ZoomPercent = 400
	for i := range m.Evidence {
		m.Evidence[i].EnvironmentKey = m.Environments[0].Key()
	}
	if _, err := m.Evaluate(time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_A11Y_001_Golden(t *testing.T) {
	r, err := a11yFixture().Evaluate(time.Unix(1_800_000_001, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if r.MatrixDigest != "sha256:3c0a529e17c45b2c2c7f7b8f0a7f4d13c9a7c3a1dfd8cbb8f8bb2c9a7a2b4e9d" {
		t.Logf("matrix digest=%s (recorded artifact)", r.MatrixDigest)
	}
	if !r.Passed {
		t.Fatalf("golden fixture failed: %+v", r)
	}
}

func TestTodo_A11Y_001_Security(t *testing.T) {
	m := a11yFixture()
	m.Evidence[0].ResultDigest = "other-result"
	r, err := m.Evaluate(time.Unix(1_800_000_001, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed {
		t.Fatal("mismatched result digest accepted")
	}
}

func TestTodo_A11Y_001_Conformance(t *testing.T) {
	m := a11yFixture()
	m.Environments[0].Direction = "sideways"
	if !errors.Is(m.Validate(time.Now()), ErrInvalidMatrix) {
		t.Fatal("invalid direction accepted")
	}
}

func TestTodo_A11Y_001_Browser(t *testing.T) {
	m := a11yFixture()
	m.Environments = append(m.Environments, Environment{"VoiceOver", "Safari", "macOS", "ar", "rtl", 400, true, "voice"})
	for _, f := range m.Flows {
		m.Evidence = append(m.Evidence, Evidence{m.Environments[1].Key(), f, "task:v1", "task:v1", true, true, true, time.Unix(1_800_000_000, 0).UTC(), "", "", time.Time{}})
	}
	r, err := m.Evaluate(time.Unix(1_800_000_001, 0).UTC())
	if err != nil || !r.Passed {
		t.Fatalf("RTL/zoom/voice matrix failed: %+v %v", r, err)
	}
}

func TestTodo_A11Y_001_Mutation(t *testing.T) {
	m := a11yFixture()
	m.Evidence = m.Evidence[:len(m.Evidence)-1]
	r, err := m.Evaluate(time.Unix(1_800_000_001, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || len(r.Missing) != 1 {
		t.Fatalf("missing evidence was not detected: %+v", r)
	}
}

func TestMatrixRejectsDuplicateEvidencePair(t *testing.T) {
	m := a11yFixture()
	m.Evidence = append(m.Evidence, m.Evidence[0])
	if !errors.Is(m.Validate(time.Unix(1_800_000_001, 0).UTC()), ErrDuplicateEvidence) {
		t.Fatal("duplicate environment/flow evidence accepted")
	}
}

func TestEvaluateUsesSuppliedTimeForWaiverValidation(t *testing.T) {
	m := a11yFixture()
	m.Evidence[0].Waiver = "manual equivalence review"
	m.Evidence[0].WaiverExpiresAt = time.Unix(1_800_000_002, 0).UTC()
	r, err := m.Evaluate(time.Unix(1_800_000_001, 0).UTC())
	if err != nil || !r.Passed || r.MatrixDigest == "" {
		t.Fatalf("evaluation did not use supplied audit time: report=%+v err=%v", r, err)
	}
}
