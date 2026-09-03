package evidence

import "testing"

func completePackage() Package {
	return Package{
		Issuer: "documents/v1", Freshness: "2026-09-03T12:00:00Z", Signature: "receipt:sig-1",
		Classification: "CONFIDENTIAL", Purpose: "employment-agreement", Retention: "records:7y",
		Hold: "NONE", Method: "provider-receipt", Schema: "document-evidence/v1",
		Rule: "records-signature/v2", Redaction: "none", VerifierAuthority: "records-verifier/v1",
		Items: []EvidenceItem{{ID: "doc-1", Kind: "RENDERED_DOCUMENT", Digest: "sha256:document", Sequence: 1}},
	}
}

func TestTodo_DOC_EVIDENCE_001(t *testing.T) {
	p, err := New(completePackage())
	if err != nil {
		t.Fatal(err)
	}
	got := p.VerifyCompleteness()
	if got.Status != Complete || got.Digest != p.Digest {
		t.Fatalf("verification = %+v, want COMPLETE and embedded digest", got)
	}
	if len(got.Missing) != 0 || len(got.Invalid) != 0 {
		t.Fatalf("unexpected repair hints: %+v", got)
	}
}

func TestTodo_DOC_EVIDENCE_001_Golden(t *testing.T) {
	p := completePackage()
	first, second := p.ContentDigest(), p.ContentDigest()
	if first == "" || first != second {
		t.Fatalf("canonical digest is not stable: %q != %q", first, second)
	}
}

func TestTodo_DOC_EVIDENCE_001_Integration(t *testing.T) {
	p := completePackage()
	p.Issuer = ""
	got := p.Verify()
	if got.Status != Partial || len(got.Missing) != 1 || got.Missing[0] != "issuer" {
		t.Fatalf("verification = %+v, want issuer-only PARTIAL", got)
	}
}

func TestTodo_DOC_EVIDENCE_001_Unknown(t *testing.T) {
	if got := (Package{}).Verify(); got.Status != Unknown {
		t.Fatalf("empty package status = %s, want UNKNOWN", got.Status)
	}
}

func TestTodo_DOC_EVIDENCE_001_Mutation(t *testing.T) {
	p, err := New(completePackage())
	if err != nil {
		t.Fatal(err)
	}
	p.Items[0].Digest = "tampered"
	got := p.Verify()
	if got.Status != Rejected || len(got.Invalid) != 1 || got.Invalid[0] != "digest" {
		t.Fatalf("verification = %+v, want digest REJECTED", got)
	}
}

func TestTodo_DOC_EVIDENCE_001_Security(t *testing.T) {
	p := completePackage()
	p.Items = []EvidenceItem{{Kind: "RENDERED_DOCUMENT", Digest: "sha256:x"}}
	got := p.Verify()
	if got.Status != Rejected {
		t.Fatalf("verification = %+v, want malformed item REJECTED", got)
	}
}
