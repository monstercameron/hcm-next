package abuse

import (
	"errors"
	"testing"
	"time"
)

func TestHighRiskClosedVocabulariesAndEventValidation(t *testing.T) {
	for _, family := range []DetectorFamily{DetectorFamilyBulkExport, DetectorFamilyPayrollChange, DetectorFamilyAccessChange} {
		if !family.Valid() {
			t.Errorf("family %q reported invalid", family)
		}
	}
	if DetectorFamily("unknown").Valid() {
		t.Fatal("unknown detector family reported valid")
	}
	for _, kind := range []ChangeKind{ChangeBankDetail, ChangePayRate, ChangeNetPayRedirection, ChangeSelfGrant, ChangeRoleEscalation, ChangeMeritBatch, ChangePayrollBatch, ChangeImportBatch} {
		if !kind.Valid() {
			t.Errorf("change kind %q reported invalid", kind)
		}
	}
	if ChangeUnspecified.Valid() || ChangeKind("unknown").Valid() {
		t.Fatal("invalid change kind reported valid")
	}
	for _, kind := range []ChangeKind{ChangeBankDetail, ChangePayRate, ChangeNetPayRedirection, ChangeSelfGrant, ChangeRoleEscalation} {
		if !kind.highRisk() {
			t.Errorf("high-risk kind %q not classified high-risk", kind)
		}
	}
	for _, kind := range []ChangeKind{ChangeUnspecified, ChangeMeritBatch, ChangePayrollBatch, ChangeImportBatch} {
		if kind.highRisk() {
			t.Errorf("ordinary kind %q classified high-risk", kind)
		}
	}
	for _, severity := range []FindingSeverity{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical} {
		if !severity.Valid() {
			t.Errorf("severity %q reported invalid", severity)
		}
	}
	if FindingSeverity("unknown").Valid() {
		t.Fatal("unknown severity reported valid")
	}

	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	base := hardeningRiskEvent("e", "p", "t", SignalKindBulkExport, now)
	base.Volume = 1
	for _, tc := range []struct {
		name   string
		mutate func(*RiskEvent)
		want   error
	}{
		{"raw", func(e *RiskEvent) { e.RawContent = "secret" }, ErrRiskRawContent},
		{"identity", func(e *RiskEvent) { e.ID = "" }, ErrRiskEvent},
		{"kind", func(e *RiskEvent) { e.Kind = SignalKind("bad") }, ErrRiskEvent},
		{"tenant", func(e *RiskEvent) { e.Tenant = "" }, ErrRiskEvent},
		{"observed at", func(e *RiskEvent) { e.ObservedAt = time.Time{} }, ErrRiskEvent},
		{"principal and actor", func(e *RiskEvent) { e.Principal = ""; e.Actor = Ref{} }, ErrRiskEvent},
		{"negative volume", func(e *RiskEvent) { e.Volume = -1 }, ErrRiskEvent},
		{"bad change", func(e *RiskEvent) { e.Change = ChangeKind("bad") }, ErrRiskEvent},
		{"bulk zero volume", func(e *RiskEvent) { e.Volume = 0 }, ErrRiskEvent},
		{"bulk change", func(e *RiskEvent) { e.Change = ChangeImportBatch }, ErrRiskEvent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.mutate(&e)
			if !errors.Is(e.Validate(), tc.want) {
				t.Fatalf("error=%v, want %v", e.Validate(), tc.want)
			}
		})
	}
	privileged := hardeningRiskEvent("p1", "p", "t", SignalKindPrivilegedChange, now)
	for _, tc := range []struct {
		name   string
		mutate func(*RiskEvent)
	}{
		{"privileged unspecified", func(e *RiskEvent) { e.Change = ChangeUnspecified }},
		{"privileged volume", func(e *RiskEvent) { e.Change = ChangeBankDetail; e.Volume = 1 }},
		{"access unspecified", func(e *RiskEvent) { e.Kind = SignalKindAccessGrant; e.Change = ChangeUnspecified }},
		{"access volume", func(e *RiskEvent) { e.Kind = SignalKindAccessGrant; e.Change = ChangeSelfGrant; e.Volume = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := privileged
			tc.mutate(&e)
			if !errors.Is(e.Validate(), ErrRiskEvent) {
				t.Fatalf("error=%v", e.Validate())
			}
		})
	}
	actorOnly := base
	actorOnly.Principal = ""
	actorOnly.Actor = Ref{Type: "user", ID: "actor-1"}
	if err := actorOnly.Validate(); err != nil || actorOnly.principalID() != "actor-1" {
		t.Fatalf("actor fallback invalid: err=%v principal=%q", err, actorOnly.principalID())
	}
	if actorOnly.subjectID() != actorOnly.Subject.ID {
		t.Fatal("subjectID did not use opaque subject reference")
	}
}

func TestHighRiskPolicyConstructorsCopiesAndExplanations(t *testing.T) {
	baseBulk := hardeningVersion(SignalKindBulkExport)
	basePayroll := hardeningVersion(SignalKindPrivilegedChange)
	baseAccess := hardeningVersion(SignalKindAccessGrant)
	rules := riskRules()
	for _, tc := range []struct {
		name string
		make func() (RiskDetectorVersion, error)
	}{
		{"bulk", func() (RiskDetectorVersion, error) { return NewBulkExportDetectorVersion(baseBulk, rules.BulkExport) }},
		{"payroll", func() (RiskDetectorVersion, error) {
			return NewPayrollChangeDetectorVersion(basePayroll, rules.PayrollChange)
		}},
		{"access", func() (RiskDetectorVersion, error) {
			return NewAccessChangeDetectorVersion(baseAccess, rules.AccessChange)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.make()
			if err != nil || v.VersionDigest() == "" {
				t.Fatalf("version=%+v err=%v", v, err)
			}
			if _, err := NewRiskDetector(v); err != nil {
				t.Fatal(err)
			}
			detector, err := NewRiskDetector(v)
			if err != nil {
				t.Fatal(err)
			}
			version := detector.Version()
			version.DeclaredInputs[0] = SignalKindAuthAnomaly
			if detector.Version().DeclaredInputs[0] == SignalKindAuthAnomaly {
				t.Fatal("Version returned an aliased input slice")
			}
			if _, err := NewDetector(v); err != nil {
				t.Fatal(err)
			}
		})
	}
	original := hardeningVersion(SignalKindBulkExport)
	copyVersion, err := NewBulkExportDetectorVersion(original, rules.BulkExport)
	if err != nil {
		t.Fatal(err)
	}
	original.DeclaredInputs[0] = SignalKindAuthAnomaly
	if copyVersion.DeclaredInputs[0] == SignalKindAuthAnomaly {
		t.Fatal("constructor retained caller-owned input slice")
	}

	bad := RiskDetectorVersion{DetectorVersion: baseBulk, Family: DetectorFamily("bad"), Rules: rules}
	if err := bad.Validate(); !errors.Is(err, ErrRiskVersion) {
		t.Fatalf("bad family error=%v", err)
	}
	bad = RiskDetectorVersion{DetectorVersion: baseBulk, Family: DetectorFamilyBulkExport, Rules: DetectionRules{BulkExport: BulkExportPolicy{}}}
	if err := bad.Validate(); !errors.Is(err, ErrRiskPolicy) {
		t.Fatalf("bad bulk policy error=%v", err)
	}
	bad = RiskDetectorVersion{DetectorVersion: basePayroll, Family: DetectorFamilyPayrollChange, Rules: DetectionRules{PayrollChange: PayrollChangePolicy{}}}
	if err := bad.Validate(); !errors.Is(err, ErrRiskPolicy) {
		t.Fatalf("bad payroll policy error=%v", err)
	}
	bad = RiskDetectorVersion{DetectorVersion: baseAccess, Family: DetectorFamilyAccessChange, Rules: DetectionRules{AccessChange: AccessChangePolicy{}}}
	if err := bad.Validate(); !errors.Is(err, ErrRiskPolicy) {
		t.Fatalf("bad access policy error=%v", err)
	}

	v := mustRiskVersion(t, riskBase("explain", SignalKindPrivilegedChange), DetectorFamilyPayrollChange)
	valid := riskEvent("valid", "p", "t", SignalKindPrivilegedChange, time.Now().UTC())
	valid.Change = ChangeBankDetail
	exp := ExplainRisk(valid, v)
	if !exp.Applicable || exp.Reason != RiskExplainDeclaredInput || len(exp.DeclaredInputs) != 1 {
		t.Fatalf("valid explanation=%+v", exp)
	}
	if exp := v.Explain(valid); !exp.Applicable {
		t.Fatalf("method explanation=%+v", exp)
	}
	invalid := valid
	invalid.RawContent = "secret"
	if exp := ExplainRisk(invalid, v); exp.Applicable || exp.Reason != RiskExplainEventInvalid {
		t.Fatalf("invalid explanation=%+v", exp)
	}
	badVersion := v
	badVersion.ActivatedAt = time.Time{}
	if exp := ExplainRisk(valid, badVersion); exp.Applicable || exp.Reason != RiskExplainVersionInvalid {
		t.Fatalf("bad version explanation=%+v", exp)
	}
	undeclared := valid
	undeclared.Kind = SignalKindAccessGrant
	undeclared.Change = ChangeSelfGrant
	if exp := ExplainRisk(undeclared, v); exp.Applicable || exp.Reason != RiskExplainNotDeclaredInput {
		t.Fatalf("undeclared explanation=%+v", exp)
	}
}

func TestHighRiskWindowAggregationAndStrictLimits(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	a := hardeningRiskEvent("a", "p", "tenant-a", SignalKindBulkExport, now.Add(-time.Hour))
	a.Volume = 10
	b := hardeningRiskEvent("b", "p", "tenant-a", SignalKindBulkExport, now)
	b.Volume = 10
	c := hardeningRiskEvent("c", "other", "tenant-b", SignalKindBulkExport, now)
	c.Volume = 10
	all := []RiskEvent{a, b, c}
	window := windowEvents(all, now, time.Hour, func(e RiskEvent) bool { return e.Kind == SignalKindBulkExport })
	if len(window) != 3 {
		t.Fatalf("window events=%v, want endpoints included", window)
	}
	agg := aggregateFor(all, now, time.Hour, FindingScopePrincipal, "p", func(e RiskEvent) bool { return true })
	if agg.count != 2 || agg.volume != 20 || len(agg.workers) != 2 {
		t.Fatalf("principal aggregate=%+v", agg)
	}
	principalEvidence := evidence(agg, FindingScopePrincipal)
	if principalEvidence.Principal != "p" || principalEvidence.Tenant != "" || principalEvidence.EventCount != 2 {
		t.Fatalf("principal evidence=%+v", principalEvidence)
	}
	tenantEvidence := evidence(aggregateFor(all, now, time.Hour, FindingScopeTenant, "tenant-a", func(e RiskEvent) bool { return true }), FindingScopeTenant)
	if tenantEvidence.Tenant != "tenant-a" || tenantEvidence.Principal != "" {
		t.Fatalf("tenant evidence=%+v", tenantEvidence)
	}

	bulkVersion := mustRiskVersion(t, riskBase("bulk-strict", SignalKindBulkExport), DetectorFamilyBulkExport)
	bulkVersion.Rules.BulkExport.MaxVolumePerPrincipal = 20
	bulkVersion.Rules.BulkExport.MaxVolumePerTenant = 20
	bulkVersion.Rules.BulkExport.MaxVelocityPerPrincipal = 20 / time.Hour.Seconds()
	bulkVersion.Rules.BulkExport.MaxVelocityPerTenant = 20 / time.Hour.Seconds()
	// Rebuild after changing rules so the detector's release is internally consistent.
	bulkVersion, err := NewBulkExportDetectorVersion(bulkVersion.DetectorVersion, bulkVersion.Rules.BulkExport)
	if err != nil {
		t.Fatal(err)
	}
	detector, err := NewRiskDetector(bulkVersion)
	if err != nil {
		t.Fatal(err)
	}
	got, err := detector.Detect([]RiskEvent{a, b})
	if err != nil || !got.Accepted() {
		t.Fatalf("equal-to-limit bulk result=%+v err=%v", got, err)
	}
	b.Volume = 11
	got, err = Detect(detector, []RiskEvent{a, b})
	if err != nil || got.Accepted() {
		t.Fatalf("strictly over-limit bulk result=%+v err=%v", got, err)
	}
}
