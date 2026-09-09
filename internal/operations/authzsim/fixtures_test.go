package authzsim_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// baseTime is the fixed instant every test treats as "now".
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func instantAt(t time.Time) values.Instant { return values.NewInstant(t) }

var (
	baseInstant = instantAt(baseTime)
	recentPast  = instantAt(baseTime.Add(-time.Hour))
)

const (
	tenantAcme = values.TenantId("acme-corp")

	subjectWorkerID = "00000000-0000-4000-8000-000000000001"
	subjectOtherID  = "00000000-0000-4000-8000-000000000002"
	kindWorker      = values.Kind("worker")
)

func workerSubject(tenant values.TenantId, id string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: kindWorker, Id: id}
}

type principalOpts struct {
	subject  string
	roles    []string
	purposes []string
}

func newPrincipal(t *testing.T, opts principalOpts) *trust.Principal {
	t.Helper()
	subject := opts.subject
	if subject == "" {
		subject = subjectWorkerID
	}
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               tenantAcme,
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                opts.roles,
		Purposes:             opts.purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceSubstantial,
		SessionRef:           "session-" + subject,
		IssuedAt:             baseTime.Add(-time.Hour),
		ExpiresAt:            baseTime.Add(time.Hour),
		CredentialDigest:     "digest-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func managerFact(subject values.EntityRef) authz.RelationshipFact {
	iv, err := values.NewOpenInstantInterval(recentPast)
	if err != nil {
		panic(err)
	}
	recorded, err := values.NewRecordedAt(recentPast)
	if err != nil {
		panic(err)
	}
	known, err := values.NewKnownAt(recentPast)
	if err != nil {
		panic(err)
	}
	return authz.RelationshipFact{
		Kind:       authz.RelationshipManagerChain,
		Subject:    subject,
		Source:     "organization.manager_chain.v3",
		Effective:  iv,
		RecordedAt: recorded,
		KnownAt:    known,
	}
}

// managerRequest returns a manager reading a direct report's compensation
// and case-notes fields under the compensation-review purpose: base salary
// is granted, case notes is denied by the P1A bootstrap policy's default
// deny.
func managerRequest(t *testing.T) authz.Request {
	t.Helper()
	subject := workerSubject(tenantAcme, subjectOtherID)
	principal := newPrincipal(t, principalOpts{roles: []string{string(authz.RoleManager)}, purposes: []string{authz.PurposeCompensationReview}})
	return authz.Request{
		Principal:     principal,
		Purpose:       authz.PurposeCompensationReview,
		EffectiveAt:   baseInstant,
		Subject:       subject,
		Relationships: []authz.RelationshipFact{managerFact(subject)},
		Fields:        []authz.FieldID{authz.FieldWorkerNumber, authz.FieldBaseSalary, authz.FieldCaseNotes},
	}
}
