package identitybinding

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func findingCodes(findings []Finding) map[string]bool {
	codes := make(map[string]bool, len(findings))
	for _, finding := range findings {
		codes[finding.Code] = true
	}
	return codes
}

func TestIdentityBinding_MetadataAndValidationBoundaries(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version() = %d, want 1", Version())
	}
	if !strings.Contains(Explain(), "reference-only") && !strings.Contains(Explain(), "without raw values") {
		t.Fatalf("Explain() = %q, want reference-only contract language", Explain())
	}
	base := validBindingPlan()
	tests := []struct {
		name   string
		mutate func(*Plan)
		code   string
	}{
		{name: "identity id", mutate: func(p *Plan) { p.Identity.ID = "" }, code: "IDENTITY_REQUIRED"},
		{name: "workload", mutate: func(p *Plan) { p.Identity.Workload = "" }, code: "WORKLOAD_REQUIRED"},
		{name: "cell", mutate: func(p *Plan) { p.Identity.CellID = "" }, code: "CELL_REQUIRED"},
		{name: "service", mutate: func(p *Plan) { p.Identity.Service = "" }, code: "SERVICE_REQUIRED"},
		{name: "destination", mutate: func(p *Plan) { p.Identity.Destination = "" }, code: "DESTINATION_REQUIRED"},
		{name: "shared", mutate: func(p *Plan) { p.Identity.Shared = true }, code: "SHARED_IDENTITY"},
		{name: "identity expired", mutate: func(p *Plan) { p.Identity.ExpiresAt = p.EvaluatedAt }, code: "IDENTITY_EXPIRED"},
		{name: "secret required", mutate: func(p *Plan) { p.Secrets = nil }, code: "SECRET_LEASE_REQUIRED"},
		{name: "secret reference", mutate: func(p *Plan) { p.Secrets[0].Name = "" }, code: "SECRET_REFERENCE_REQUIRED"},
		{name: "secret audience", mutate: func(p *Plan) { p.Secrets[0].Audience = "other" }, code: "SECRET_AUDIENCE_REQUIRED"},
		{name: "raw secret", mutate: func(p *Plan) { p.Secrets[0].RawValue = "secret" }, code: "RAW_SECRET_FORBIDDEN"},
		{name: "secret expiry", mutate: func(p *Plan) { p.Secrets[0].ExpiresAt = p.EvaluatedAt }, code: "SECRET_LEASE_EXPIRED"},
		{name: "certificate required", mutate: func(p *Plan) { p.Certificates = nil }, code: "CERTIFICATE_REQUIRED"},
		{name: "certificate reference", mutate: func(p *Plan) { p.Certificates[0].Reference = "" }, code: "CERTIFICATE_REFERENCE_REQUIRED"},
		{name: "raw certificate", mutate: func(p *Plan) { p.Certificates[0].ReferenceOnly = false }, code: "RAW_CERTIFICATE_FORBIDDEN"},
		{name: "certificate destination", mutate: func(p *Plan) { p.Certificates[0].Destination = "other" }, code: "CERTIFICATE_DESTINATION_MISMATCH"},
		{name: "fingerprint", mutate: func(p *Plan) { p.Certificates[0].Fingerprint = "sha256:bad" }, code: "CERTIFICATE_FINGERPRINT_REQUIRED"},
		{name: "certificate expiry", mutate: func(p *Plan) { p.Certificates[0].NotAfter = p.EvaluatedAt }, code: "CERTIFICATE_EXPIRED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := base
			plan.Secrets = append([]SecretLease(nil), base.Secrets...)
			plan.Certificates = append([]CertificateBinding(nil), base.Certificates...)
			tt.mutate(&plan)
			codes := findingCodes(Validate(plan))
			if !codes[tt.code] {
				t.Fatalf("Validate codes = %v, want %q", codes, tt.code)
			}
			if err := Check(plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("Check error = %v, want ErrInvalidPlan", err)
			}
		})
	}
	if findings := Validate(base); len(findings) != 0 {
		t.Fatalf("valid plan findings = %+v", findings)
	}
}

func TestIdentityBinding_ScanCanonicalJSONAndDigestAreFailClosed(t *testing.T) {
	for _, serialized := range []string{
		"password=value", `{"password":"value"}`, "password:value",
		"client_secret=value", `{"client_secret":"value"}`, "client_secret:value",
		"private_key=value", `{"private_key":"value"}`, "private_key:value",
		"token=value", `{"token":"value"}`, "token:value",
		"secret_value=value", `{"secret_value":"value"}`, "secret_value:value",
	} {
		if err := Scan([]byte(serialized)); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("Scan(%q) = %v, want ErrInvalidPlan", serialized, err)
		}
	}
	if err := Scan([]byte(`{"secret_reference":"provider-client"}`)); err != nil {
		t.Fatalf("Scan safe serialized state: %v", err)
	}

	plan := validBindingPlan()
	data, err := CanonicalJSON(plan)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") || strings.Contains(string(data), "RawValue") || strings.Contains(string(data), "do-not-store") {
		t.Fatalf("canonical JSON = %q, want newline and no raw field", data)
	}
	digest, err := Digest(plan)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("Digest = %q, err=%v", digest, err)
	}
	plan.Identity.Service = "changed"
	changedDigest, err := Digest(plan)
	if err != nil || changedDigest == digest {
		t.Fatalf("changed plan digest = %q, err=%v, want a different digest", changedDigest, err)
	}
	plan = validBindingPlan()
	plan.Secrets[0].RawValue = "must-not-store"
	if _, err := CanonicalJSON(plan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("CanonicalJSON with raw secret = %v, want ErrInvalidPlan", err)
	}
}

func TestValidFingerprint_AcceptsOnlyLowercaseSHA256Identity(t *testing.T) {
	valid := "sha256:" + strings.Repeat("a", 64)
	if !validFingerprint(valid) {
		t.Fatal("valid fingerprint rejected")
	}
	for _, value := range []string{
		"", "sha1:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 65), "sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("g", 64), "sha256:" + strings.Repeat("0", 63) + "!",
	} {
		if validFingerprint(value) {
			t.Fatalf("invalid fingerprint %q accepted", value)
		}
	}
}

func TestIdentityBinding_CheckHandlesEvaluationTimeBoundaries(t *testing.T) {
	plan := validBindingPlan()
	plan.EvaluatedAt = plan.Identity.ExpiresAt
	if err := Check(plan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("identity expiry boundary Check = %v, want ErrInvalidPlan", err)
	}
	plan = validBindingPlan()
	plan.Secrets[0].ExpiresAt = plan.EvaluatedAt.Add(time.Nanosecond)
	if err := Check(plan); err != nil {
		t.Fatalf("strictly future secret lease rejected: %v", err)
	}
}
