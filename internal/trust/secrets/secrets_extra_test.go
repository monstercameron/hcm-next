package secrets

import (
	"errors"
	"reflect"
	"testing"
)

func TestSecretReference_ValidateAcceptsClosedSetsAndRejectsMalformedMetadata(t *testing.T) {
	for _, kind := range []Kind{OpaqueSecret, DatabaseCredential, APICredential, OAuthClientSecret, OAuthGrant, SymmetricKey, AsymmetricKeyHandle, SigningKeyHandle, CertificateKeyHandle, ExternalKMSReference} {
		ref := validReference()
		ref.Kind = kind
		if err := ref.Validate(); err != nil {
			t.Fatalf("kind %q rejected: %v", kind, err)
		}
	}
	for _, state := range []State{Requested, Active, Rotating, ActiveNew, Disabled, Destroyed} {
		ref := validReference()
		ref.State = state
		if err := ref.Validate(); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
	for _, mutate := range []func(*SecretReference){
		func(r *SecretReference) { r.ID = "" },
		func(r *SecretReference) { r.Version = "" },
		func(r *SecretReference) { r.Provider = "" },
		func(r *SecretReference) { r.ProviderPath = "" },
		func(r *SecretReference) { r.Tenant = "" },
		func(r *SecretReference) { r.Region = "" },
		func(r *SecretReference) { r.Kind = "unknown" },
		func(r *SecretReference) { r.State = "unknown" },
		func(r *SecretReference) { r.Provider = " vault" },
		func(r *SecretReference) { r.ProviderPath = "vault\npath" },
	} {
		ref := validReference()
		mutate(&ref)
		if !errors.Is(ref.Validate(), ErrInvalidReference) {
			t.Fatalf("malformed reference accepted: %+v", ref)
		}
	}
	for _, assignment := range []string{"?password=x", "&secret=x", ";token=x", "/credential=x", "?api-key=x", "?private_key=x"} {
		ref := validReference()
		ref.ProviderPath = "vault" + assignment
		if !errors.Is(ref.Validate(), ErrInvalidReference) {
			t.Fatalf("credential assignment %q accepted", assignment)
		}
	}
}

type extraScanStruct struct {
	Password  string
	SecretRef string
	Bytes     []byte
	Array     [20]byte
	Values    []string
}

func TestScan_Check_WalkAllSupportedShapesWithoutEchoingValues(t *testing.T) {
	valid := validReference()
	if err := Check(nil); err != nil {
		t.Fatalf("Check(nil) = %v, want nil", err)
	}
	if got := Scan(&struct{ Ref SecretReference }{Ref: valid}); len(got) != 0 {
		t.Fatalf("valid reference scan = %#v", got)
	}
	nilNode := (*extraScanStruct)(nil)
	if got := Scan(nilNode); len(got) != 0 {
		t.Fatalf("nil pointer scan = %#v", got)
	}
	snapshot := struct {
		Password  string
		SecretRef string
		Blob      []byte
		Raw       [20]byte
		Nested    []string
	}{
		Password: "operator-secret", SecretRef: "opaque-locator", Blob: []byte(" sk-test-abcdefghijklmnop"), Nested: []string{"sk-test-abcdefghijklmnop"},
	}
	copy(snapshot.Raw[:], "AKIA1234567890ABCDEF")
	findings := Scan(snapshot)
	if len(findings) != 4 {
		t.Fatalf("shape scan findings = %#v, want four violations", findings)
	}
	if !reflect.DeepEqual(findings[0], Finding{Path: "$.Blob", Code: "secret-shaped_value"}) || !reflect.DeepEqual(findings[1], Finding{Path: "$.Nested[0]", Code: "secret-shaped_value"}) || findings[2].Path != "$.Password" || findings[3].Path != "$.Raw" {
		t.Fatalf("shape scan ordering/codes = %#v", findings)
	}
	for _, finding := range findings {
		if finding.Code == "" || finding.Path == "" || finding.Path == "$.Password.operator-secret" {
			t.Fatalf("finding contained invalid or value-bearing data: %#v", finding)
		}
	}
	if err := Check(snapshot); err == nil || reflect.ValueOf(err).String() == "" {
		t.Fatalf("Check(snapshot) = %v, want redaction error", err)
	}
	if got := Scan(map[int]string{1: "ignored"}); len(got) != 0 {
		t.Fatalf("non-string map key produced findings: %#v", got)
	}
	if got := Scan(struct{ Password string }{}); len(got) != 0 {
		t.Fatalf("empty sensitive field produced findings: %#v", got)
	}
}

func TestScan_ReferenceNamesAndByteArraysUseCorrectDecisionBranches(t *testing.T) {
	if got := Scan(map[string]any{"secret-ref": "sk-test-abcdefghijklmnop"}); len(got) != 1 || got[0].Code != "secret-shaped_value" {
		t.Fatalf("reference-shaped secret value = %#v", got)
	}
	if got := Scan(map[string]any{"secret-ref": "opaque-locator"}); len(got) != 0 {
		t.Fatalf("opaque reference locator rejected: %#v", got)
	}
	if got := Scan(map[string]any{"raw_value": [3]byte{1, 2, 3}}); len(got) != 1 || got[0].Code != "plaintext_secret_field" {
		t.Fatalf("sensitive byte array = %#v", got)
	}
	if got := Scan(map[string]any{"data": [32]byte{'A', 'K', 'I', 'A', '1', '2', '3', '4', '5', '6', '7', '8', '9', '0', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R'}}); len(got) != 1 || got[0].Code != "secret-shaped_value" {
		t.Fatalf("secret-shaped byte array = %#v", got)
	}
	for _, name := range []string{"password", "secret", "token", "credential", "private-key", "api_key", "clientSecret", "rawValue"} {
		if !looksSensitiveField(name) {
			t.Fatalf("looksSensitiveField(%q) = false", name)
		}
	}
	for _, name := range []string{"ref", "locator", "provider_path"} {
		if !looksReferenceField(name) {
			t.Fatalf("looksReferenceField(%q) = false", name)
		}
	}
	if normalizeName("Client-Secret_2") != "clientsecret2" || looksSecretValue("ordinary text") || !looksSecretValue(" ghp-abcdefghijklmnop") {
		t.Fatal("normalization or secret-value recognition branch failed")
	}
}
