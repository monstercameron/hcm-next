package conformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport/conformance"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

const (
	tenantID    = values.TenantId("acme")
	principalID = "00000000-0000-4000-8000-000000000001"
	managedID   = "00000000-0000-4000-8000-000000000002"
	hiddenID    = "00000000-0000-4000-8000-000000000003"
	foreignID   = "00000000-0000-4000-8000-000000000004"
)

var queryInstant = values.NewInstant(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))

func newPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               tenantID,
		Subject:              principalID,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{string(authz.RoleManager)},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceSubstantial,
		SessionRef:           "session-query-parity",
		IssuedAt:             queryInstant.Time().Add(-time.Hour),
		ExpiresAt:            queryInstant.Time().Add(time.Hour),
		CredentialDigest:     "credential-query-parity",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func subject(id string) values.EntityRef {
	return values.EntityRef{Tenant: tenantID, Kind: values.Kind("worker"), Id: id}
}

func foreignSubject() values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId("vendor"), Kind: values.Kind("worker"), Id: foreignID}
}

func managedFact(t *testing.T, s values.EntityRef) authz.RelationshipFact {
	t.Helper()
	interval, err := values.NewOpenInstantInterval(values.NewInstant(queryInstant.Time().Add(-time.Hour)))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(queryInstant.Time().Add(-time.Minute)))
	if err != nil {
		t.Fatalf("NewRecordedAt: %v", err)
	}
	known, err := values.NewKnownAt(values.NewInstant(queryInstant.Time().Add(-2 * time.Minute)))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return authz.RelationshipFact{
		Kind:       authz.RelationshipManagerChain,
		Subject:    s,
		Source:     "organization.manager_chain.v3",
		Effective:  interval,
		RecordedAt: recorded,
		KnownAt:    known,
	}
}

func request(t *testing.T, includeHidden bool, reverse bool) conformance.QueryRequest {
	t.Helper()
	p := newPrincipal(t)
	managed := subject(managedID)
	hidden := subject(hiddenID)
	candidates := []authz.ScopeInput{{
		Subject:       managed,
		EffectiveAt:   queryInstant,
		Relationships: []authz.RelationshipFact{managedFact(t, managed)},
	}}
	requested := []values.EntityRef{managed}
	if includeHidden {
		candidates = append(candidates, authz.ScopeInput{Subject: hidden, EffectiveAt: queryInstant})
		requested = append(requested, hidden)
	}
	if reverse {
		for left, right := 0, len(candidates)-1; left < right; left, right = left+1, right-1 {
			candidates[left], candidates[right] = candidates[right], candidates[left]
			requested[left], requested[right] = requested[right], requested[left]
		}
	}
	scope, err := authz.PlanRepositoryScope(authz.RepositoryQueryRequest{
		Principal:   p,
		Purpose:     authz.PurposeCompensationReview,
		EffectiveAt: queryInstant,
		Tenant:      tenantID,
		Candidates:  candidates,
		Fields:      []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary},
	})
	if err != nil {
		t.Fatalf("PlanRepositoryScope: %v", err)
	}
	records := map[values.EntityRef]map[authz.FieldID]string{
		managed: {
			authz.FieldWorkerNumber: "W-0002",
			authz.FieldBaseSalary:   "100000.00",
		},
		hidden: {
			authz.FieldWorkerNumber: "W-0003",
			authz.FieldBaseSalary:   "90000.00",
		},
	}
	return conformance.QueryRequest{
		Tenant:            tenantID,
		Purpose:           authz.PurposeCompensationReview,
		EffectiveAt:       queryInstant,
		Scope:             scope,
		Gate:              authz.NewRepositoryGate(records),
		Requested:         requested,
		SchemaVersion:     "schema.worker.v3",
		DefinitionVersion: "page.people.v2",
		Freshness: conformance.Freshness{
			SourceVersion:      "worker-source.v7",
			ProjectionVersion:  "worker-projection.v7",
			SourceSequence:     42,
			ProjectionSequence: 42,
			ObservedAt:         queryInstant,
		},
	}
}

func execute(t *testing.T, includeHidden bool) conformance.QueryEnvelope {
	t.Helper()
	envelope, err := conformance.Execute(context.Background(), request(t, includeHidden, false))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return envelope
}

func TestTodo_ALIGN_058(t *testing.T) {
	envelope := execute(t, true)
	if envelope.ContractVersion != conformance.Version() || envelope.SemanticDigest == "" {
		t.Fatalf("contract=%d digest=%q", envelope.ContractVersion, envelope.SemanticDigest)
	}
	if envelope.Freshness.State() != conformance.FreshnessFresh {
		t.Fatalf("freshness = %s, want FRESH", envelope.Freshness.State())
	}
	if len(envelope.Rows) != 1 || envelope.Rows[0].Subject != subject(managedID) {
		t.Fatalf("rows = %+v, want only the authorized managed subject", envelope.Rows)
	}
	var surfaces []conformance.SurfaceProjection
	for _, surface := range []conformance.Surface{
		conformance.SurfaceSSR, conformance.SurfaceBrowser, conformance.SurfaceRPC, conformance.SurfaceExport,
	} {
		projection, err := conformance.ProjectSurface(surface, envelope)
		if err != nil {
			t.Fatalf("ProjectSurface(%s): %v", surface, err)
		}
		surfaces = append(surfaces, projection)
	}
	if err := conformance.CheckNoninterference(surfaces, []values.EntityRef{subject(hiddenID)}); err != nil {
		t.Fatalf("CheckNoninterference: %v", err)
	}
}

func TestTodo_ALIGN_058_Property(t *testing.T) {
	first := execute(t, true)
	reversed, err := conformance.Execute(context.Background(), request(t, true, true))
	if err != nil {
		t.Fatalf("Execute(reversed): %v", err)
	}
	if first.SemanticDigest != reversed.SemanticDigest {
		t.Fatalf("reordering a bounded query changed semantic digest: %s != %s", first.SemanticDigest, reversed.SemanticDigest)
	}
}

func TestTodo_ALIGN_058_Golden(t *testing.T) {
	envelope := execute(t, false)
	want := envelope.CanonicalDigest()
	if envelope.SemanticDigest != want {
		t.Fatalf("semantic digest = %q, want canonical digest %q", envelope.SemanticDigest, want)
	}
	if got := envelope.Authorization.PolicyVersion; got != authz.PolicyVersion {
		t.Fatalf("policy version = %q, want %q", got, authz.PolicyVersion)
	}
}

func TestTodo_ALIGN_058_Security(t *testing.T) {
	envelope := execute(t, true)
	serialized := envelope.Authorization.Reason + " " + envelope.SemanticDigest
	if strings.Contains(serialized, hiddenID) || strings.Contains(serialized, "90000.00") {
		t.Fatalf("authorization metadata leaked hidden data: %q", serialized)
	}
	for _, row := range envelope.Rows {
		for _, field := range row.Fields {
			if field.Effect == authz.EffectWithheld {
				t.Fatal("a withheld field crossed the product-query envelope")
			}
		}
	}
}

func TestTodo_ALIGN_058_Integration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := conformance.Execute(ctx, request(t, false, false)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled) = %v, want context.Canceled", err)
	}
}

func TestTodo_ALIGN_058_Fault(t *testing.T) {
	bad := request(t, false, false)
	bad.Scope = authz.RepositoryScope{}
	if _, err := conformance.Execute(context.Background(), bad); !errors.Is(err, conformance.ErrInvalidQuery) {
		t.Fatalf("Execute(zero scope) = %v, want ErrInvalidQuery", err)
	}
	stale := request(t, false, false)
	stale.Freshness.ProjectionSequence = 41
	envelope, err := conformance.Execute(context.Background(), stale)
	if err != nil {
		t.Fatalf("Execute(stale projection): %v", err)
	}
	if envelope.Freshness.State() != conformance.FreshnessStale {
		t.Fatalf("freshness = %s, want STALE", envelope.Freshness.State())
	}
}

func TestTodo_ALIGN_058_Conformance(t *testing.T) {
	envelope := execute(t, false)
	left, err := conformance.ProjectSurface(conformance.SurfaceSSR, envelope)
	if err != nil {
		t.Fatal(err)
	}
	right, err := conformance.ProjectSurface(conformance.SurfaceRPC, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := conformance.AssertParity(left, right); err != nil {
		t.Fatalf("SSR/RPC parity: %v", err)
	}
	left.SemanticDigest = "sha256:tampered"
	if err := conformance.AssertParity(left, right); !errors.Is(err, conformance.ErrParityMismatch) {
		t.Fatalf("tampered parity = %v, want ErrParityMismatch", err)
	}
}

func TestTodo_ALIGN_059(t *testing.T) {
	envelope := execute(t, true)
	projection, err := conformance.ProjectSurface(conformance.SurfaceSSR, envelope)
	if err != nil {
		t.Fatal(err)
	}
	invalidation, err := conformance.AuthorizeInvalidation(
		executeScope(t), queryInstant, "worker-projection.v7", 43,
		[]values.EntityRef{subject(managedID), subject(hiddenID)},
	)
	if err != nil {
		t.Fatalf("AuthorizeInvalidation: %v", err)
	}
	if len(invalidation.Subjects) != 1 || invalidation.Subjects[0] != subject(managedID) {
		t.Fatalf("invalidation subjects = %v, want only the managed subject", invalidation.Subjects)
	}
	if err := conformance.CheckNoninterference([]conformance.SurfaceProjection{projection, projection}, []values.EntityRef{subject(hiddenID)}); err != nil {
		t.Fatalf("cross-surface noninterference: %v", err)
	}
}

func executeScope(t *testing.T) authz.RepositoryScope {
	t.Helper()
	return request(t, true, false).Scope
}

func TestTodo_ALIGN_059_Property(t *testing.T) {
	base := execute(t, false)
	withHidden := execute(t, true)
	if base.SemanticDigest != withHidden.SemanticDigest {
		t.Fatalf("adding an unauthorized candidate changed semantic digest: %s != %s", base.SemanticDigest, withHidden.SemanticDigest)
	}
}

func TestTodo_ALIGN_059_Golden(t *testing.T) {
	message, err := conformance.AuthorizeInvalidation(executeScope(t), queryInstant, "worker-projection.v7", 43, []values.EntityRef{subject(hiddenID), subject(managedID)})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(message.Canonical()); !strings.Contains(got, "subjects=1") || strings.Contains(got, hiddenID) {
		t.Fatalf("canonical invalidation = %q, want one authorized subject and no hidden identifier", got)
	}
	if message.Explain() == "" {
		t.Fatal("invalidation explanation is empty")
	}
}

func TestTodo_ALIGN_059_Security(t *testing.T) {
	message, err := conformance.AuthorizeInvalidation(executeScope(t), queryInstant, "worker-projection.v7", 43, []values.EntityRef{subject(managedID), subject(hiddenID)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(message.Explain(), hiddenID) || strings.Contains(string(message.Canonical()), "90000.00") {
		t.Fatalf("invalidation leaked protected data: %s", message.Explain())
	}
}

func TestTodo_ALIGN_059_Integration(t *testing.T) {
	message, err := conformance.AuthorizeInvalidation(executeScope(t), queryInstant, "worker-projection.v7", 44, []values.EntityRef{subject(managedID)})
	if err != nil {
		t.Fatal(err)
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(message.Canonical()) > conformance.MaxInvalidationBytes {
		t.Fatalf("canonical invalidation length = %d, exceeds bound", len(message.Canonical()))
	}
}

func TestTodo_ALIGN_059_Fault(t *testing.T) {
	if _, err := conformance.AuthorizeInvalidation(executeScope(t), queryInstant, "worker-projection.v7", 43, []values.EntityRef{foreignSubject()}); !errors.Is(err, conformance.ErrInvalidationRejected) {
		t.Fatalf("foreign invalidation = %v, want ErrInvalidationRejected", err)
	}
	tooMany := make([]values.EntityRef, conformance.MaxInvalidationSubjects+1)
	for i := range tooMany {
		tooMany[i] = subject(managedID)
	}
	if _, err := conformance.AuthorizeInvalidation(executeScope(t), queryInstant, "worker-projection.v7", 43, tooMany); !errors.Is(err, conformance.ErrInvalidationRejected) {
		t.Fatalf("oversized invalidation = %v, want ErrInvalidationRejected", err)
	}
}

func TestTodo_ALIGN_059_Conformance(t *testing.T) {
	envelope := execute(t, true)
	projections := make([]conformance.SurfaceProjection, 0, 3)
	for _, surface := range []conformance.Surface{conformance.SurfaceSSR, conformance.SurfaceRPC, conformance.SurfaceExport} {
		projection, err := conformance.ProjectSurface(surface, envelope)
		if err != nil {
			t.Fatal(err)
		}
		projections = append(projections, projection)
	}
	if err := conformance.CheckNoninterference(projections, []values.EntityRef{subject(hiddenID)}); err != nil {
		t.Fatalf("CheckNoninterference: %v", err)
	}
}
