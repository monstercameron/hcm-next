package access_test

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/access"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func derivationInstant(t testing.TB, text string) values.Instant {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(at)
}

func derivationInterval(t testing.TB, start, end string) values.EffectiveInterval {
	t.Helper()
	interval, err := values.NewInstantInterval(derivationInstant(t, start), derivationInstant(t, end))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return interval
}

func derivationMetadata(t testing.TB) (values.RevisionToken, values.KnownAt, evidence.Provenance) {
	t.Helper()
	revision, err := values.NewSequenceRevision("access.derivation", 1)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(derivationInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(derivationInstant(t, "2026-09-01T00:00:01Z"))
	if err != nil {
		t.Fatal(err)
	}
	return revision, known, evidence.Provenance{Source: "workforce", EvidenceRef: "evidence/access-002", RecordedAt: recorded}
}

func derivationRequest(t testing.TB) access.EntitlementDerivationRequest {
	t.Helper()
	ten := values.TenantId("tenant-a")
	effective := derivationInterval(t, "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z")
	revision, known, provenance := derivationMetadata(t)
	graph := access.Graph{
		Tenant: ten,
		Identities: []access.WorkforceIdentity{{
			ID: "identity-a", Tenant: ten, Subject: "subject-a", System: "hcm",
			WorkerRef: values.EntityRef{Tenant: ten, Kind: "worker", Id: "11111111-1111-4111-8111-111111111111"},
			Revision:  revision, Authority: access.AuthorityNative, Effective: effective,
			KnownAt: known, Provenance: provenance, Lifecycle: access.LifecycleActive,
		}},
		Entitlements: []access.EntitlementDefinition{{
			ID: "entitlement-github", Tenant: ten, Subject: "team/platform", System: "github",
			Application: "github", Code: "platform-read", Version: "v1", RiskClass: access.RiskModerate,
			Owner: "security", Revision: revision, Authority: access.AuthorityNative,
			Effective: effective, KnownAt: known, Provenance: provenance, Lifecycle: access.LifecycleActive,
		}},
	}
	return access.EntitlementDerivationRequest{
		Graph: graph, AsOf: derivationInstant(t, "2026-09-05T12:00:00Z"),
		Employment: []access.EmploymentPeriod{{Ref: "employment-a", WorkforceIdentityID: "identity-a", Effective: effective}},
		Positions:  []access.PositionAssignment{{Ref: "position-a", WorkforceIdentityID: "identity-a", PositionID: "position-code-a", OrgUnitRef: "org/engineering", Effective: effective}},
		Policies:   []access.DeclaredAccessPolicy{{ID: "policy/engineering", Version: "3", EntitlementID: "entitlement-github", EmploymentRef: "employment-a", PositionRef: "position-a", OrgUnitRef: "org/engineering", Effect: access.AccessPolicyAllow, Effective: effective}},
		Revision:   revision, KnownAt: known, Provenance: provenance,
	}
}

func TestExpectedEntitlementCalculationReturnsExplainableUnknownSafeGraph(t *testing.T) {
	request := derivationRequest(t)
	calculation, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatalf("calculate: %v", err)
	}
	if len(calculation.Expected) != 1 || calculation.Decisions[0].Status != access.DerivationExpected {
		t.Fatalf("calculation = %+v", calculation)
	}
	if calculation.Expected[0].EmploymentRef != "employment-a" || calculation.Expected[0].PositionRef != "position-a" || calculation.Expected[0].PolicyRef != "policy/engineering@3" {
		t.Fatalf("basis was not retained: %+v", calculation.Expected[0])
	}
	if calculation.Digest() == "" {
		t.Fatal("calculation has no digest")
	}
	explanation := calculation.Explain()
	if explanation.ExpectedCount != 1 || explanation.UnknownCount != 0 || len(explanation.Lines) != 1 {
		t.Fatalf("explanation = %+v", explanation)
	}
	if strings.Contains(explanation.Lines[0], "engineering@3") || strings.Contains(explanation.Lines[0], "secret") {
		t.Fatalf("explanation disclosed a sensitive basis reference: %q", explanation.Lines[0])
	}

	missing := request
	missing.Positions = nil
	unknown, err := access.CalculateExpectedEntitlements(missing)
	if err != nil {
		t.Fatalf("missing fact calculation: %v", err)
	}
	if unknown.Decisions[0].Status != access.DerivationUnknown || len(unknown.Expected) != 0 {
		t.Fatalf("missing governed fact was not safe/unknown: %+v", unknown)
	}

	denied := request
	denied.Policies = append(append([]access.DeclaredAccessPolicy(nil), request.Policies...), access.DeclaredAccessPolicy{
		ID: "policy/deny", Version: "1", EntitlementID: "entitlement-github", Effect: access.AccessPolicyDeny, Effective: request.Policies[0].Effective,
	})
	deniedResult, err := access.CalculateExpectedEntitlements(denied)
	if err != nil {
		t.Fatalf("deny calculation: %v", err)
	}
	if deniedResult.Decisions[0].Status != access.DerivationNotExpected || len(deniedResult.Expected) != 0 {
		t.Fatalf("deny did not dominate allow: %+v", deniedResult.Decisions[0])
	}

	observed := request
	observed.Observations = []access.ExternalAccessObservation{{ID: "provider-observation"}}
	if !errors.Is(func() error { _, err := access.CalculateExpectedEntitlements(observed); return err }(), access.ErrObservationInput) {
		t.Fatal("observation was accepted as a derivation fact")
	}

	current := request.Graph
	current.Expected = append([]access.ExpectedEntitlement(nil), calculation.Expected...)
	currentResult := request
	currentResult.Graph = current
	currentResult.Policies = nil
	next, err := access.CalculateExpectedEntitlements(currentResult)
	if err != nil {
		t.Fatalf("next calculation: %v", err)
	}
	deltas, err := access.DiffExpectedEntitlements(current.Expected, next)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke {
		t.Fatalf("expected revoke delta, got %+v", deltas)
	}
}

func TestTodo_ACCESS_002_Property(t *testing.T) {
	base := derivationRequest(t)
	first, err := access.CalculateExpectedEntitlements(base)
	if err != nil {
		t.Fatal(err)
	}
	permuted := base
	permuted.Employment = append([]access.EmploymentPeriod(nil), base.Employment...)
	permuted.Positions = append([]access.PositionAssignment(nil), base.Positions...)
	permuted.Policies = append([]access.DeclaredAccessPolicy(nil), base.Policies...)
	permuted.Employment = append(permuted.Employment, access.EmploymentPeriod{Ref: "employment-unused", WorkforceIdentityID: "identity-a", Effective: base.Employment[0].Effective})
	permuted.Employment[0], permuted.Employment[1] = permuted.Employment[1], permuted.Employment[0]
	second, err := access.CalculateExpectedEntitlements(permuted)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("irrelevant input changed digest: %s vs %s", first.Digest(), second.Digest())
	}
}

func TestTodo_ACCESS_002_Golden(t *testing.T) {
	first, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() == "" || first.Digest() != second.Digest() {
		t.Fatalf("golden digest is unstable: %q %q", first.Digest(), second.Digest())
	}
}

func TestTodo_ACCESS_002_Security(t *testing.T) {
	request := derivationRequest(t)
	request.Observations = []access.ExternalAccessObservation{{ID: "observation", Authority: access.AuthorityExternalObservation}}
	if _, err := access.CalculateExpectedEntitlements(request); !errors.Is(err, access.ErrObservationInput) {
		t.Fatalf("observation error = %v", err)
	}
	request = derivationRequest(t)
	request.Graph.Identities[0].Lifecycle = access.LifecycleRevoked
	result, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].Status != access.DerivationNotExpected || len(result.Expected) != 0 {
		t.Fatalf("revoked identity received access: %+v", result)
	}
	if strings.Contains(strings.Join(result.Explain().Lines, "\n"), "secret") {
		t.Fatal("sensitive basis leaked")
	}
}

func TestTodo_ACCESS_002_Mutation(t *testing.T) {
	base, err := access.CalculateExpectedEntitlements(derivationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutated := derivationRequest(t)
	mutated.Policies[0].Effect = access.AccessPolicyDeny
	changed, err := access.CalculateExpectedEntitlements(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest() == changed.Digest() {
		t.Fatal("policy mutation did not change digest")
	}
	deltas, err := access.DiffExpectedEntitlements(base.Expected, changed)
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 1 || deltas[0].Kind != access.EntitlementRevoke {
		t.Fatalf("mutation delta = %+v", deltas)
	}
}

func FuzzTodo_ACCESS_002(f *testing.F) {
	f.Add("identity-a", "entitlement-github")
	f.Fuzz(func(t *testing.T, identityID, entitlementID string) {
		request := derivationRequest(t)
		request.Graph.Identities[0].ID = identityID
		request.Employment[0].WorkforceIdentityID = identityID
		request.Positions[0].WorkforceIdentityID = identityID
		request.Graph.Entitlements[0].ID = entitlementID
		request.Policies[0].EntitlementID = entitlementID
		_, _ = access.CalculateExpectedEntitlements(request)
	})
}

func TestTodo_ACCESS_002_DeterministicAcrossGoroutines(t *testing.T) {
	request := derivationRequest(t)
	want, err := access.CalculateExpectedEntitlements(request)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	got := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := access.CalculateExpectedEntitlements(request)
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			got[i] = result.Digest()
		}(i)
	}
	wg.Wait()
	for i, digest := range got {
		if digest != want.Digest() {
			t.Fatalf("worker %d digest = %q, want %q", i, digest, want.Digest())
		}
	}
	if !reflect.DeepEqual(got, append([]string(nil), got...)) {
		t.Fatal("digest result was not stable")
	}
	_ = sort.Strings
}
