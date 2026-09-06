package abuse

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestActivityAnomalyPolicyValidationCopiesAndInputBoundaries(t *testing.T) {
	valid := anomalyPolicyFixture(t).ActivityAnomalyPolicy
	for _, tc := range []struct {
		name   string
		mutate func(*ActivityAnomalyPolicy)
	}{
		{"identity", func(p *ActivityAnomalyPolicy) { p.DetectorID = "" }}, {"semver", func(p *ActivityAnomalyPolicy) { p.Semver = "v1" }},
		{"inputs", func(p *ActivityAnomalyPolicy) { p.DeclaredInputs = nil }}, {"input kind", func(p *ActivityAnomalyPolicy) { p.DeclaredInputs = []SignalKind{"bad"} }},
		{"hours low", func(p *ActivityAnomalyPolicy) { p.DeclaredHours[0] = -1 }}, {"hours high", func(p *ActivityAnomalyPolicy) { p.DeclaredHours[1] = 25 }}, {"hours order", func(p *ActivityAnomalyPolicy) { p.DeclaredHours = [2]int{18, 8} }},
		{"locations", func(p *ActivityAnomalyPolicy) { p.DeclaredLocations = nil }}, {"blank location", func(p *ActivityAnomalyPolicy) { p.DeclaredLocations = []string{" "} }},
		{"privileged window", func(p *ActivityAnomalyPolicy) { p.PrivilegedWindow = 0 }}, {"privileged limit", func(p *ActivityAnomalyPolicy) { p.MaxPrivilegedOperations = 0 }},
		{"sensitive window", func(p *ActivityAnomalyPolicy) { p.SensitiveWindow = 0 }}, {"multiplier", func(p *ActivityAnomalyPolicy) { p.SensitiveVolumeMultiplier = 0 }}, {"default baseline", func(p *ActivityAnomalyPolicy) { p.DefaultSensitiveVolumeBaseline = 0 }},
		{"baseline principal", func(p *ActivityAnomalyPolicy) { p.SensitiveVolumeBaseline[" "] = 1 }}, {"baseline value", func(p *ActivityAnomalyPolicy) { p.SensitiveVolumeBaseline["bad"] = 0 }},
		{"scope principal", func(p *ActivityAnomalyPolicy) { p.DeclaredSubjectScopes[" "] = []string{"subject"} }}, {"scope empty", func(p *ActivityAnomalyPolicy) { p.DeclaredSubjectScopes["p"] = nil }}, {"scope subject", func(p *ActivityAnomalyPolicy) { p.DeclaredSubjectScopes["p"] = []string{" "} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := cloneAnomalyPolicy(valid)
			tc.mutate(&p)
			if !errors.Is(p.Validate(), ErrAnomalyVersion) {
				t.Fatalf("error=%v", p.Validate())
			}
		})
	}
	policy := cloneAnomalyPolicy(valid)
	version, err := NewActivityAnomalyDetectorVersion(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.DeclaredInputs[0] = SignalKindAuthAnomaly
	policy.DeclaredLocations[0] = "mutated"
	policy.SensitiveVolumeBaseline["operator-1"] = 999
	policy.DeclaredSubjectScopes["operator-1"][0] = "mutated"
	if version.DeclaredInputs[0] == SignalKindAuthAnomaly || version.DeclaredLocations[0] == "mutated" || version.SensitiveVolumeBaseline["operator-1"] == 999 || version.DeclaredSubjectScopes["operator-1"][0] == "mutated" {
		t.Fatal("anomaly version retained caller-owned state")
	}
	if len(cloneInt64Map(nil)) != 0 || len(cloneStringMap(nil)) != 0 {
		t.Fatal("nil map clones were not empty")
	}

	base := anomalyInput("input", "operator-1", "worker-1", SignalKindSensitiveRead, time.Now().UTC())
	for _, tc := range []struct {
		name   string
		mutate func(*ActivityAnomalyInput)
		want   error
	}{
		{"raw", func(a *ActivityAnomalyInput) { a.RawContent = "raw" }, ErrAnomalyRaw}, {"id", func(a *ActivityAnomalyInput) { a.ID = "" }, ErrAnomalyInput},
		{"principal", func(a *ActivityAnomalyInput) { a.Principal = "" }, ErrAnomalyInput}, {"tenant", func(a *ActivityAnomalyInput) { a.Tenant = "" }, ErrAnomalyInput}, {"subject", func(a *ActivityAnomalyInput) { a.Subject = "" }, ErrAnomalyInput},
		{"kind", func(a *ActivityAnomalyInput) { a.Kind = SignalKind("bad") }, ErrAnomalyInput}, {"time", func(a *ActivityAnomalyInput) { a.ObservedAt = time.Time{} }, ErrAnomalyInput}, {"hour low", func(a *ActivityAnomalyInput) { a.Hour = -1 }, ErrAnomalyInput}, {"hour high", func(a *ActivityAnomalyInput) { a.Hour = 24 }, ErrAnomalyInput}, {"volume", func(a *ActivityAnomalyInput) { a.Volume = -1 }, ErrAnomalyInput}, {"sensitive zero", func(a *ActivityAnomalyInput) { a.Volume = 0 }, ErrAnomalyInput}, {"unsupported kind", func(a *ActivityAnomalyInput) { a.Kind = SignalKindAuthAnomaly }, ErrAnomalyInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			tc.mutate(&a)
			if !errors.Is(a.Validate(), tc.want) {
				t.Fatalf("error=%v, want %v", a.Validate(), tc.want)
			}
		})
	}
}

func TestActivityAnomalyExplainDetectorCopiesAndOverflow(t *testing.T) {
	version := anomalyPolicyFixture(t)
	detector, err := NewActivityAnomalyDetector(version)
	if err != nil {
		t.Fatal(err)
	}
	base := anomalyInput("explain", "operator-1", "worker-1", SignalKindSensitiveRead, time.Now().UTC())
	declared := detector.Explain(base)
	if !declared.Applicable || declared.Reason != AnomalyExplainDeclaredKind || declared.DetectorDigest == "" {
		t.Fatalf("declared explanation=%+v", declared)
	}
	undeclared := base
	undeclared.Kind = SignalKindSensitiveRead
	undeclaredPolicy := cloneAnomalyPolicy(version.ActivityAnomalyPolicy)
	undeclaredPolicy.DeclaredInputs = []SignalKind{SignalKindPrivilegedChange, SignalKindAccessGrant}
	undeclaredVersion, err := NewActivityAnomalyDetectorVersion(undeclaredPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if exp := ExplainActivityAnomaly(undeclared, undeclaredVersion); exp.Applicable || exp.Reason != AnomalyExplainUndeclaredKind {
		t.Fatalf("undeclared explanation=%+v", exp)
	}
	invalid := base
	invalid.RawContent = "raw"
	if exp := ExplainActivityAnomaly(invalid, version); exp.Applicable || exp.Reason != AnomalyExplainInputInvalid {
		t.Fatalf("invalid explanation=%+v", exp)
	}
	badVersion := version
	badVersion.Semver = "v1"
	if exp := ExplainActivityAnomaly(base, badVersion); exp.Applicable || exp.Reason != AnomalyExplainVersionInvalid {
		t.Fatalf("bad version explanation=%+v", exp)
	}
	copyVersion := detector.Version()
	copyVersion.DeclaredInputs[0] = SignalKindAuthAnomaly
	copyVersion.DeclaredLocations[0] = "mutated"
	copyVersion.SensitiveVolumeBaseline["operator-1"] = 99
	if detector.Version().DeclaredInputs[0] == SignalKindAuthAnomaly || detector.Version().DeclaredLocations[0] == "mutated" || detector.Version().SensitiveVolumeBaseline["operator-1"] == 99 {
		t.Fatal("Version returned aliased policy state")
	}
	if !(ActivityAnomalyDetection{}).Accepted() || (ActivityAnomalyDetection{Findings: []ActivityAnomalyFinding{{ActivityID: "x"}}}).Accepted() {
		t.Fatal("Accepted did not reflect findings")
	}

	policy := version.ActivityAnomalyPolicy
	policy.DefaultSensitiveVolumeBaseline = math.MaxInt64
	policy.SensitiveVolumeMultiplier = 2
	policy.SensitiveVolumeBaseline = nil
	policy.DeclaredSubjectScopes = map[string][]string{"operator-1": {"worker-1"}}
	overflowVersion, err := NewActivityAnomalyDetectorVersion(policy)
	if err != nil {
		t.Fatal(err)
	}
	overflowDetector, err := NewActivityAnomalyDetector(overflowVersion)
	if err != nil {
		t.Fatal(err)
	}
	input := anomalyInput("overflow", "operator-1", "worker-1", SignalKindSensitiveRead, time.Now().UTC())
	input.Volume = 1
	result, err := overflowDetector.Detect([]ActivityAnomalyInput{input})
	if err != nil || !result.Accepted() {
		t.Fatalf("overflow-safe result=%+v err=%v", result, err)
	}
	invalidDetector := ActivityAnomalyDetector{}
	if _, err := invalidDetector.Detect(nil); !errors.Is(err, ErrAnomalyVersion) {
		t.Fatalf("invalid detector error=%v", err)
	}
	if !containsString([]string{"a", "b"}, "b") || containsString([]string{"a"}, "b") {
		t.Fatal("containsString branch failed")
	}
}

func TestActivityAnomalyDetectionOrderingAndWindowBoundaries(t *testing.T) {
	version := anomalyPolicyFixture(t)
	detector, err := NewActivityAnomalyDetector(version)
	if err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	atStart := anomalyInput("start", "operator-1", "worker-1", SignalKindPrivilegedChange, end.Add(-time.Hour))
	atStart.Hour = 12
	atEnd := anomalyInput("end", "operator-1", "worker-1", SignalKindPrivilegedChange, end)
	tooOld := anomalyInput("old", "operator-1", "worker-1", SignalKindPrivilegedChange, end.Add(-time.Hour-time.Nanosecond))
	result, err := detector.Detect([]ActivityAnomalyInput{atEnd, tooOld, atStart})
	if err != nil || len(result.Explanations) != 3 {
		t.Fatalf("ordered detection=%+v err=%v", result, err)
	}
	if result.Explanations[0].ActivityID != "old" || result.Explanations[1].ActivityID != "start" || result.Explanations[2].ActivityID != "end" {
		t.Fatalf("explanations not time ordered: %+v", result.Explanations)
	}
	window := windowAnomalyInputs([]ActivityAnomalyInput{atStart, atEnd, tooOld}, atEnd, time.Hour, func(a ActivityAnomalyInput) bool { return true })
	if len(window) != 2 {
		t.Fatalf("anomaly window=%v, want inclusive endpoints only", window)
	}
	agg := aggregateEvidence(window, atEnd, time.Hour)
	if agg.EventCount != 2 || len(agg.InputIDs) != 2 || !agg.WindowStart.Equal(end.Add(-time.Hour)) || !agg.WindowEnd.Equal(end) {
		t.Fatalf("aggregate evidence=%+v", agg)
	}
	single := singleEvidence(atEnd)
	if single.EventCount != 1 || single.InputIDs[0] != "end" || single.Volume != atEnd.Volume {
		t.Fatalf("single evidence=%+v", single)
	}
	finding := detector.finding(atEnd, AnomalySensitiveOutOfScope, SeverityHigh, "field", single)
	if finding.DetectorID != version.DetectorID || finding.ActivityID != atEnd.ID || finding.State != "REVIEW_REQUIRED" {
		t.Fatalf("finding=%+v", finding)
	}
}
