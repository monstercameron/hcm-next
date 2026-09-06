package abuse

import (
	"errors"
	"testing"
	"time"
)

func anomalyPolicyFixture(t *testing.T) ActivityAnomalyDetectorVersion {
	t.Helper()
	policy := ActivityAnomalyPolicy{
		DetectorID: "abuse-002", Semver: "1.0.0",
		DeclaredInputs: []SignalKind{SignalKindPrivilegedChange, SignalKindAccessGrant, SignalKindSensitiveRead},
		DeclaredHours:  [2]int{8, 18}, DeclaredLocations: []string{"office", "vpn"},
		KnownPrincipals: []string{"operator-1"}, PrivilegedWindow: time.Hour, MaxPrivilegedOperations: 2,
		SensitiveWindow: time.Hour, SensitiveVolumeBaseline: map[string]int64{"operator-1": 5},
		DefaultSensitiveVolumeBaseline: 5, SensitiveVolumeMultiplier: 2,
		DeclaredSubjectScopes: map[string][]string{"operator-1": {"worker-1", "worker-2"}},
	}
	version, err := NewActivityAnomalyDetectorVersion(policy)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func anomalyInput(id, principal, subject string, kind SignalKind, at time.Time) ActivityAnomalyInput {
	return ActivityAnomalyInput{ID: id, Principal: principal, Tenant: "tenant-a", Subject: subject, Kind: kind, ObservedAt: at, Hour: at.Hour(), Location: "office", Volume: 1}
}

// TestTodo_ABUSE_002 proves time, new-principal and privileged-burst
// detection together with rolling sensitive-volume and declared-subject-scope
// checks, while approved work remains free of findings.
func TestTodo_ABUSE_002(t *testing.T) {
	version := anomalyPolicyFixture(t)
	detector, err := NewActivityAnomalyDetector(version)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	outOfHours := anomalyInput("priv-hours", "operator-1", "worker-1", SignalKindPrivilegedChange, base)
	outOfHours.Hour = 2
	newPrincipal := anomalyInput("priv-new", "operator-new", "worker-1", SignalKindPrivilegedChange, base.Add(10*time.Minute))
	burst := anomalyInput("priv-burst", "operator-1", "worker-2", SignalKindAccessGrant, base.Add(20*time.Minute))
	burst2 := anomalyInput("priv-burst-2", "operator-1", "worker-2", SignalKindAccessGrant, base.Add(21*time.Minute))
	burst3 := anomalyInput("priv-burst-3", "operator-1", "worker-2", SignalKindAccessGrant, base.Add(22*time.Minute))
	sensitive1 := anomalyInput("read-1", "operator-1", "worker-1", SignalKindSensitiveRead, base.Add(30*time.Minute))
	sensitive1.Volume = 6
	sensitive2 := anomalyInput("read-2", "operator-1", "worker-1", SignalKindSensitiveRead, base.Add(31*time.Minute))
	sensitive2.Volume = 6
	outOfScope := anomalyInput("read-out-of-scope", "operator-1", "worker-out", SignalKindSensitiveRead, base.Add(32*time.Minute))
	approved := anomalyInput("approved-batch", "operator-1", "worker-out", SignalKindSensitiveRead, base.Add(33*time.Minute))
	approved.Volume = 1000
	approved.ApprovedContext = true

	result, err := detector.Detect([]ActivityAnomalyInput{outOfHours, newPrincipal, burst, burst2, burst3, sensitive1, sensitive2, outOfScope, approved})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted() || len(result.Findings) < 5 {
		t.Fatalf("anomaly result = %+v, want multiple typed findings", result)
	}
	seen := map[ActivityAnomalyCategory]bool{}
	for _, finding := range result.Findings {
		seen[finding.Category] = true
		if finding.Severity == "" || finding.State != "REVIEW_REQUIRED" || finding.DetectorDigest == "" || len(finding.Evidence.InputIDs) == 0 {
			t.Fatalf("finding lacks typed evidence: %+v", finding)
		}
	}
	for _, category := range []ActivityAnomalyCategory{AnomalyPrivilegedOutsideHours, AnomalyPrivilegedNewPrincipal, AnomalyPrivilegedBurst, AnomalySensitiveVolume, AnomalySensitiveOutOfScope} {
		if !seen[category] {
			t.Errorf("missing category %s in %+v", category, result.Findings)
		}
	}
	for _, finding := range result.Findings {
		if finding.ActivityID == approved.ID {
			t.Fatalf("approved work produced a finding: %+v", finding)
		}
	}
	if len(result.Explanations) != 9 {
		t.Fatalf("explanations = %d, want one per input", len(result.Explanations))
	}
}

func TestTodo_ABUSE_002_Security(t *testing.T) {
	version := anomalyPolicyFixture(t)
	bad := anomalyInput("raw", "operator-1", "worker-1", SignalKindSensitiveRead, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	bad.RawContent = "salary=secret"
	if _, err := NewActivityAnomalyDetector(version); err != nil {
		t.Fatal(err)
	}
	detector, _ := NewActivityAnomalyDetector(version)
	if _, err := detector.Detect([]ActivityAnomalyInput{bad}); !errors.Is(err, ErrAnomalyRaw) {
		t.Fatalf("raw content was accepted: %v", err)
	}
	undeclared := anomalyInput("bulk", "operator-1", "worker-1", SignalKindBulkExport, bad.ObservedAt)
	if _, err := detector.Detect([]ActivityAnomalyInput{undeclared}); !errors.Is(err, ErrAnomalyInput) {
		t.Fatalf("undeclared activity shape was accepted: %v", err)
	}
	if exp := detector.Explain(bad); exp.Reason != AnomalyExplainInputInvalid || exp.Applicable {
		t.Fatalf("raw-content explanation = %+v", exp)
	}
}

func TestTodo_ABUSE_002_Mutation(t *testing.T) {
	version := anomalyPolicyFixture(t)
	mutatedPolicy := cloneAnomalyPolicy(version.ActivityAnomalyPolicy)
	mutatedPolicy.SensitiveVolumeMultiplier = 3
	mutated, err := NewActivityAnomalyDetectorVersion(mutatedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if version.Digest == mutated.Digest {
		t.Fatal("changing the declared baseline multiplier did not change detector version digest")
	}
	base := anomalyInput("read", "operator-1", "worker-1", SignalKindSensitiveRead, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	base.Volume = 11
	originalDetector, _ := NewActivityAnomalyDetector(version)
	mutatedDetector, _ := NewActivityAnomalyDetector(mutated)
	original, err := originalDetector.Detect([]ActivityAnomalyInput{base})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := mutatedDetector.Detect([]ActivityAnomalyInput{base})
	if err != nil {
		t.Fatal(err)
	}
	if original.Accepted() == changed.Accepted() {
		t.Fatalf("policy mutation did not change result: original=%+v changed=%+v", original, changed)
	}
}
