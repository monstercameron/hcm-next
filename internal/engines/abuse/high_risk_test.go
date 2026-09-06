package abuse

import (
	"errors"
	"testing"
	"time"
)

func riskBase(id string, input SignalKind) DetectorVersion {
	return DetectorVersion{
		DetectorID:      id,
		Semver:          "1.0.0",
		DeclaredInputs:  []SignalKind{input},
		DeclaredOutputs: []string{"REVIEW_REQUIRED"},
		Thresholds:      []ThresholdRef{{ID: id + "-window"}},
		ActivatedAt:     time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
}

func riskEvent(id, principal, tenant string, kind SignalKind, at time.Time) RiskEvent {
	return RiskEvent{
		ID: id, Kind: kind, Principal: principal, Tenant: tenant,
		Subject:    Ref{Type: "worker", ID: "worker-" + id},
		ObservedAt: at,
	}
}

func riskRules() DetectionRules {
	return DetectionRules{
		BulkExport: BulkExportPolicy{
			Window: time.Hour, MaxVolumePerPrincipal: 100, MaxVolumePerTenant: 300,
			MaxVelocityPerPrincipal: 0.02, MaxVelocityPerTenant: 0.1,
		},
		PayrollChange: PayrollChangePolicy{
			Window: time.Hour, MaxChangesPerPrincipal: 2, MaxDistinctWorkersPerPrincipal: 2,
		},
		AccessChange: AccessChangePolicy{
			Window: time.Hour, MaxChangesPerPrincipal: 2, MaxChangesPerTenant: 3,
		},
	}
}

func mustRiskVersion(t *testing.T, base DetectorVersion, family DetectorFamily) RiskDetectorVersion {
	t.Helper()
	v, err := NewRiskDetectorVersion(base, family, riskRules())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTodo_ABUSE_003 covers the three typed detector families: a mass export,
// sensitive payroll changes, and access escalation/self-grant activity produce
// evidence-backed findings, while an explicitly approved batch is ignored.
func TestTodo_ABUSE_003(t *testing.T) {
	baseTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	bulkVersion := mustRiskVersion(t, riskBase("bulk-export-v1", SignalKindBulkExport), DetectorFamilyBulkExport)
	bulk, err := NewRiskDetector(bulkVersion)
	if err != nil {
		t.Fatal(err)
	}
	bulkEvents := []RiskEvent{
		riskEvent("export-1", "operator-1", "tenant-a", SignalKindBulkExport, baseTime),
		riskEvent("export-2", "operator-1", "tenant-a", SignalKindBulkExport, baseTime.Add(10*time.Minute)),
	}
	bulkEvents[0].Volume = 60
	bulkEvents[1].Volume = 60
	approved := riskEvent("approved-export", "operator-1", "tenant-a", SignalKindBulkExport, baseTime.Add(20*time.Minute))
	approved.Volume = 1000
	approved.ApprovedContext = true
	bulkEvents = append(bulkEvents, approved)
	gotBulk, err := bulk.Detect(bulkEvents)
	if err != nil {
		t.Fatal(err)
	}
	if gotBulk.Accepted() || len(gotBulk.Findings) == 0 {
		t.Fatalf("mass export was not found: %+v", gotBulk)
	}
	if gotBulk.Findings[0].Category != FindingBulkExport || gotBulk.Findings[0].Severity != SeverityHigh || gotBulk.Findings[0].Evidence.Scope != FindingScopePrincipal {
		t.Fatalf("bulk finding lost typed evidence: %+v", gotBulk.Findings[0])
	}
	if gotBulk.Findings[0].Evidence.Volume != 120 {
		t.Fatalf("approved export leaked into aggregate: %+v", gotBulk.Findings[0].Evidence)
	}

	payrollVersion := mustRiskVersion(t, riskBase("payroll-change-v1", SignalKindPrivilegedChange), DetectorFamilyPayrollChange)
	payroll, err := NewRiskDetector(payrollVersion)
	if err != nil {
		t.Fatal(err)
	}
	bank := riskEvent("bank-1", "operator-2", "tenant-a", SignalKindPrivilegedChange, baseTime)
	bank.Change = ChangeBankDetail
	pay := riskEvent("pay-1", "operator-2", "tenant-a", SignalKindPrivilegedChange, baseTime.Add(5*time.Minute))
	pay.Change = ChangePayRate
	redirect := riskEvent("redirect-1", "operator-2", "tenant-a", SignalKindPrivilegedChange, baseTime.Add(10*time.Minute))
	redirect.Change = ChangeNetPayRedirection
	approvedMerit := riskEvent("merit-1", "operator-2", "tenant-a", SignalKindPrivilegedChange, baseTime.Add(15*time.Minute))
	approvedMerit.Change = ChangeMeritBatch
	approvedMerit.ApprovedContext = true
	gotPayroll, err := payroll.Detect([]RiskEvent{bank, pay, redirect, approvedMerit})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotPayroll.Findings) < 4 {
		t.Fatalf("payroll changes did not produce typed event and fake-worker findings: %+v", gotPayroll.Findings)
	}
	seenCategories := map[FindingCategory]bool{}
	for _, f := range gotPayroll.Findings {
		seenCategories[f.Category] = true
		if f.DetectorDigest != payrollVersion.Digest || len(f.Evidence.InputIDs) == 0 {
			t.Fatalf("payroll finding lacks immutable evidence: %+v", f)
		}
	}
	for _, category := range []FindingCategory{FindingBankDetailChange, FindingPayRateChange, FindingNetPayRedirect, FindingFakeWorker} {
		if !seenCategories[category] {
			t.Fatalf("payroll category %s missing from %+v", category, seenCategories)
		}
	}

	accessVersion := mustRiskVersion(t, riskBase("access-change-v1", SignalKindAccessGrant), DetectorFamilyAccessChange)
	access, err := NewRiskDetector(accessVersion)
	if err != nil {
		t.Fatal(err)
	}
	selfGrant := riskEvent("grant-1", "operator-3", "tenant-a", SignalKindAccessGrant, baseTime)
	selfGrant.Change = ChangeSelfGrant
	roleEscalation := riskEvent("grant-2", "operator-3", "tenant-a", SignalKindAccessGrant, baseTime.Add(5*time.Minute))
	roleEscalation.Change = ChangeRoleEscalation
	privilegeBurst := riskEvent("grant-3", "operator-3", "tenant-a", SignalKindAccessGrant, baseTime.Add(10*time.Minute))
	privilegeBurst.Change = ChangeRoleEscalation
	gotAccess, err := access.Detect([]RiskEvent{selfGrant, roleEscalation, privilegeBurst})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotAccess.Findings) < 3 {
		t.Fatalf("access escalation/self-grant was not fully detected: %+v", gotAccess.Findings)
	}
	if gotAccess.Findings[0].Severity != SeverityCritical {
		t.Fatalf("self-grant severity = %s, want %s", gotAccess.Findings[0].Severity, SeverityCritical)
	}
}

// TestTodo_ABUSE_003_Security proves the minimized-input and declared-input
// boundaries: raw content and ungoverned changes are rejected, an ordinary
// import batch is approved only when explicitly marked, and explanations name
// the detector version's declared input rather than exposing activity data.
func TestTodo_ABUSE_003_Security(t *testing.T) {
	v := mustRiskVersion(t, riskBase("payroll-change-v1", SignalKindPrivilegedChange), DetectorFamilyPayrollChange)
	e := riskEvent("bad-raw", "operator-1", "tenant-a", SignalKindPrivilegedChange, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	e.Change = ChangeBankDetail
	e.RawContent = "account number"
	if !errors.Is(e.Validate(), ErrRiskRawContent) {
		t.Fatalf("raw content was accepted: %v", e.Validate())
	}
	if exp := ExplainRisk(e, v); exp.Applicable || exp.Reason != RiskExplainEventInvalid || len(exp.DeclaredInputs) != 1 || exp.DeclaredInputs[0] != SignalKindPrivilegedChange {
		t.Fatalf("raw event explanation was not safely bounded: %+v", exp)
	}

	undeclared := riskEvent("wrong-kind", "operator-1", "tenant-a", SignalKindAccessGrant, e.ObservedAt)
	undeclared.Change = ChangeSelfGrant
	if exp := ExplainRisk(undeclared, v); exp.Applicable || exp.Reason != RiskExplainNotDeclaredInput {
		t.Fatalf("undeclared signal kind matched: %+v", exp)
	}

	approvedImport := riskEvent("import-1", "operator-1", "tenant-a", SignalKindPrivilegedChange, e.ObservedAt)
	approvedImport.Change = ChangeImportBatch
	approvedImport.ApprovedContext = true
	detector, err := NewRiskDetector(v)
	if err != nil {
		t.Fatal(err)
	}
	result, err := detector.Detect([]RiskEvent{approvedImport})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted() || len(result.Findings) != 0 {
		t.Fatalf("approved import was not classified as approved context: %+v", result)
	}

	badVersion := v
	badVersion.DeclaredInputs = []SignalKind{SignalKindAccessGrant}
	if _, err := NewRiskDetector(badVersion); !errors.Is(err, ErrRiskVersionFamily) {
		t.Fatalf("family/input mismatch was accepted: %v", err)
	}
}
