package access_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/access"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type fixture struct {
	identity    access.WorkforceIdentity
	account     access.AccountLink
	entitlement access.EntitlementDefinition
	expected    access.ExpectedEntitlement
	observation access.ExternalAccessObservation
}

func mustInstant(t testing.TB, text string) values.Instant {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(at)
}

func mustRevision(t testing.TB, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatalf("revision %q: %v", stream, err)
	}
	return revision
}

func testCoordinates(t testing.TB, source, evidenceRef string) (values.EffectiveInterval, values.KnownAt, evidence.Provenance) {
	t.Helper()
	effective, err := values.NewInstantInterval(
		mustInstant(t, "2026-09-01T00:00:00Z"),
		mustInstant(t, "2027-09-01T00:00:00Z"),
	)
	if err != nil {
		t.Fatalf("effective interval: %v", err)
	}
	knownAt, err := values.NewKnownAt(mustInstant(t, "2026-09-02T10:00:00Z"))
	if err != nil {
		t.Fatalf("known at: %v", err)
	}
	recordedAt, err := values.NewRecordedAt(mustInstant(t, "2026-09-02T10:00:01Z"))
	if err != nil {
		t.Fatalf("recorded at: %v", err)
	}
	return effective, knownAt, evidence.Provenance{
		Source: source, EvidenceRef: evidenceRef, RecordedAt: recordedAt,
	}
}

func validFixture(t testing.TB, suffix string) fixture {
	t.Helper()
	tenant := values.TenantId("tenant-a")
	effective, knownAt, nativeProvenance := testCoordinates(t, "hcmnext.access", "evidence-native-"+suffix)
	_, _, externalProvenance := testCoordinates(t, "github", "evidence-observed-"+suffix)
	workerID := "11111111-1111-4111-8111-111111111111"
	if suffix == "b" {
		workerID = "22222222-2222-4222-8222-222222222222"
	}
	subject := "workforce-subject-" + suffix
	identityID := "identity-" + suffix
	accountID := "account-link-" + suffix
	entitlementID := "entitlement-" + suffix

	return fixture{
		identity: access.WorkforceIdentity{
			ID: identityID, Tenant: tenant, Subject: subject, System: "hcmnext.access",
			WorkerRef: values.EntityRef{Tenant: tenant, Kind: "worker", Id: workerID},
			Revision:  mustRevision(t, "access.identity."+suffix, 1), Authority: access.AuthorityNative,
			Effective: effective, KnownAt: knownAt, Provenance: nativeProvenance, Lifecycle: access.LifecycleActive,
		},
		account: access.AccountLink{
			ID: accountID, Tenant: tenant, Subject: "alice-" + suffix, System: "github",
			Source: "connection/github-prod", WorkforceIdentityID: identityID,
			Application: "github-enterprise", AccountID: "provider-account-" + suffix,
			Revision: mustRevision(t, "access.account."+suffix, 1), Authority: access.AuthorityNative,
			Effective: effective, KnownAt: knownAt, Provenance: nativeProvenance, Lifecycle: access.LifecycleActive,
		},
		entitlement: access.EntitlementDefinition{
			ID: entitlementID, Tenant: tenant, Subject: "team/platform-" + suffix, System: "github",
			Application: "github-enterprise", Code: "platform-write-" + suffix,
			Version: "2026.09", RiskClass: access.RiskHigh, Owner: "owner/platform-security",
			Revision: mustRevision(t, "access.entitlement."+suffix, 1), Authority: access.AuthorityNative,
			Effective: effective, KnownAt: knownAt, Provenance: nativeProvenance, Lifecycle: access.LifecycleActive,
		},
		expected: access.ExpectedEntitlement{
			ID: "expected-" + suffix, Tenant: tenant, Subject: subject, System: "github",
			WorkforceIdentityID: identityID, AccountLinkID: accountID, EntitlementID: entitlementID,
			EmploymentRef: "employment-" + suffix, PositionRef: "position-" + suffix, PolicyRef: "policy/birthright/3",
			Revision: mustRevision(t, "access.expected."+suffix, 1), Authority: access.AuthorityNative,
			Effective: effective, KnownAt: knownAt, Provenance: nativeProvenance, Lifecycle: access.LifecycleActive,
		},
		observation: access.ExternalAccessObservation{
			ID: "observation-" + suffix, Tenant: tenant, Subject: "alice-" + suffix, System: "github",
			Application: "github-enterprise", AccountID: "provider-account-" + suffix,
			EntitlementID: entitlementID, ProviderVersion: "etag-7", ObservedState: "GRANTED",
			Revision: mustRevision(t, "access.observation."+suffix, 1), Authority: access.AuthorityExternalObservation,
			Effective: effective, KnownAt: knownAt, Provenance: externalProvenance, Lifecycle: access.LifecycleActive,
		},
	}
}

func validGraph(t testing.TB) access.Graph {
	t.Helper()
	f := validFixture(t, "a")
	g, err := access.NewGraph(f.identity.Tenant)
	if err != nil {
		t.Fatalf("new graph: %v", err)
	}
	for _, record := range []access.Record{f.identity, f.account, f.entitlement, f.expected} {
		if err := g.Add(record); err != nil {
			t.Fatalf("add %T: %v", record, err)
		}
	}
	return g
}

func requireFieldError(t testing.TB, err error, field string, cause error) {
	t.Helper()
	if err == nil {
		t.Fatalf("field %s: expected rejection", field)
	}
	var fieldError *access.FieldError
	if !errors.As(err, &fieldError) {
		t.Fatalf("error %T %v is not *access.FieldError", err, err)
	}
	if fieldError.Field != field {
		t.Fatalf("field = %q, want %q (error %v)", fieldError.Field, field, err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want errors.Is(..., %v)", err, cause)
	}
}

func TestWorkforceAccessGraphRejectsUnownedAccountEntitlementAndSubjectLinks(t *testing.T) {
	f := validFixture(t, "a")

	t.Run("account requires an owned workforce identity", func(t *testing.T) {
		g, _ := access.NewGraph(f.identity.Tenant)
		account := f.account
		account.WorkforceIdentityID = "identity-missing"
		requireFieldError(t, g.Add(account), "workforce_identity_id", access.ErrUnownedReference)
	})

	for _, tc := range []struct {
		name   string
		field  string
		mutate func(*access.AccountLink)
	}{
		{"account system", "system", func(a *access.AccountLink) { a.System = "" }},
		{"account source", "source", func(a *access.AccountLink) { a.Source = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := access.NewGraph(f.identity.Tenant)
			if err := g.Add(f.identity); err != nil {
				t.Fatal(err)
			}
			account := f.account
			tc.mutate(&account)
			requireFieldError(t, g.Add(account), tc.field, access.ErrIncompleteRevision)
		})
	}

	for _, tc := range []struct {
		name   string
		field  string
		mutate func(*access.EntitlementDefinition)
	}{
		{"entitlement version", "version", func(e *access.EntitlementDefinition) { e.Version = "" }},
		{"entitlement risk", "risk_class", func(e *access.EntitlementDefinition) { e.RiskClass = access.RiskUnspecified }},
		{"entitlement owner", "owner", func(e *access.EntitlementDefinition) { e.Owner = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entitlement := f.entitlement
			tc.mutate(&entitlement)
			requireFieldError(t, entitlement.Validate(), tc.field, access.ErrIncompleteRevision)
		})
	}

	t.Run("expected entitlement requires an owned entitlement", func(t *testing.T) {
		g := validGraph(t)
		edge := f.expected
		edge.ID = "expected-unowned-entitlement"
		edge.EntitlementID = "entitlement-missing"
		requireFieldError(t, g.Add(edge), "entitlement_id", access.ErrUnownedReference)
	})

	t.Run("expected entitlement subject must match its workforce identity", func(t *testing.T) {
		g := validGraph(t)
		edge := f.expected
		edge.ID = "expected-wrong-subject"
		edge.Subject = "different-subject"
		requireFieldError(t, g.Add(edge), "subject", access.ErrUnownedReference)
	})

	t.Run("expected entitlement account must belong to its workforce identity", func(t *testing.T) {
		g := validGraph(t)
		other := validFixture(t, "b")
		if err := g.Add(other.identity); err != nil {
			t.Fatal(err)
		}
		if err := g.Add(other.account); err != nil {
			t.Fatal(err)
		}
		edge := f.expected
		edge.ID = "expected-wrong-account"
		edge.AccountLinkID = other.account.ID
		requireFieldError(t, g.Add(edge), "account_link_id", access.ErrUnownedReference)
	})

	t.Run("expected entitlement requires a governed basis", func(t *testing.T) {
		edge := f.expected
		edge.EmploymentRef, edge.PositionRef, edge.PolicyRef = "", "", ""
		requireFieldError(t, edge.Validate(), "basis", access.ErrIncompleteRevision)
	})

	t.Run("external observation cannot mutate the graph", func(t *testing.T) {
		g := validGraph(t)
		before := g.Digest()
		requireFieldError(t, g.Add(f.observation), "authority_class", access.ErrObservationMutation)
		if after := g.Digest(); after != before {
			t.Fatalf("rejected observation mutated graph: %s -> %s", before, after)
		}
	})
}

func TestTodo_ACCESS_001_Property(t *testing.T) {
	a := validFixture(t, "a")
	b := validFixture(t, "b")
	forward := access.Graph{
		Tenant:       a.identity.Tenant,
		Identities:   []access.WorkforceIdentity{a.identity, b.identity},
		Accounts:     []access.AccountLink{a.account, b.account},
		Entitlements: []access.EntitlementDefinition{a.entitlement, b.entitlement},
		Expected:     []access.ExpectedEntitlement{a.expected, b.expected},
	}
	reverse := access.Graph{
		Tenant:       a.identity.Tenant,
		Identities:   []access.WorkforceIdentity{b.identity, a.identity},
		Accounts:     []access.AccountLink{b.account, a.account},
		Entitlements: []access.EntitlementDefinition{b.entitlement, a.entitlement},
		Expected:     []access.ExpectedEntitlement{b.expected, a.expected},
	}
	if err := forward.Validate(); err != nil {
		t.Fatalf("forward graph: %v", err)
	}
	if err := reverse.Validate(); err != nil {
		t.Fatalf("reverse graph: %v", err)
	}
	if forward.Digest() == "" || forward.Digest() != reverse.Digest() {
		t.Fatalf("canonical digest depends on insertion order: %q vs %q", forward.Digest(), reverse.Digest())
	}
}

func TestTodo_ACCESS_001_Golden(t *testing.T) {
	const want = "sha256:3dad1827a4807555f7d54cd7d472477d2f2f6233f8f1b38adfd4f15eb3302c56"
	got := validGraph(t).Digest()
	if got != want {
		t.Fatalf("canonical graph digest = %q, want %q", got, want)
	}
}

func TestTodo_ACCESS_001_Security(t *testing.T) {
	g := validGraph(t)
	base := access.ExplainRequest{Tenant: g.Tenant, Purpose: "access_review", PolicyVersion: "authz/7"}

	for _, tc := range []struct {
		name string
		req  access.ExplainRequest
	}{
		{"nil authorizer", base},
		{"explicit denial", func() access.ExplainRequest {
			r := base
			r.Authorize = func(values.TenantId, string) bool { return false }
			return r
		}()},
		{"authorizer panic", func() access.ExplainRequest {
			r := base
			r.Authorize = func(values.TenantId, string) bool { panic("policy unavailable") }
			return r
		}()},
		{"cross tenant", func() access.ExplainRequest {
			r := base
			r.Tenant = "tenant-b"
			r.Authorize = func(values.TenantId, string) bool { return true }
			return r
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			explanation, err := g.Explain(tc.req)
			if !errors.Is(err, access.ErrUnauthorized) {
				t.Fatalf("error = %v, want ErrUnauthorized", err)
			}
			if explanation.CanonicalDigest != "" || len(explanation.Lines) != 0 {
				t.Fatalf("denial disclosed graph details: %+v", explanation)
			}
		})
	}

	base.Authorize = func(tenant values.TenantId, purpose string) bool {
		return tenant == g.Tenant && purpose == "access_review"
	}
	explanation, err := g.Explain(base)
	if err != nil {
		t.Fatalf("authorized explanation: %v", err)
	}
	if explanation.CanonicalDigest != g.Digest() || len(explanation.Lines) != 4 {
		t.Fatalf("incomplete explanation: %+v", explanation)
	}
	for _, line := range explanation.Lines {
		if strings.Contains(line, "credential") || strings.Contains(line, "principal") {
			t.Fatalf("workforce explanation crossed into platform identity: %q", line)
		}
	}
}

func TestTodo_ACCESS_001_Conformance(t *testing.T) {
	f := validFixture(t, "a")
	for _, record := range []access.Record{f.identity, f.account, f.entitlement, f.expected, f.observation} {
		if err := record.Validate(); err != nil {
			t.Errorf("%T rejected: %v", record, err)
		}
		if len(record.Canonical()) == 0 {
			t.Errorf("%T has no canonical revision encoding", record)
		}
	}
	if access.AuthorityNative.String() != "NATIVE" || access.AuthorityExternalObservation.String() != "EXTERNAL_OBSERVATION" {
		t.Fatal("authority vocabulary drifted")
	}

	for _, basis := range []struct{ employment, position, policy string }{
		{employment: "employment-a"},
		{position: "position-a"},
		{policy: "policy/a"},
	} {
		edge := f.expected
		edge.EmploymentRef, edge.PositionRef, edge.PolicyRef = basis.employment, basis.position, basis.policy
		if err := edge.Validate(); err != nil {
			t.Errorf("single governed basis %+v rejected: %v", basis, err)
		}
	}

	observation := f.observation
	observation.Authority = access.AuthorityNative
	requireFieldError(t, observation.Validate(), "authority_class", access.ErrAuthorityClass)

	// Compile-time conformance for all explicitly named revision kinds.
	var _ access.Record = access.WorkforceIdentityRevision{}
	var _ access.Record = access.AccountLinkRevision{}
	var _ access.Record = access.EntitlementDefinitionRevision{}
	var _ access.Record = access.ExpectedEntitlementRevision{}
	var _ access.Record = access.ExternalAccessObservationRevision{}
}

func TestTodo_ACCESS_001_Mutation(t *testing.T) {
	base := validGraph(t)
	baseDigest := base.Digest()
	if baseDigest == "" {
		t.Fatal("base graph has no digest")
	}

	mutants := []struct {
		name   string
		mutate func(*access.Graph)
	}{
		{"identity revision", func(g *access.Graph) { g.Identities[0].Revision = mustRevision(t, "access.identity.a", 2) }},
		{"account source", func(g *access.Graph) { g.Accounts[0].Source = "connection/github-dr" }},
		{"entitlement owner", func(g *access.Graph) { g.Entitlements[0].Owner = "owner/security-operations" }},
		{"entitlement risk", func(g *access.Graph) { g.Entitlements[0].RiskClass = access.RiskPrivileged }},
		{"expected basis", func(g *access.Graph) { g.Expected[0].PolicyRef = "policy/birthright/4" }},
		{"lifecycle", func(g *access.Graph) { g.Accounts[0].Lifecycle = access.LifecycleSuspended }},
	}
	for _, tc := range mutants {
		t.Run(tc.name, func(t *testing.T) {
			mutant := base
			mutant.Identities = append([]access.WorkforceIdentity(nil), base.Identities...)
			mutant.Accounts = append([]access.AccountLink(nil), base.Accounts...)
			mutant.Entitlements = append([]access.EntitlementDefinition(nil), base.Entitlements...)
			mutant.Expected = append([]access.ExpectedEntitlement(nil), base.Expected...)
			tc.mutate(&mutant)
			if got := mutant.Digest(); got == baseDigest {
				t.Fatalf("material mutation left digest unchanged: %s", got)
			}
		})
	}

	f := validFixture(t, "a")
	before := base.Digest()
	if err := base.Add(f.observation); !errors.Is(err, access.ErrObservationMutation) {
		t.Fatalf("observation mutation error = %v", err)
	}
	if after := base.Digest(); after != before {
		t.Fatalf("failed Add was not atomic: %s -> %s", before, after)
	}

	injected := base
	injected.Observations = []access.ExternalAccessObservation{f.observation}
	requireFieldError(t, injected.Validate(), "observations", access.ErrObservationMutation)
}
