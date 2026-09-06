package attest

import (
	"errors"
	"strings"
	"testing"
)

func evidenceExportRequest() ExportRequest {
	return ExportRequest{
		Authorization: EvidenceAuthorization{Tenant: "tenant-a", Purpose: "audit", Recipient: "auditor", RedactionProfile: "minimum/v1", DecisionDigest: "decision-digest", Allowed: true},
		StatementID:   "statement-1", StatementVersion: 2, StatementDigest: "statement-digest", BindingDigest: "binding-digest",
		Response: testResponseRequest(ResponseAccepted).asResponseForTest(),
	}
}

func (r ResponseRequest) asResponseForTest() Response {
	return Response{
		Tenant: r.Tenant, ResponseID: r.ResponseID, Revision: 1, StatementID: r.StatementID, StatementVersion: r.StatementVersion,
		StatementDigest: r.StatementDigest, BindingDigest: r.BindingDigest, Status: r.Status, Kind: AssertionResponse,
		EvidenceReceipt: r.EvidenceReceipt, IdempotencyKey: r.IdempotencyKey, RecordedAt: testTrustedAt, RequestDigest: "request-digest", Digest: "response-digest",
	}
}

func TestExport_ValidationRedactionAndAliases(t *testing.T) {
	valid := evidenceExportRequest()
	pkg, err := Export(valid)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Schema == "" || pkg.TenantDigest == "tenant-a" || pkg.RecipientDigest == "auditor" || pkg.Digest == "" {
		t.Fatalf("export did not create redacted digest-bound package: %+v", pkg)
	}
	if got := pkg.Verify(); !got.Valid || got.Status != "COMPLETE" || got.Digest != pkg.Digest {
		t.Fatalf("valid verification=%+v", got)
	}
	if got := VerifyEvidencePackage(pkg); !got.Valid {
		t.Fatalf("package alias rejected valid package: %+v", got)
	}
	if got := VerifyEvidence(pkg); !got.Valid {
		t.Fatalf("evidence alias rejected valid package: %+v", got)
	}
	if data, err := pkg.Marshal(); err != nil || !strings.Contains(string(data), "statement_digest") {
		t.Fatalf("Marshal data=%q err=%v", data, err)
	}
	if _, err := ExportEvidencePackage(valid); err != nil {
		t.Fatalf("descriptive export alias: %v", err)
	}

	correction := valid.Response
	correction.Kind = AssertionCorrection
	correction.CorrectsResponseID = "old-response"
	correction.Reason = "corrected"
	correction.Authority = "authority"
	revocation := correction
	revocation.Kind = AssertionRevocation
	revocation.CorrectsResponseID = "another-response"
	valid.History = []Response{valid.Response, correction, revocation}
	withHistory, err := Export(valid)
	if err != nil || len(withHistory.Corrections) != 2 {
		t.Fatalf("history export=%+v err=%v", withHistory, err)
	}
	if withHistory.Corrections[0].TargetDigest == withHistory.Corrections[1].TargetDigest {
		t.Fatal("correction lineage was not retained distinctly")
	}
}

func TestExport_RejectsIncompleteAuthorizationAndEvidence(t *testing.T) {
	base := evidenceExportRequest()
	cases := []struct {
		name   string
		mutate func(*ExportRequest)
	}{
		{"not allowed", func(r *ExportRequest) { r.Authorization.Allowed = false }},
		{"tenant missing", func(r *ExportRequest) { r.Authorization.Tenant = "" }},
		{"purpose missing", func(r *ExportRequest) { r.Authorization.Purpose = "" }},
		{"recipient missing", func(r *ExportRequest) { r.Authorization.Recipient = "" }},
		{"redaction profile missing", func(r *ExportRequest) { r.Authorization.RedactionProfile = "" }},
		{"decision missing", func(r *ExportRequest) { r.Authorization.DecisionDigest = "" }},
		{"statement version missing", func(r *ExportRequest) { r.StatementVersion = 0 }},
		{"statement id missing", func(r *ExportRequest) { r.StatementID = "" }},
		{"statement digest missing", func(r *ExportRequest) { r.StatementDigest = "" }},
		{"binding digest missing", func(r *ExportRequest) { r.BindingDigest = "" }},
		{"response status missing", func(r *ExportRequest) { r.Response.Status = "" }},
		{"response digest missing", func(r *ExportRequest) { r.Response.Digest = "" }},
		{"response time missing", func(r *ExportRequest) { r.Response.RecordedAt = TrustedTime{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			if _, err := Export(r); err == nil || !errors.Is(err, ErrEvidencePackage) {
				t.Fatalf("Export err=%v, want ErrEvidencePackage", err)
			}
		})
	}
}

func TestEvidencePackage_VerifyAndMarshalRejectsFirstSecurityFailure(t *testing.T) {
	pkg, err := Export(evidenceExportRequest())
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*EvidencePackage)
		want   string
	}{
		{"schema", func(p *EvidencePackage) { p.Schema = "wrong" }, "schema"},
		{"tenant", func(p *EvidencePackage) { p.TenantDigest = "" }, "tenant_digest"},
		{"purpose", func(p *EvidencePackage) { p.Purpose = "" }, "purpose"},
		{"recipient", func(p *EvidencePackage) { p.RecipientDigest = "" }, "recipient_digest"},
		{"profile", func(p *EvidencePackage) { p.RedactionProfile = "" }, "redaction_profile"},
		{"authorization", func(p *EvidencePackage) { p.AuthorizationDigest = "" }, "authorization_digest"},
		{"version", func(p *EvidencePackage) { p.StatementVersion = 0 }, "statement_version"},
		{"statement", func(p *EvidencePackage) { p.StatementDigest = "" }, "statement_digest"},
		{"binding", func(p *EvidencePackage) { p.BindingDigest = "" }, "binding_digest"},
		{"response", func(p *EvidencePackage) { p.ResponseDigest = "" }, "response_digest"},
		{"status", func(p *EvidencePackage) { p.ResponseStatus = "BAD" }, "response_status"},
		{"kind", func(p *EvidencePackage) { p.ResponseKind = "BAD" }, "response_kind"},
		{"time", func(p *EvidencePackage) { p.TimeEvidenceID = "" }, "time"},
		{"nul purpose", func(p *EvidencePackage) { p.Purpose = "audit\x00secret" }, "purpose"},
		{"digest", func(p *EvidencePackage) { p.Digest = "wrong" }, "digest"},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			copy := pkg
			tc.mutate(&copy)
			got := copy.Verify()
			if got.Valid || got.Status != "ATTEST_007_REJECTED" || got.Invalid != tc.want {
				t.Fatalf("verification=%+v, want invalid=%q", got, tc.want)
			}
			if _, err := copy.Marshal(); err == nil || !strings.Contains(err.Error(), "ATTEST_007_REJECTED") {
				t.Fatalf("Marshal err=%v, want verification refusal", err)
			}
		})
	}
	withBadLink := pkg
	withBadLink.Corrections = []EvidenceLink{{Kind: "BAD", TargetDigest: "target", ReasonDigest: "reason", AuthorityDigest: "authority"}}
	withBadLink.Digest = withBadLink.ContentDigest()
	if got := withBadLink.Verify(); got.Valid || got.Invalid != "corrections[0].kind" {
		t.Fatalf("bad correction kind verification=%+v", got)
	}
	withEmptyLink := pkg
	withEmptyLink.Corrections = []EvidenceLink{{Kind: string(AssertionCorrection)}}
	withEmptyLink.Digest = withEmptyLink.ContentDigest()
	if got := withEmptyLink.Verify(); got.Valid || got.Invalid != "corrections[0]" {
		t.Fatalf("bad correction fields verification=%+v", got)
	}
}
