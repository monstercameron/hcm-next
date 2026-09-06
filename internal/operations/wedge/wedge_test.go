package wedge

import (
	"errors"
	"testing"
	"time"
)

var wedgeAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func wedgeWindow() EvidenceWindow {
	return EvidenceWindow{From: wedgeAt, To: wedgeAt.Add(24 * time.Hour), Source: "PLACEHOLDER_SOURCE_SYSTEM_EXPORT", Exclusions: []string{"PLACEHOLDER_EXCLUSION"}}
}

func topologyFixture() HandoffTopology {
	return HandoffTopology{
		SchemaVersion: WedgeSchemaVersion,
		ID:            "promotion-handoff-v1",
		Source:        SystemIdentity{ConnectorID: "PLACEHOLDER_HCM_NEXT_CONNECTOR", SystemID: "PLACEHOLDER_HCM_NEXT_SYSTEM", AuthorityBoundary: "PLACEHOLDER_HCM_NEXT_AUTHORITY", OwnerID: "PLACEHOLDER_HCM_NEXT_OWNER"},
		Destination:   SystemIdentity{ConnectorID: "PLACEHOLDER_DOWNSTREAM_CONNECTOR", SystemID: "PLACEHOLDER_DOWNSTREAM_SYSTEM", AuthorityBoundary: "PLACEHOLDER_PARTNER_AUTHORITY", OwnerID: "PLACEHOLDER_PARTNER_OWNER"},
		Claim:         HandoffClaim{Purpose: "PLACEHOLDER downstream review observation", NarrowedCrossSystem: true, Fields: []string{"promotion_id", "decision_digest"}, NoMutation: true},
		Observation:   ObservationContract{Mechanism: "ACK_CALLBACK", AckID: "PLACEHOLDER_ACK_ID", Evidence: "PLACEHOLDER_ACK_DIGEST"},
		Owner:         Accountability{OwnerID: "PLACEHOLDER_PARTNER_OWNER", Role: "PLACEHOLDER_ACCOUNTABLE_ROLE"},
	}
}

func adoptionFixture() AdoptionMetric {
	return AdoptionMetric{SchemaVersion: WedgeSchemaVersion, MetricID: "promotion-adoption-v1", Window: wedgeWindow(), Taxonomy: BypassTaxonomy{Version: "v1", Codes: []BypassReason{{Code: "MANUAL", Label: "Manual partner handling"}, {Code: "INCUMBENT", Label: "Incumbent workflow"}, {Code: "SPREADSHEET", Label: "Spreadsheet workflow"}, {Code: "EMERGENCY", Label: "Emergency path"}}}, Records: []AdoptionRecord{
		{TransactionID: "txn-1", Eligible: true, Path: AdoptionHCMNext},
		{TransactionID: "txn-2", Eligible: true, Path: AdoptionApprovedBypass, BypassCode: "MANUAL"},
		{TransactionID: "txn-3", Eligible: true, Path: AdoptionUnexplained},
		{TransactionID: "txn-ignored", Eligible: false},
	}}
}

func costFixture() CostReport {
	return CostReport{SchemaVersion: WedgeSchemaVersion, ReportID: "promotion-cost-v1", Evidence: []CostEvidence{
		{ID: "cost-customer", Category: CostCustomerLabor, Role: "PLACEHOLDER_IMPLEMENTATION_ROLE", Activity: "configuration", Minutes: 120, RateCentsPerHour: 6000, RateSource: "PLACEHOLDER_CUSTOMER_RATE_SOURCE", Window: wedgeWindow(), AllocationMethod: "DIRECT_ACTIVITY"},
		{ID: "cost-support", Category: CostHCMNext, Role: "PLACEHOLDER_SUPPORT_ROLE", Activity: "pilot_support", Minutes: 60, RateCentsPerHour: 12000, RateSource: "PLACEHOLDER_HCM_NEXT_COST_SOURCE", Window: wedgeWindow(), AllocationMethod: "DIRECT_ACTIVITY"},
	}}
}

func thresholdFixture() PilotThresholds {
	return PilotThresholds{SchemaVersion: WedgeSchemaVersion, Version: "pilot-thresholds-v1", MinSample: 10, Proceed: ProceedThreshold{MinAdoptionBasisPoints: 8000, MaxCustomerLaborCents: 20000, MaxHCMNextCostCents: 30000}, Reselect: ReselectThreshold{MinAdoptionBasisPoints: 5000, MaxExceptionBasisPoints: 2000}, Stop: StopThreshold{MaxExceptionBasisPoints: 5000, MaxHCMNextCostCents: 100000}, ApprovedBy: "PLACEHOLDER_HUMAN_APPROVER", Signature: "PLACEHOLDER_SIGNATURE_RECEIPT"}
}

func exitFixture() PilotExitPlan {
	return PilotExitPlan{SchemaVersion: WedgeSchemaVersion, PlanID: "exit-plan-v1", TenantID: "tenant-placeholder", Revocations: []Revocation{{ConnectorID: "connector-placeholder", CredentialID: "credential-placeholder", Revoked: true, RevokedAt: wedgeAt.Add(time.Hour)}}, Pending: []PendingWork{{WorkID: "work-placeholder", Disposition: PendingExported}}, Export: EvidenceExport{TenantID: "tenant-placeholder", Complete: true, Items: []ExportItem{{Kind: "adoption-evidence", Digest: "sha256:placeholder"}}}, RetentionDays: 30, Holds: []RetentionHold{{HoldID: "hold-placeholder", Reason: "PLACEHOLDER_LEGAL_OR_RECORDS_HOLD", Until: wedgeAt.Add(30 * 24 * time.Hour)}}, Destruction: DestructionResponsibility{OwnerID: "PLACEHOLDER_TENANT_OWNER", Method: "PLACEHOLDER_VERIFIED_DESTRUCTION"}}
}

func TestTopologyRejectsSingleSystemCrossSystemClaim(t *testing.T) {
	topology := topologyFixture()
	if err := topology.Validate(); err != nil {
		t.Fatal(err)
	}
	topology.Destination.AuthorityBoundary = topology.Source.AuthorityBoundary
	topology.Claim.NarrowedCrossSystem = false
	if err := topology.Validate(); !errors.Is(err, ErrInvalidWedgeRecord) {
		t.Fatalf("single-system claim error = %v, want invalid topology", err)
	}
	topology.Claim.NarrowedCrossSystem = true
	if err := topology.Validate(); err != nil {
		t.Fatalf("explicit narrowed claim rejected: %v", err)
	}
}

func TestAdoptionMetricRejectsHiddenBypass(t *testing.T) {
	metric := adoptionFixture()
	metric.Records[1].Path = ""
	if _, err := metric.Evaluate(); !errors.Is(err, ErrInvalidWedgeRecord) {
		t.Fatalf("hidden bypass error = %v, want invalid adoption metric", err)
	}
	metric = adoptionFixture()
	metric.Records[1].BypassCode = "NOT_IN_TAXONOMY"
	if _, err := metric.Evaluate(); !errors.Is(err, ErrInvalidWedgeRecord) {
		t.Fatalf("unregistered bypass error = %v, want invalid adoption metric", err)
	}
}

func TestTodo_WEDGE_006_Property(t *testing.T) {
	metric := adoptionFixture()
	for i, code := range []string{"MANUAL", "INCUMBENT", "SPREADSHEET", "EMERGENCY"} {
		metric.Records = []AdoptionRecord{{TransactionID: "txn-property", Eligible: true, Path: AdoptionApprovedBypass, BypassCode: code}}
		result, err := metric.Evaluate()
		if err != nil || result.Eligible != 1 || result.ApprovedBypass != 1 || result.Unexplained != 0 {
			t.Fatalf("property %d result=%+v err=%v", i, result, err)
		}
	}
}

func TestTodo_WEDGE_006_Mutation(t *testing.T) {
	mutations := []func(*AdoptionRecord){func(r *AdoptionRecord) { r.Path = "BYPASS" }, func(r *AdoptionRecord) { r.BypassCode = "" }, func(r *AdoptionRecord) { r.TransactionID = "txn-1" }}
	for i, mutate := range mutations {
		candidate := adoptionFixture()
		mutate(&candidate.Records[1])
		if _, err := candidate.Evaluate(); !errors.Is(err, ErrInvalidWedgeRecord) {
			t.Fatalf("mutation %d escaped validation: %v", i, err)
		}
	}
}

func TestCostEvidenceRejectsMissingRoleOrWindow(t *testing.T) {
	report := costFixture()
	report.Evidence[0].Role = ""
	if _, err := report.Compile(); !errors.Is(err, ErrInvalidWedgeRecord) {
		t.Fatalf("missing role error = %v, want invalid cost evidence", err)
	}
	report = costFixture()
	report.Evidence[0].Window = EvidenceWindow{}
	if _, err := report.Compile(); !errors.Is(err, ErrInvalidWedgeRecord) {
		t.Fatalf("missing window error = %v, want invalid cost evidence", err)
	}
}

func TestTodo_WEDGE_007_Golden(t *testing.T) {
	first, err := costFixture().Compile()
	if err != nil {
		t.Fatal(err)
	}
	second, err := costFixture().Compile()
	if err != nil || first != second {
		t.Fatalf("cost evidence is not deterministic: first=%+v second=%+v err=%v", first, second, err)
	}
	if first.CustomerLaborCents != 12000 || first.HCMNextCostCents != 12000 || first.ReportDigest == "" {
		t.Fatalf("unexpected cost summary: %+v", first)
	}
}

func TestPilotDecisionRejectsQualitativeOnlyThresholds(t *testing.T) {
	thresholds := thresholdFixture()
	if _, err := thresholds.Evaluate(PilotMeasurement{SampleSize: 10, AdoptionBasisPoints: 9000}); err != nil {
		t.Fatal(err)
	}
	thresholds.MinSample = 0
	if _, err := thresholds.Evaluate(PilotMeasurement{}); !errors.Is(err, ErrUnsignedThresholds) {
		t.Fatalf("qualitative-only threshold error = %v, want unsigned/invalid thresholds", err)
	}
	thresholds = thresholdFixture()
	thresholds.Proceed.MinAdoptionBasisPoints = thresholds.Reselect.MinAdoptionBasisPoints
	if _, err := thresholds.Evaluate(PilotMeasurement{}); !errors.Is(err, ErrInvalidWedgeRecord) {
		t.Fatalf("unordered threshold error = %v, want invalid thresholds", err)
	}
}

func TestTodo_WEDGE_008_Property(t *testing.T) {
	thresholds := thresholdFixture()
	measurements := []struct {
		measurement PilotMeasurement
		want        PilotDecisionCode
	}{
		{PilotMeasurement{SampleSize: 10, AdoptionBasisPoints: 8000, ExceptionBasisPoints: 1000, CustomerLaborCents: 20000, HCMNextCostCents: 30000}, DecisionProceed},
		{PilotMeasurement{SampleSize: 10, AdoptionBasisPoints: 7000, ExceptionBasisPoints: 1000, CustomerLaborCents: 20000, HCMNextCostCents: 30000}, DecisionReselectWedge},
		{PilotMeasurement{SampleSize: 10, AdoptionBasisPoints: 9000, ExceptionBasisPoints: 5001, CustomerLaborCents: 0, HCMNextCostCents: 0}, DecisionStop},
	}
	for i, tc := range measurements {
		decision, err := thresholds.Evaluate(tc.measurement)
		if err != nil || decision.Code != tc.want {
			t.Fatalf("measurement %d decision=%+v err=%v, want %s", i, decision, err, tc.want)
		}
	}
}

func TestTodo_WEDGE_008_Mutation(t *testing.T) {
	thresholds := thresholdFixture()
	base := PilotMeasurement{SampleSize: 10, AdoptionBasisPoints: 8000, ExceptionBasisPoints: 1000, CustomerLaborCents: 20000, HCMNextCostCents: 30000}
	decision, err := thresholds.Evaluate(base)
	if err != nil || decision.Code != DecisionProceed || decision.ThresholdDigest == "" {
		t.Fatalf("baseline decision=%+v err=%v", decision, err)
	}
	for _, mutate := range []func(*PilotMeasurement){func(m *PilotMeasurement) { m.SampleSize = 9 }, func(m *PilotMeasurement) { m.ExceptionBasisPoints = 5001 }, func(m *PilotMeasurement) { m.HCMNextCostCents = 100001 }} {
		candidate := base
		mutate(&candidate)
		mutated, evalErr := thresholds.Evaluate(candidate)
		if evalErr != nil || mutated.Code == DecisionProceed {
			t.Fatalf("measurement mutation escaped decision: %+v err=%v", mutated, evalErr)
		}
	}
}

func TestPilotExitPlanRejectsMissingRevocationOrExport(t *testing.T) {
	plan := exitFixture()
	plan.Revocations[0].Revoked = false
	if _, err := DryRunExit(plan); !errors.Is(err, ErrIncompleteExit) {
		t.Fatalf("missing revocation error = %v", err)
	}
	plan = exitFixture()
	plan.Export.Complete = false
	if _, err := DryRunExit(plan); !errors.Is(err, ErrIncompleteExit) {
		t.Fatalf("incomplete export error = %v", err)
	}
}

func TestTodo_WEDGE_009_Integration(t *testing.T) {
	receipt, err := DryRunExit(exitFixture())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TenantID != "tenant-placeholder" || receipt.ActiveConnectorCredentials != 0 || receipt.ExportDigest == "" || receipt.PlanDigest == "" {
		t.Fatalf("incomplete dry-run receipt: %+v", receipt)
	}
}

func TestTodo_WEDGE_009_Security(t *testing.T) {
	plan := exitFixture()
	plan.Export.TenantID = "another-tenant"
	if _, err := DryRunExit(plan); !errors.Is(err, ErrIncompleteExit) {
		t.Fatalf("cross-tenant export error = %v", err)
	}
	plan = exitFixture()
	plan.Holds[0].Reason = ""
	if _, err := DryRunExit(plan); !errors.Is(err, ErrIncompleteExit) {
		t.Fatalf("unowned hold error = %v", err)
	}
}
