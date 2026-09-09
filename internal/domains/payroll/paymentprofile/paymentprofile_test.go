package paymentprofile

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func profileInstant(t *testing.T, seconds int64) values.Instant {
	t.Helper()
	at, err := values.NewInstantFromUnix(seconds, 0)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func profileWindow(t *testing.T) values.EffectiveInterval {
	t.Helper()
	window, err := values.NewInstantInterval(profileInstant(t, 100), profileInstant(t, 500))
	if err != nil {
		t.Fatal(err)
	}
	return window
}

func profileFlow(t *testing.T, connector string) DataFlowMap {
	t.Helper()
	flow, err := NewDataFlowMap(DataFlowMap{
		Nodes: []DataFlowNode{
			{ID: "tenant", Kind: NodeTenant, PackagePath: "internal/domains/tenant"},
			{ID: "payroll", Kind: NodePayroll, PackagePath: "internal/domains/payroll"},
			{ID: "paymethod", Kind: NodePaymethod, PackagePath: "internal/domains/paymethod"},
			{ID: "dlp", Kind: NodeDLP, PackagePath: "internal/trust/dlp"},
			{ID: "connector", Kind: NodeConnector, PackagePath: "internal/connectivity", ConnectorID: connector},
			{ID: "provider", Kind: NodeExternal, PackagePath: "connector/provider", ConnectorID: connector},
		},
		Edges: []DataFlowEdge{
			{From: "tenant", To: "payroll", PackagePath: "internal/domains/tenant", ConnectorID: connector, Action: Processes, Classes: []PaymentDataClass{ClassCardholder, ClassACHAccount, ClassFTI}},
			{From: "payroll", To: "paymethod", PackagePath: "internal/domains/payroll", ConnectorID: connector, Action: CanAffect, Classes: []PaymentDataClass{ClassCardholder, ClassACHAccount, ClassFTI}},
			{From: "paymethod", To: "dlp", PackagePath: "internal/domains/paymethod", ConnectorID: connector, Action: Processes, Classes: []PaymentDataClass{ClassCardholder, ClassACHAccount, ClassFTI}},
			{From: "dlp", To: "connector", PackagePath: "internal/trust/dlp", ConnectorID: connector, Action: Transmits, Classes: []PaymentDataClass{ClassCardholder, ClassACHAccount, ClassFTI}},
			{From: "connector", To: "provider", PackagePath: "internal/connectivity", ConnectorID: connector, Action: Transmits, Classes: []PaymentDataClass{ClassCardholder, ClassACHAccount, ClassFTI}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func profileDecision(t *testing.T, tenantID, connector string, kind DecisionKind, outcome DecisionOutcome) ApplicabilityDecision {
	t.Helper()
	return ApplicabilityDecision{
		ID: "decision-" + strings.ToLower(string(kind)), Kind: kind, Outcome: outcome,
		DataFlow: profileFlow(t, connector), BoundaryTestRef: BoundaryTestRef,
		EvidenceRef: "evidence:" + tenantID + ":" + strings.ToLower(string(kind)),
		Effective:   profileWindow(t), Revision: 1,
	}
}

func profileFixture(t *testing.T, tenantID, connector string) PaymentDataProfile {
	t.Helper()
	profile, err := NewPaymentDataProfile(PaymentDataProfile{
		TenantRef: tenantID, IntegrationRef: connector, ConnectorVersion: connectivity.Version{Major: 1, Minor: 0, Patch: 0},
		Revision: 1, Effective: profileWindow(t), PreparedBy: "policy-author",
		Review: ReviewerSignOff{Reviewer: "independent-reviewer", SignatureRef: "review-signature:" + tenantID, SignedAt: profileInstant(t, 90)},
		Decisions: []ApplicabilityDecision{
			profileDecision(t, tenantID, connector, PCIScopeDecision, Applicable),
			profileDecision(t, tenantID, connector, NachaThresholdDecision, Applicable),
			profileDecision(t, tenantID, connector, GLBACoveredInstitutionDecision, NotApplicable),
			profileDecision(t, tenantID, connector, FTIDecision, NotApplicable),
		},
	})
	if err != nil {
		t.Fatalf("NewPaymentDataProfile: %v", err)
	}
	return profile
}

// TestTodo_SECARCH_012 is the primary contract: a signed, immutable profile
// resolves all four payment-data questions for one tenant and connector.
func TestTodo_SECARCH_012(t *testing.T) {
	profile := profileFixture(t, "tenant-a", "acme-payments")
	resolved, err := Resolve("tenant-a", profileInstant(t, 200), []PaymentDataProfile{profile})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.CanonicalDigest == "" || len(resolved.Decisions) != 4 {
		t.Fatalf("resolved profile is incomplete: %+v", resolved)
	}
	if resolved.Decisions[0].DataFlow.CanonicalDigest == "" {
		t.Fatal("decision flow was not digested")
	}
	for _, kind := range []DecisionKind{PCIScopeDecision, NachaThresholdDecision, GLBACoveredInstitutionDecision, FTIDecision} {
		found := false
		for _, decision := range resolved.Decisions {
			if decision.Kind == kind {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing decision %s", kind)
		}
	}
	if profile.CanonicalDigest != resolved.CanonicalDigest {
		t.Fatal("resolution changed the immutable profile")
	}
}

// TestTodo_SECARCH_012_Golden covers two independent tenant fixtures and pins
// their deterministic signed revision digests and resolved outcomes.
func TestTodo_SECARCH_012_Golden(t *testing.T) {
	fixtures := []struct {
		profile PaymentDataProfile
		at      int64
		want    []DecisionOutcome
	}{
		{profileFixture(t, "tenant-a", "acme-payments"), 200, []DecisionOutcome{Applicable, Applicable, NotApplicable, NotApplicable}},
		{profileFixture(t, "tenant-b", "other-payments"), 300, []DecisionOutcome{Applicable, Applicable, NotApplicable, NotApplicable}},
	}
	const wantA = "sha256:73b2283d50859cc1f2c3e20b78c7fe678cceb7cee086f3cc8d2431cb2e0287a7"
	const wantB = "sha256:622c5a880069b5b1ce3f051023a6d594fc5de75fca44ed9d5031242d8a6d5191"
	for i, fixture := range fixtures {
		resolved, err := Resolve(fixture.profile.TenantRef, profileInstant(t, fixture.at), []PaymentDataProfile{fixture.profile})
		if err != nil {
			t.Fatalf("fixture %d Resolve: %v", i, err)
		}
		for j, want := range fixture.want {
			if resolved.Decisions[j].Outcome != want {
				t.Fatalf("fixture %d decision %d = %s, want %s", i, j, resolved.Decisions[j].Outcome, want)
			}
		}
		wantDigest := wantA
		if i == 1 {
			wantDigest = wantB
		}
		if resolved.CanonicalDigest != wantDigest {
			t.Fatalf("fixture %d digest = %s, want %s", i, resolved.CanonicalDigest, wantDigest)
		}
	}
}

// TestTodo_SECARCH_012_Security proves missing, unsigned and unknown-boundary
// refusals name their offending fields and that explanations contain no IDs.
func TestTodo_SECARCH_012_Security(t *testing.T) {
	profile := profileFixture(t, "tenant-a", "acme-payments")
	missing := profile
	missing.Decisions = append([]ApplicabilityDecision(nil), profile.Decisions[:3]...)
	if _, err := NewPaymentDataProfile(missing); !errors.Is(err, ErrMissingDecision) {
		t.Fatalf("missing decision error = %v", err)
	} else {
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "decisions.FTI" {
			t.Fatalf("missing decision field = %+v", field)
		}
	}
	unsigned := profile
	unsigned.CanonicalDigest = ""
	if _, err := Resolve("tenant-a", profileInstant(t, 200), []PaymentDataProfile{unsigned}); !errors.Is(err, ErrUnsignedProfile) {
		t.Fatalf("unsigned profile error = %v", err)
	} else {
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "canonical_digest" {
			t.Fatalf("unsigned profile field = %+v", field)
		}
	}
	unknown := profile
	unknown.Decisions = append([]ApplicabilityDecision(nil), profile.Decisions...)
	unknown.Decisions[0].BoundaryTestRef = "internal/domains/payroll/paymentprofile/missing_test.go#TestMissingBoundary"
	unknown.Decisions[0].CanonicalDigest = ""
	if _, err := NewPaymentDataProfile(unknown); !errors.Is(err, ErrUnknownBoundaryTest) {
		t.Fatalf("unknown boundary error = %v", err)
	} else {
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "decision.boundary_test_ref" {
			t.Fatalf("unknown boundary field = %+v", field)
		}
	}
	explanation, err := profile.Explain()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"tenant-a", "acme-payments", "independent-reviewer", "signature"} {
		if strings.Contains(fmtProfileExplanation(explanation), secret) {
			t.Fatalf("explanation leaked %q", secret)
		}
	}
}

// TestTodo_SECARCH_012_Integration wires the real tenant, payroll, paymethod,
// DLP and connector constructors into the profile's references.
func TestTodo_SECARCH_012_Integration(t *testing.T) {
	key := [32]byte{1, 2, 3}
	placement, err := tenant.Sign(tenant.Placement{Tenant: "tenant-int", Cell: "cell-east", Region: "us-east", ResidencyProfile: "us-only", IsolationTier: "dedicated", Epoch: 1}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenant.Verify(placement, key); err != nil {
		t.Fatal(err)
	}
	if _, err := payroll.NewPayrollRun("run-int", "monthly", payroll.PeriodRef{ID: "period", Version: "v1", Digest: "sha256:period"}, payroll.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "sha256:population"}, "sha256:inputs"); err != nil {
		t.Fatal(err)
	}
	window := profileWindow(t)
	if _, err := paymethod.NewDestination(paymethod.Destination{DestinationID: "destination-int", WorkerRef: "worker-int", Rail: paymethod.RailACH, Risk: paymethod.RiskMedium, GovernedRef: "vault-ref", DisplayHint: "••••1234", Currency: "USD", CountryCode: "US", Effective: window}); err != nil {
		t.Fatal(err)
	}
	detector, err := dlp.NewDetector("payment-class", dlp.ClassBank, dlp.SeverityHigh, `account-[0-9]+`)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := dlp.NewInspector(detector)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspector.Inspect([]byte("tokenized payment"))
	if err != nil || len(inspection.Findings) != 0 {
		t.Fatalf("DLP inspection = %+v, %v", inspection, err)
	}
	version, err := connectivity.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := NewPaymentDataProfile(PaymentDataProfile{
		TenantRef: "tenant-int", IntegrationRef: "acme-payments", ConnectorVersion: version, Revision: 1,
		Decisions: []ApplicabilityDecision{
			profileDecision(t, placement.Tenant, "acme-payments", PCIScopeDecision, Applicable),
			profileDecision(t, placement.Tenant, "acme-payments", NachaThresholdDecision, Applicable),
			profileDecision(t, placement.Tenant, "acme-payments", GLBACoveredInstitutionDecision, NotApplicable),
			profileDecision(t, placement.Tenant, "acme-payments", FTIDecision, NotApplicable),
		},
		Effective: window, PreparedBy: "author-int",
		Review: ReviewerSignOff{Reviewer: "reviewer-int", SignatureRef: "review-int", SignedAt: profileInstant(t, 90)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("tenant-int", profileInstant(t, 200), []PaymentDataProfile{profile}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_SECARCH_012_Mutation proves a changed decision cannot reuse the
// original digest and cannot be passed as a signed revision.
func TestTodo_SECARCH_012_Mutation(t *testing.T) {
	profile := profileFixture(t, "tenant-a", "acme-payments")
	changed := profile
	changed.Decisions = append([]ApplicabilityDecision(nil), profile.Decisions...)
	changed.Decisions[0].Outcome = NotApplicable
	if changed.computedDigest() == profile.CanonicalDigest {
		t.Fatal("changed decision reused profile digest")
	}
	if _, err := Resolve("tenant-a", profileInstant(t, 200), []PaymentDataProfile{changed}); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("tampered profile error = %v", err)
	}
}

func fmtProfileExplanation(explanation ProfileExplanation) string {
	return strings.Join([]string{
		fmtUint(explanation.Revision), fmtUint(uint64(explanation.DecisionCount)),
		fmtUint(uint64(explanation.FlowCount)), explanation.Digest,
	}, "|")
}

func fmtUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}
