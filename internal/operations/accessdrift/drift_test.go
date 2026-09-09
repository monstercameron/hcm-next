package accessdrift

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

func resource(kind ResourceKind, id, subject, value, state string, risk Risk) Resource {
	return Resource{ID: id, Kind: kind, Subject: subject, Value: value, State: state, Risk: risk}
}
func observed(r Resource, state string, complete bool, freshness observe.Freshness) Resource {
	r.State, r.Complete, r.Freshness, r.ProviderVersion, r.ObservedAt = state, complete, freshness, "provider-1", time.Unix(100, 0)
	return r
}

func TestAccessReconciliationClassifiesAndRepairsPartialLogicalDeviceAndBadgeDrift(t *testing.T) {
	expected := []Resource{resource(KindAccount, "a-1", "worker", "account", "ACTIVE", RiskLow), resource(KindDevice, "d-1", "worker", "laptop", "ACTIVE", RiskHigh), resource(KindBadge, "b-1", "worker", "zone-a", "REVOKED", RiskPrivileged)}
	got, err := Reconcile(ReconcileRequest{Tenant: "tenant-1", AsOf: time.Unix(120, 0), Expected: expected, Observed: []Resource{observed(expected[0], "ACTIVE", true, observe.FreshnessFresh), observed(expected[1], "LOCKED", true, observe.FreshnessFresh), observed(resource(KindBadge, "b-1", "worker", "zone-a", "ACTIVE", RiskPrivileged), "ACTIVE", true, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	var matched, device, badge Finding
	for _, finding := range got.Findings {
		switch finding.Kind {
		case KindAccount:
			matched = finding
		case KindDevice:
			device = finding
		case KindBadge:
			badge = finding
		}
	}
	if matched.Status != StatusMatch || device.Status != StatusPartial || badge.Severity != SeverityCritical {
		t.Fatalf("report = %#v", got)
	}
	plan, err := CreateRepairPlan(RepairRequest{Report: got, FindingIDs: []string{got.Findings[1].ID, got.Findings[2].ID}, Actor: "access-ops", IdempotencyKey: "repair-1", At: time.Unix(130, 0), Freshness: observe.FreshnessFresh, Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || !plan.RequiresApproval {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestTodo_ACCESS_004_Property(t *testing.T) {
	want := resource(KindEntitlement, "e-1", "worker", "read", "ACTIVE", RiskLow)
	for i := 0; i < 10; i++ {
		r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessFresh)}})
		if err != nil || r.Findings[0].Status != StatusMatch {
			t.Fatalf("iteration %d: %#v %v", i, r, err)
		}
	}
}

func TestTodo_ACCESS_004_Golden(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskLow)
	r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: nil})
	if err != nil {
		t.Fatal(err)
	}
	if r.Findings[0].Status != StatusUnknown || r.Freshness != observe.FreshnessUnknown {
		t.Fatalf("unknown report = %#v", r)
	}
}

func TestTodo_ACCESS_004_Security(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskPrivileged)
	r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(resource(KindAccount, "a", "w", "v", "ACTIVE", RiskPrivileged), "REVOKED", true, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Findings[0].Status != StatusPartial {
		t.Fatal("state drift was not detected")
	}
	if _, err := CreateRepairPlan(RepairRequest{Report: r, FindingIDs: []string{r.Findings[0].ID}, ParentTransactionID: "employment-1", Actor: "ops", IdempotencyKey: "x", At: time.Unix(3, 0), Freshness: observe.FreshnessFresh, Complete: true}); !errors.Is(err, ErrRepairParent) {
		t.Fatalf("parent transaction = %v", err)
	}
}

func TestTodo_ACCESS_004_Integration(t *testing.T) {
	want := resource(KindDevice, "d", "w", "laptop", "ACTIVE", RiskHigh)
	r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessStale)}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Findings[0].Status != StatusStale {
		t.Fatalf("stale = %#v", r.Findings[0])
	}
	if _, err := CreateRepairPlan(RepairRequest{Report: r, FindingIDs: []string{r.Findings[0].ID}, Actor: "ops", IdempotencyKey: "x", At: time.Unix(3, 0), Freshness: observe.FreshnessStale, Complete: true}); !errors.Is(err, ErrFreshObservation) {
		t.Fatalf("stale repair = %v", err)
	}
}

func TestTodo_ACCESS_004_Race(t *testing.T) {
	want := resource(KindEntitlement, "e", "w", "read", "ACTIVE", RiskLow)
	done := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() {
			_, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessFresh)}})
			done <- err
		}()
	}
	for i := 0; i < 12; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_ACCESS_004_Fault(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskLow)
	if _, err := Reconcile(ReconcileRequest{Tenant: "", AsOf: time.Time{}, Expected: []Resource{want}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid request = %v", err)
	}
}

func TestTodo_ACCESS_004_Conformance(t *testing.T) {
	want := resource(KindBadge, "b", "w", "zone", "ACTIVE", RiskLow)
	got, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(resource(KindBadge, "b", "w", "zone", "ACTIVE", RiskLow), "ACTIVE", false, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Findings[0].Status != StatusPartial || got.Complete {
		t.Fatalf("incomplete = %#v", got)
	}
}

func TestTodo_ACCESS_004_Mutation(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskLow)
	r, _ := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessFresh)}})
	original := r.Findings[0].ID
	r.Findings[0].Status = StatusExcess
	if original != r.Findings[0].ID {
		t.Fatal("finding identity changed")
	}
}

func TestVocabulary_ValidatesClosedSecurityEnums(t *testing.T) {
	for _, kind := range []ResourceKind{KindAccount, KindEntitlement, KindDevice, KindBadge} {
		if !kind.Valid() {
			t.Errorf("resource kind %q rejected", kind)
		}
	}
	if ResourceKind("UNKNOWN").Valid() {
		t.Fatal("unknown resource kind accepted")
	}
	for _, risk := range []Risk{RiskLow, RiskHigh, RiskPrivileged} {
		if !risk.Valid() {
			t.Errorf("risk %q rejected", risk)
		}
	}
	if Risk("MEDIUM").Valid() {
		t.Fatal("unknown risk accepted")
	}
	for _, status := range []DriftStatus{StatusMatch, StatusMissing, StatusExcess, StatusPartial, StatusStale, StatusUnknown} {
		if !status.Valid() {
			t.Errorf("status %q rejected", status)
		}
	}
	if DriftStatus("GRANTED").Valid() {
		t.Fatal("unknown status accepted")
	}
}

func TestReconcile_RejectsMalformedAndDuplicateEvidence(t *testing.T) {
	base := resource(KindAccount, "account", "worker", "value", "ACTIVE", RiskLow)
	tests := []struct {
		name string
		make func() ReconcileRequest
	}{
		{"missing tenant", func() ReconcileRequest { return ReconcileRequest{AsOf: time.Unix(1, 0), Expected: []Resource{base}} }},
		{"missing as of", func() ReconcileRequest { return ReconcileRequest{Tenant: "tenant", Expected: []Resource{base}} }},
		{"invalid expected identity", func() ReconcileRequest {
			r := base
			r.ID = ""
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{r}}
		}},
		{"invalid expected kind", func() ReconcileRequest {
			r := base
			r.Kind = "bad"
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{r}}
		}},
		{"invalid expected state", func() ReconcileRequest {
			r := base
			r.State = ""
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{r}}
		}},
		{"invalid expected risk", func() ReconcileRequest {
			r := base
			r.Risk = "bad"
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{r}}
		}},
		{"invalid observed freshness", func() ReconcileRequest {
			r := observed(base, base.State, true, "bad")
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{base}, Observed: []Resource{r}}
		}},
		{"observed without timestamp", func() ReconcileRequest {
			r := base
			r.Freshness = observe.FreshnessFresh
			r.ProviderVersion = "provider"
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{base}, Observed: []Resource{r}}
		}},
		{"observed without provider version", func() ReconcileRequest {
			r := observed(base, base.State, true, observe.FreshnessFresh)
			r.ProviderVersion = ""
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{base}, Observed: []Resource{r}}
		}},
		{"duplicate expected", func() ReconcileRequest {
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{base, base}}
		}},
		{"duplicate observed", func() ReconcileRequest {
			r := observed(base, base.State, true, observe.FreshnessFresh)
			return ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{base}, Observed: []Resource{r, r}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Reconcile(tt.make())
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestReconcile_ClassifiesEveryEvidenceStateAndDetectMatches(t *testing.T) {
	want := resource(KindAccount, "a", "worker", "value", "ACTIVE", RiskLow)
	other := resource(KindDevice, "d", "worker", "laptop", "ACTIVE", RiskHigh)
	observedWant := observed(want, want.State, true, observe.FreshnessFresh)
	cases := []struct {
		name   string
		want   []Resource
		got    []Resource
		target string
		status DriftStatus
	}{
		{"match", []Resource{want}, []Resource{observedWant}, want.ID, StatusMatch},
		{"missing", []Resource{want, other}, []Resource{observedWant}, other.ID, StatusMissing},
		{"excess", []Resource{want}, []Resource{observedWant, observed(other, other.State, true, observe.FreshnessFresh)}, other.ID, StatusExcess},
		{"partial", []Resource{want}, []Resource{observed(want, "DISABLED", true, observe.FreshnessFresh)}, want.ID, StatusPartial},
		{"stale", []Resource{want}, []Resource{observed(want, want.State, true, observe.FreshnessStale)}, want.ID, StatusStale},
		{"incomplete", []Resource{want}, []Resource{observed(want, want.State, false, observe.FreshnessFresh)}, want.ID, StatusPartial},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(200, 0), Expected: tc.want, Observed: tc.got}
			got, err := Reconcile(req)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			found := false
			for _, finding := range got.Findings {
				if finding.ResourceID == tc.target {
					found = true
					if finding.Status != tc.status {
						t.Fatalf("status = %s, want %s", finding.Status, tc.status)
					}
				}
			}
			if !found {
				t.Fatalf("no finding for %q: %#v", want.ID, got.Findings)
			}
			detected, err := Detect(req)
			if err != nil || !reflect.DeepEqual(detected, got) {
				t.Fatalf("Detect = %#v, %v; Reconcile = %#v", detected, err, got)
			}
		})
	}

	unknown, err := Reconcile(ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(200, 0), Expected: []Resource{want}})
	if err != nil || unknown.Freshness != observe.FreshnessUnknown || unknown.Complete {
		t.Fatalf("empty observation report = %#v, %v", unknown, err)
	}
	if unknown.Findings[0].Status != StatusUnknown {
		t.Fatalf("empty observation status = %s", unknown.Findings[0].Status)
	}
	if Explain(unknown) != unknown.Explain() || unknown.Explain() == "" {
		t.Fatalf("report explanation = %q", unknown.Explain())
	}
}

func TestReconcile_UsesWorstFreshnessAndCriticalSeverity(t *testing.T) {
	want := resource(KindBadge, "badge", "worker", "zone", "REVOKED", RiskPrivileged)
	stale := resource(KindDevice, "device", "worker", "laptop", "ACTIVE", RiskLow)
	report, err := Reconcile(ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(200, 0), Expected: []Resource{want, stale}, Observed: []Resource{
		observed(want, "ACTIVE", true, observe.FreshnessFresh),
		observed(stale, stale.State, true, observe.FreshnessUnavailable),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Freshness != observe.FreshnessUnavailable {
		t.Fatalf("worst freshness = %s", report.Freshness)
	}
	if report.Findings[0].Severity != SeverityCritical {
		t.Fatalf("privileged revoked drift severity = %s", report.Findings[0].Severity)
	}
}

func TestReconcile_OrdersAllFreshnessRanks(t *testing.T) {
	base := resource(KindAccount, "account", "worker", "value", "ACTIVE", RiskLow)
	for _, freshness := range []observe.Freshness{observe.FreshnessStale, observe.FreshnessPartial, observe.FreshnessUnknown, observe.FreshnessUnavailable} {
		t.Run(string(freshness), func(t *testing.T) {
			got, err := Reconcile(ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(1, 0), Expected: []Resource{base}, Observed: []Resource{observed(base, base.State, true, freshness)}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Freshness != freshness || got.Findings[0].Status != StatusStale {
				t.Fatalf("freshness=%s report=%#v", freshness, got)
			}
		})
	}
}

func TestCreateRepairPlan_ValidatesGuardsAndMapsActions(t *testing.T) {
	wantMissing := resource(KindAccount, "missing", "worker", "value", "ACTIVE", RiskHigh)
	wantPartial := resource(KindDevice, "partial", "worker", "laptop", "ACTIVE", RiskLow)
	report, err := Reconcile(ReconcileRequest{Tenant: "tenant", AsOf: time.Unix(200, 0), Expected: []Resource{wantMissing, wantPartial}, Observed: []Resource{observed(wantPartial, "LOCKED", true, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{report.Findings[0].ID, report.Findings[1].ID}
	plan, err := CreateRepairPlan(RepairRequest{Report: report, FindingIDs: ids, Actor: "operator", IdempotencyKey: "repair-1", At: time.Unix(210, 0), Freshness: observe.FreshnessFresh, Complete: true})
	if err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	if len(plan.Steps) != 2 || !plan.RequiresApproval || plan.CanonicalDigest == "" {
		t.Fatalf("repair plan = %#v", plan)
	}
	actions := map[string]RepairAction{}
	for _, step := range plan.Steps {
		actions[step.ResourceID] = step.Action
	}
	if actions[wantMissing.ID] != RepairGrant || actions[wantPartial.ID] != RepairRevoke {
		t.Fatalf("repair actions = %#v", actions)
	}
	if plan.Explain() == "" || plan.ReportDigest != report.CanonicalDigest || Explain(report) != report.Explain() {
		t.Fatalf("explanations/parent digest = %#v", plan)
	}

	tests := []struct {
		name string
		err  error
	}{
		{"missing actor", ErrInvalidRequest},
		{"missing idempotency", ErrInvalidRequest},
		{"missing report digest", ErrInvalidRequest},
		{"parent transaction", ErrRepairParent},
		{"stale request", ErrFreshObservation},
		{"no findings", ErrInvalidRequest},
		{"duplicate finding", ErrInvalidRequest},
		{"unknown finding", ErrRepairConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := RepairRequest{Report: report, FindingIDs: []string{ids[0]}, Actor: "operator", IdempotencyKey: "repair-2", At: time.Unix(210, 0), Freshness: observe.FreshnessFresh, Complete: true}
			switch tc.name {
			case "missing actor":
				req.Actor = ""
			case "missing idempotency":
				req.IdempotencyKey = ""
			case "missing report digest":
				req.Report.CanonicalDigest = ""
			case "parent transaction":
				req.ParentTransactionID = "employment-1"
			case "stale request":
				req.Freshness = observe.FreshnessStale
			case "no findings":
				req.FindingIDs = nil
			case "duplicate finding":
				req.FindingIDs = []string{ids[0], ids[0]}
			case "unknown finding":
				req.FindingIDs = []string{"missing-finding"}
			}
			_, gotErr := CreateRepairPlan(req)
			if !errors.Is(gotErr, tc.err) {
				t.Fatalf("error = %v, want %v", gotErr, tc.err)
			}
		})
	}

	for _, status := range []DriftStatus{StatusMatch, StatusStale, StatusUnknown} {
		bad := report
		bad.Findings = []Finding{{ID: "f", ResourceID: "r", Kind: KindAccount, Subject: "s", Status: status, Risk: RiskLow}}
		bad.CanonicalDigest = "report"
		if _, err := CreateRepairPlan(RepairRequest{Report: bad, FindingIDs: []string{"f"}, Actor: "operator", IdempotencyKey: "key", At: time.Unix(1, 0), Freshness: observe.FreshnessFresh, Complete: true}); !errors.Is(err, ErrRepairConflict) {
			t.Fatalf("status %s error = %v", status, err)
		}
	}
	investigate := report
	investigate.Findings = []Finding{{ID: "investigate", ResourceID: "r", Kind: KindDevice, Subject: "s", Status: StatusPartial, Risk: RiskLow}}
	investigate.CanonicalDigest = "report"
	plan, err = CreateRepairPlan(RepairRequest{Report: investigate, FindingIDs: []string{"investigate"}, Actor: "operator", IdempotencyKey: "key", At: time.Unix(1, 0), Freshness: observe.FreshnessFresh, Complete: true})
	if err != nil || plan.Steps[0].Action != RepairInvestigate {
		t.Fatalf("investigation plan = %#v, %v", plan, err)
	}
}
