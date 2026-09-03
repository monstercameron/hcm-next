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

func TestTodo_TRUST_015_SensitiveBytesAndStableFindings(t *testing.T) {
	snapshot := map[string]any{
		"z_token":    []byte("operator-secret"),
		"a_password": []byte("another-secret"),
	}
	findings := Scan(snapshot)
	if len(findings) != 2 {
		t.Fatalf("sensitive bytes were not rejected: %#v", findings)
	}
	if findings[0].Path != "$.a_password" || findings[1].Path != "$.z_token" {
		t.Fatalf("findings are not stable by path: %#v", findings)
	}
	if findings[0].Code != "plaintext_secret_field" || findings[1].Code != "plaintext_secret_field" {
		t.Fatalf("unexpected finding codes: %#v", findings)
	}
}

func TestTodo_TRUST_015_CyclicSnapshotsTerminate(t *testing.T) {
	type node struct {
		Next  *node
		Token string
	}
	n := &node{Token: "operator-secret"}
	n.Next = n
	findings := Scan(n)
	if len(findings) != 1 || findings[0].Path != "$.Token" {
		t.Fatalf("unexpected cyclic scan findings: %#v", findings)
	}
}

func TestTodo_TRUST_015_ReferenceFieldStillRequiresReferenceShape(t *testing.T) {
	// A reference-shaped name is allowed to carry an opaque locator, but a
	// recognizable credential value must still be rejected.
	findings := Scan(map[string]any{"secret-ref": "sk-test-abcdefghijklmnop"})
	if len(findings) != 1 || findings[0].Path != "$.secret-ref" || findings[0].Code != "secret-shaped_value" {
		t.Fatalf("credential value hidden by reference name: %#v", findings)
	}
}

func FuzzTodo_TRUST_015(f *testing.F) {
	f.Add("operator-secret", "token")
	f.Add("sk-test-abcdefghijklmnop", "credential")
	f.Fuzz(func(t *testing.T, value, field string) {
		findings := Scan(map[string]any{field: value})
		for _, finding := range findings {
			if finding.Code != "plaintext_secret_field" && finding.Code != "secret-shaped_value" && finding.Code != "invalid_reference" {
				t.Fatalf("unexpected finding code: %#v", finding)
			}
		}
	})
}

func TestTodo_TRUST_015_Integration(t *testing.T) {
	ref := validReference()
	snapshot := map[string]any{"connection": map[string]any{"secret_ref": ref}}
	if err := Check(snapshot); err != nil {
		t.Fatalf("reference-only connection snapshot rejected: %v", err)
	}
}

func TestTodo_TRUST_015_Security(t *testing.T) {
	secret := "sk-test-abcdefghijklmnop"
	err := Check(map[string]any{"api_token": secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret was accepted or disclosed: %v", err)
	}
}

func TestTodo_TRUST_015_Mutation(t *testing.T) {
	for _, field := range []string{"password", "client_secret", "private-key", "rawValue"} {
		if err := Check(map[string]any{field: []byte("opaque-value")}); err == nil {
			t.Fatalf("raw bytes accepted for %q", field)
		}
	}
}
