package authz_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// baseTime is the fixed instant every test treats as "now". Nothing in this
// package reads the wall clock, so every test here is deterministic.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func instantAt(t time.Time) values.Instant { return values.NewInstant(t) }

var (
	baseInstant  = instantAt(baseTime)
	farPast      = instantAt(baseTime.Add(-365 * 24 * time.Hour))
	farFuture    = instantAt(baseTime.Add(365 * 24 * time.Hour))
	recentPast   = instantAt(baseTime.Add(-time.Hour))
	shortlyAfter = instantAt(baseTime.Add(time.Hour))
)

// Fixed canonical tenants and subjects used across the suite.
const (
	tenantAcme   = values.TenantId("acme-corp")
	tenantVendor = values.TenantId("vendor-corp")

	subjectWorkerID = "00000000-0000-4000-8000-000000000001"
	subjectOtherID  = "00000000-0000-4000-8000-000000000002"
	principalSelfID = subjectWorkerID
	kindWorker      = values.Kind("worker")
)

func mustOpenInterval(t *testing.T, start values.Instant) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	return iv
}

func mustInterval(t *testing.T, start, end values.Instant) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatalf("NewInstantInterval: %v", err)
	}
	return iv
}

func workerSubject(tenant values.TenantId, id string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: kindWorker, Id: id}
}

// principalOpts configures a fixture principal beyond the fields every test
// fixture shares.
type principalOpts struct {
	tenant    values.TenantId
	subject   string
	roles     []string
	purposes  []string
	assurance trust.Assurance
}

func newPrincipal(t *testing.T, opts principalOpts) *trust.Principal {
	t.Helper()
	if opts.tenant == "" {
		opts.tenant = tenantAcme
	}
	if opts.subject == "" {
		opts.subject = subjectWorkerID
	}
	if opts.assurance == trust.AssuranceUnspecified {
		opts.assurance = trust.AssuranceSubstantial
	}
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               opts.tenant,
		Subject:              opts.subject,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                opts.roles,
		Purposes:             opts.purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            opts.assurance,
		SessionRef:           "session-" + opts.subject,
		IssuedAt:             baseTime.Add(-time.Hour),
		ExpiresAt:            baseTime.Add(time.Hour),
		CredentialDigest:     "digest-" + opts.subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// managerFact returns a valid manager-chain relationship fact over subject,
// attributed to a source and active across the fixture's time window.
func managerFact(subject values.EntityRef) authz.RelationshipFact {
	return authz.RelationshipFact{
		Kind:       authz.RelationshipManagerChain,
		Subject:    subject,
		Source:     "organization.manager_chain.v3",
		Effective:  mustOpenIntervalUnchecked(recentPast),
		RecordedAt: mustRecordedAt(recentPast),
		KnownAt:    mustKnownAt(recentPast),
	}
}

func hrPartnerFact(subject values.EntityRef) authz.RelationshipFact {
	return authz.RelationshipFact{
		Kind:       authz.RelationshipHRPartner,
		Subject:    subject,
		Source:     "organization.hr_partner_scope.v1",
		Effective:  mustOpenIntervalUnchecked(recentPast),
		RecordedAt: mustRecordedAt(recentPast),
		KnownAt:    mustKnownAt(recentPast),
	}
}

func assignedPopulationFact(subject values.EntityRef) authz.RelationshipFact {
	return authz.RelationshipFact{
		Kind:       authz.RelationshipAssignedPopulation,
		Subject:    subject,
		Source:     "population.assignment.v1",
		Effective:  mustOpenIntervalUnchecked(recentPast),
		RecordedAt: mustRecordedAt(recentPast),
		KnownAt:    mustKnownAt(recentPast),
	}
}

// mustOpenIntervalUnchecked panics on error. It exists so fixture builders
// that cannot take a *testing.T (they are used from Fuzz targets too) can
// still build intervals from fixed, known-valid instants.
func mustOpenIntervalUnchecked(start values.Instant) values.EffectiveInterval {
	iv, err := values.NewOpenInstantInterval(start)
	if err != nil {
		panic(err)
	}
	return iv
}

func mustRecordedAt(at values.Instant) values.RecordedAt {
	r, err := values.NewRecordedAt(at)
	if err != nil {
		panic(err)
	}
	return r
}

func mustKnownAt(at values.Instant) values.KnownAt {
	k, err := values.NewKnownAt(at)
	if err != nil {
		panic(err)
	}
	return k
}
