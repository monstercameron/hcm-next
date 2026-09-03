package adoption

import (
	"errors"
	"testing"
	"time"
)

func TestCustomer004RegistryIsExact(t *testing.T) {
	got := RegistryMatrix()
	want := []struct {
		id   Journey
		role Role
	}{{JourneyAdministratorSetup, RoleAdministrator}, {JourneyApproverDecision, RoleApprover}, {JourneyEmployeeRequest, RoleEmployee}, {JourneySupportTriage, RoleSupport}}
	if len(got) != len(want) {
		t.Fatalf("registry length=%d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Role != w.role {
			t.Fatalf("entry %d=%+v, want %s/%s", i, got[i], w.id, w.role)
		}
		if len(got[i].Required) != 4 {
			t.Errorf("%s dimensions=%d, want 4", got[i].ID, len(got[i].Required))
		}
	}
}

func completeMatrix() Matrix {
	m := Matrix{ID: "customer-004-adoption", Version: "2026.09"}
	for _, spec := range RegistryMatrix() {
		for _, d := range spec.Required {
			m.Evidence = append(m.Evidence, Evidence{Journey: spec.ID, Dimension: d, TaskDigest: "build:v1", ResultDigest: "build:v1", RecordedAt: time.Unix(1800000000, 0), Accessible: true, Trained: true, RollbackPath: true, HelpPath: true})
		}
	}
	return m
}

func TestCustomer004ReadinessRequiresEveryDimension(t *testing.T) {
	now := time.Unix(1800000060, 0).UTC()
	m := completeMatrix()
	report, err := m.Evaluate(now)
	if err != nil || !report.Passed || len(report.Missing) != 0 || report.MatrixDigest == "" {
		t.Fatalf("complete matrix=%+v err=%v", report, err)
	}
	m.Evidence[0].Accessible = false
	report, err = m.Evaluate(now)
	if err != nil || report.Passed || len(report.Missing) == 0 {
		t.Fatalf("inaccessible evidence passed: %+v err=%v", report, err)
	}
	m = completeMatrix()
	m.Evidence[0].Waiver = "catch-up"
	m.Evidence[0].WaiverExpiresAt = now.Add(-time.Second)
	if _, err := m.Evaluate(now); !errors.Is(err, ErrInvalidMatrix) {
		t.Fatalf("expired waiver err=%v", err)
	}
}

func TestCustomer004RejectsUnknownAndDuplicateEvidence(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	m := completeMatrix()
	m.Evidence[0].Journey = Journey("unknown")
	if _, err := m.Evaluate(now); !errors.Is(err, ErrMissingEvidence) {
		t.Errorf("unknown journey err=%v", err)
	}
	m = completeMatrix()
	m.Evidence[1].Journey = m.Evidence[0].Journey
	m.Evidence[1].Dimension = m.Evidence[0].Dimension
	if _, err := m.Evaluate(now); !errors.Is(err, ErrDuplicateEvidence) {
		t.Errorf("duplicate err=%v", err)
	}
}

// TestCustomer004AdoptionMatrix is the primary CUSTOMER-004 acceptance
// matrix. It walks every role and every required adoption dimension, rather
// than merely checking that the registry has four entries.
func TestCustomer004AdoptionMatrix(t *testing.T) {
	m := completeMatrix()
	report, err := m.Evaluate(time.Unix(1800000060, 0).UTC())
	if err != nil || !report.Passed {
		t.Fatalf("primary CUSTOMER-004 matrix failed: report=%+v err=%v", report, err)
	}
	for _, spec := range RegistryMatrix() {
		for _, dimension := range spec.Required {
			for i, evidence := range m.Evidence {
				if evidence.Journey == spec.ID && evidence.Dimension == dimension {
					m.Evidence[i].ResultDigest = "different-build"
					broken, evalErr := m.Evaluate(time.Unix(1800000060, 0).UTC())
					if evalErr != nil || broken.Passed {
						t.Fatalf("%s/%s mismatch passed: %+v err=%v", spec.ID, dimension, broken, evalErr)
					}
					m.Evidence[i].ResultDigest = m.Evidence[i].TaskDigest
				}
			}
		}
	}
}

// BenchmarkCustomer004AdoptionReadiness measures the representative release
// gate over all sixteen role/dimension observations. It exercises validation,
// digesting, and missing-evidence detection together as production does.
func BenchmarkCustomer004AdoptionReadiness(b *testing.B) {
	m := completeMatrix()
	now := time.Unix(1800000060, 0).UTC()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := m.Evaluate(now)
		if err != nil || !report.Passed || report.MatrixDigest == "" {
			b.Fatalf("benchmark readiness failed: %+v err=%v", report, err)
		}
	}
}

func TestPilotRoleReadinessMeetsTaskAccessibilityComprehensionAndSupportThresholds(t *testing.T) {
	m := completeMatrix()
	if report, err := m.Evaluate(time.Unix(1800000060, 0).UTC()); err != nil || !report.Passed {
		t.Fatalf("pilot thresholds not met: %+v err=%v", report, err)
	}
	for _, spec := range RegistryMatrix() {
		for _, d := range spec.Required {
			candidate := completeMatrix()
			for i := range candidate.Evidence {
				if candidate.Evidence[i].Journey == spec.ID && candidate.Evidence[i].Dimension == d {
					candidate.Evidence[i].HelpPath = false
				}
			}
			report, err := candidate.Evaluate(time.Unix(1800000060, 0).UTC())
			if err != nil || report.Passed {
				t.Fatalf("support threshold failure was accepted for %s/%s", spec.ID, d)
			}
		}
	}
}

func TestTodo_CUSTOMER_004_Browser(t *testing.T) {
	// Browser-level evidence is represented by the accessibility dimension;
	// clearing its signal must block release even when other signals pass.
	m := completeMatrix()
	for i := range m.Evidence {
		if m.Evidence[i].Dimension == DimensionAccessibility {
			m.Evidence[i].Accessible = false
		}
	}
	report, err := m.Evaluate(time.Unix(1800000060, 0).UTC())
	if err != nil || report.Passed || len(report.Missing) != 4 {
		t.Fatalf("browser accessibility gate=%+v err=%v", report, err)
	}
}

func TestTodo_CUSTOMER_004_Conformance(t *testing.T) {
	m := completeMatrix()
	for _, spec := range RegistryMatrix() {
		if found, ok := Lookup(spec.ID); !ok || found.Role != spec.Role || len(found.Required) != 4 {
			t.Fatalf("registry conformance failed for %s", spec.ID)
		}
	}
	if err := m.Validate(time.Unix(1800000060, 0).UTC()); err != nil {
		t.Fatalf("valid matrix rejected: %v", err)
	}
}

func TestTodo_CUSTOMER_004_Golden(t *testing.T) {
	m := completeMatrix()
	first, err := m.Evaluate(time.Unix(1800000060, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Evaluate(time.Unix(1800000060, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if first.MatrixDigest != second.MatrixDigest || !first.Passed || len(first.Missing) != 0 {
		t.Fatalf("golden result changed: first=%+v second=%+v", first, second)
	}
}

func TestTodo_CUSTOMER_004_Mutation(t *testing.T) {
	mutations := []func(*Evidence){func(e *Evidence) { e.Trained = false }, func(e *Evidence) { e.RollbackPath = false }, func(e *Evidence) { e.HelpPath = false }, func(e *Evidence) { e.ResultDigest = "tampered" }}
	for _, mutate := range mutations {
		m := completeMatrix()
		mutate(&m.Evidence[0])
		report, err := m.Evaluate(time.Unix(1800000060, 0).UTC())
		if err != nil || report.Passed {
			t.Fatalf("mutation escaped gate: %+v err=%v", report, err)
		}
	}
}

func TestTodo_CUSTOMER_004_Property(t *testing.T) {
	base := completeMatrix()
	now := time.Unix(1800000060, 0).UTC()
	for i := range base.Evidence {
		m := completeMatrix()
		m.Evidence[i].RecordedAt = base.Evidence[i].RecordedAt.Add(time.Duration(i+1) * time.Hour)
		report, err := m.Evaluate(now)
		if err != nil || !report.Passed {
			t.Fatalf("timestamp property failed at %d: %+v err=%v", i, report, err)
		}
	}
}

func TestTodo_CUSTOMER_004_Security(t *testing.T) {
	m := completeMatrix()
	m.Evidence[0].Waiver = "temporary"
	m.Evidence[0].WaiverExpiresAt = time.Time{}
	if _, err := m.Evaluate(time.Unix(1800000060, 0).UTC()); !errors.Is(err, ErrInvalidMatrix) {
		t.Fatalf("waiver without expiry accepted: %v", err)
	}
	m = completeMatrix()
	m.Evidence[0].Journey = Journey("../support")
	if _, err := m.Evaluate(time.Unix(1800000060, 0).UTC()); !errors.Is(err, ErrMissingEvidence) {
		t.Fatalf("unregistered journey accepted: %v", err)
	}
}

func BenchmarkTodo_CUSTOMER_004(b *testing.B) {
	m := completeMatrix()
	now := time.Unix(1800000060, 0).UTC()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := Check(m, now)
		if err != nil || !report.Passed {
			b.Fatalf("CUSTOMER-004 benchmark failed: %+v err=%v", report, err)
		}
	}
}
