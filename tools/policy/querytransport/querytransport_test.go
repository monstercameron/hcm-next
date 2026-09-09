package querytransport_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/querytransport"
)

func validEnvelope() productquery.Envelope {
	return productquery.Envelope{
		ContractVersion: productquery.Version(),
		Tenant:          "acme",
		Purpose:         "compensation_review",
		PolicyVersions:  []string{"authz.p1a.bootstrap.v1"},
		Projection: productquery.Projection{
			Name: "worker_summary", DefinitionVersion: "worker-summary.v3", SchemaVersion: "schema.v5",
			SourceSequence: 12, Watermark: 10,
		},
		Freshness: productquery.FreshnessCurrent,
	}
}

func TestValidateParityAcceptsEquivalentSurfaces(t *testing.T) {
	envelope := validEnvelope()
	findings := querytransport.ValidateParity([]querytransport.Observation{
		{Surface: "grpc", Envelope: envelope},
		{Surface: "http", Envelope: envelope},
		{Surface: "ssr", Envelope: envelope},
	})
	if len(findings) != 0 {
		t.Fatalf("equivalent surfaces produced findings: %+v", findings)
	}
}

func TestValidateParityRejectsSemanticDrift(t *testing.T) {
	first := validEnvelope()
	second := first
	second.Purpose = "audit_review"
	findings := querytransport.ValidateParity([]querytransport.Observation{
		{Surface: "grpc", Envelope: first},
		{Surface: "http", Envelope: second},
	})
	if len(findings) != 1 || findings[0].Code != "SEMANTIC_DIGEST_MISMATCH" {
		t.Fatalf("semantic drift findings = %+v", findings)
	}
}

func TestValidateInvalidationAcceptsBoundedMessage(t *testing.T) {
	message := productquery.InvalidationMessage{
		ContractVersion: productquery.Version(), Tenant: "acme", Projection: "worker_summary",
		SourceSequence: 3, Watermark: 2,
		Items: []productquery.InvalidationItem{{
			Subject:  productquerySubject("acme", "00000000-0000-4000-8000-000000000001"),
			Revision: 4,
		}},
	}
	if findings := querytransport.ValidateInvalidation(message); len(findings) != 0 {
		t.Fatalf("valid invalidation produced findings: %+v", findings)
	}
}

func TestValidateInvalidationRejectsForeignAndUnsortedItems(t *testing.T) {
	message := productquery.InvalidationMessage{
		ContractVersion: productquery.Version(), Tenant: "acme", Projection: "worker_summary",
		Items: []productquery.InvalidationItem{
			{Subject: productquerySubject("other", "00000000-0000-4000-8000-000000000002"), Revision: 1},
			{Subject: productquerySubject("acme", "00000000-0000-4000-8000-000000000001")},
		},
	}
	findings := querytransport.ValidateInvalidation(message)
	if len(findings) < 2 {
		t.Fatalf("invalid invalidation findings = %+v", findings)
	}
}

func TestPolicyVersionAndExplanation(t *testing.T) {
	if querytransport.Version() != 1 || (querytransport.Report{}).Explain() == "" {
		t.Fatal("policy contract shape is incomplete")
	}
}

func productquerySubject(tenant, id string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind("worker"), Id: id}
}
