package secrets

import (
	"strings"
	"testing"
)

func validReference() SecretReference {
	return SecretReference{ID: "sec_123", Kind: APICredential, Version: "v3", Provider: "vault", ProviderPath: "kv/tenant-a/payroll", Tenant: "tenant-a", Region: "us-east", State: Active}
}

func TestTodo_TRUST_015(t *testing.T) {
	ref := validReference()
	if err := ref.Validate(); err != nil {
		t.Fatalf("valid reference rejected: %v", err)
	}
	if err := Check(map[string]any{"connector_secret_ref": ref, "purpose": "sync"}); err != nil {
		t.Fatalf("reference snapshot rejected: %v", err)
	}
	for _, snapshot := range []any{
		map[string]any{"password": "do-not-store"},
		map[string]any{"signing_key": "-----BEGIN PRIVATE KEY-----"},
		struct {
			ClientSecret string `json:"client_secret"`
		}{ClientSecret: "do-not-store"},
	} {
		if err := Check(snapshot); err == nil {
			t.Fatalf("raw secret snapshot accepted: %#v", snapshot)
		}
	}
}

func TestTodo_TRUST_015_Golden(t *testing.T) {
	findings := Scan(map[string]any{"credentials": map[string]any{"api_key": "sk-test-abcdefghijklmnop"}})
	if len(findings) != 1 || findings[0].Path != "$.credentials.api_key" || findings[0].Code != "plaintext_secret_field" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
	if strings.Contains(Check(map[string]any{"token": "super-secret-value"}).Error(), "super-secret-value") {
		t.Fatal("checker echoed secret value")
	}
}

func TestTodo_TRUST_015_InvalidReference(t *testing.T) {
	r := validReference()
	r.ProviderPath = "vault?password=leaked"
	if err := r.Validate(); err == nil {
		t.Fatal("credential-bearing provider path accepted")
	}
	if got := Scan(r); len(got) != 1 || got[0].Code != "invalid_reference" {
		t.Fatalf("invalid reference was not reported: %#v", got)
	}
}
