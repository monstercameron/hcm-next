// Package identitybinding models portable workload identity, secret lease,
// and certificate bindings. Values are intentionally reference-only.
package identitybinding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

func Version() int { return contractVersion }

func Explain() string {
	return "IAC-008 v1: workload-scoped short-lived identity secret leases and certificate bindings without raw values"
}

type WorkloadIdentity struct {
	ID          string
	Workload    string
	CellID      string
	Service     string
	Destination string
	Shared      bool
	ExpiresAt   time.Time
}

type SecretLease struct {
	ReferenceOnly bool
	Name          string
	Version       string
	Audience      string
	ExpiresAt     time.Time
	RawValue      string `json:"-"`
}

type CertificateBinding struct {
	ReferenceOnly bool
	Reference     string
	Subject       string
	Issuer        string
	Fingerprint   string
	Destination   string
	NotAfter      time.Time
}

type Plan struct {
	Identity     WorkloadIdentity
	Secrets      []SecretLease
	Certificates []CertificateBinding
	EvaluatedAt  time.Time
}

type Finding struct {
	Field  string
	Code   string
	Detail string
}

var ErrInvalidPlan = errors.New("identitybinding: invalid binding plan")

func Validate(p Plan) []Finding {
	var out []Finding
	need := func(field, code, detail string, bad bool) {
		if bad {
			out = append(out, Finding{Field: field, Code: code, Detail: detail})
		}
	}
	i := p.Identity
	need("identity.id", "IDENTITY_REQUIRED", "workload identity id is required", strings.TrimSpace(i.ID) == "")
	need("identity.workload", "WORKLOAD_REQUIRED", "workload identity must name one workload", strings.TrimSpace(i.Workload) == "")
	need("identity.cell", "CELL_REQUIRED", "workload identity must name one cell", strings.TrimSpace(i.CellID) == "")
	need("identity.service", "SERVICE_REQUIRED", "workload identity must name one service", strings.TrimSpace(i.Service) == "")
	need("identity.destination", "DESTINATION_REQUIRED", "workload identity must name one destination", strings.TrimSpace(i.Destination) == "")
	need("identity.shared", "SHARED_IDENTITY", "workload identities must not be shared", i.Shared)
	if !i.ExpiresAt.After(p.EvaluatedAt) {
		out = append(out, Finding{Field: "identity.expires_at", Code: "IDENTITY_EXPIRED", Detail: "identity must expire after evaluation"})
	}
	if len(p.Secrets) == 0 {
		out = append(out, Finding{Field: "secrets", Code: "SECRET_LEASE_REQUIRED", Detail: "at least one reference-only secret lease is required"})
	}
	for index, secret := range p.Secrets {
		field := fmt.Sprintf("secrets[%d]", index)
		need(field+".reference", "SECRET_REFERENCE_REQUIRED", "secret leases require a name and version", strings.TrimSpace(secret.Name) == "" || strings.TrimSpace(secret.Version) == "")
		need(field+".audience", "SECRET_AUDIENCE_REQUIRED", "secret lease audience must match the workload destination", secret.Audience != i.Destination)
		need(field+".reference_only", "RAW_SECRET_FORBIDDEN", "secret leases must not carry values", !secret.ReferenceOnly || secret.RawValue != "")
		if !secret.ExpiresAt.After(p.EvaluatedAt) {
			out = append(out, Finding{Field: field + ".expires_at", Code: "SECRET_LEASE_EXPIRED", Detail: "secret lease must expire after evaluation"})
		}
	}
	if len(p.Certificates) == 0 {
		out = append(out, Finding{Field: "certificates", Code: "CERTIFICATE_REQUIRED", Detail: "at least one certificate binding is required"})
	}
	for index, cert := range p.Certificates {
		field := fmt.Sprintf("certificates[%d]", index)
		need(field+".reference", "CERTIFICATE_REFERENCE_REQUIRED", "certificate reference, issuer and subject are required", strings.TrimSpace(cert.Reference) == "" || strings.TrimSpace(cert.Issuer) == "" || strings.TrimSpace(cert.Subject) == "")
		need(field+".reference_only", "RAW_CERTIFICATE_FORBIDDEN", "certificate bindings must be reference-only", !cert.ReferenceOnly)
		need(field+".destination", "CERTIFICATE_DESTINATION_MISMATCH", "certificate destination must match the workload destination", cert.Destination != i.Destination)
		if !validFingerprint(cert.Fingerprint) {
			out = append(out, Finding{Field: field + ".fingerprint", Code: "CERTIFICATE_FINGERPRINT_REQUIRED", Detail: "certificate fingerprint must be a sha256 identity"})
		}
		if !cert.NotAfter.After(p.EvaluatedAt) {
			out = append(out, Finding{Field: field + ".not_after", Code: "CERTIFICATE_EXPIRED", Detail: "certificate must expire after evaluation"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func Check(p Plan) error {
	if findings := Validate(p); len(findings) != 0 {
		f := findings[0]
		return fmt.Errorf("%w: %s %s: %s", ErrInvalidPlan, f.Code, f.Field, f.Detail)
	}
	return nil
}

// Scan rejects common serialized secret-bearing fields before plan/state
// bytes can be admitted to an evidence or state store.
func Scan(serialized []byte) error {
	text := strings.ToLower(string(serialized))
	for _, marker := range []string{"password=", "password\":", "password:", "client_secret=", "client_secret\":", "client_secret:", "private_key=", "private_key\":", "private_key:", "token=", "token\":", "token:", "secret_value=", "secret_value\":", "secret_value:"} {
		if strings.Contains(text, marker) {
			return fmt.Errorf("%w: serialized plan contains %s", ErrInvalidPlan, marker)
		}
	}
	return nil
}

func CanonicalJSON(p Plan) ([]byte, error) {
	if err := Check(p); err != nil {
		return nil, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("identitybinding: encode plan: %w", err)
	}
	if err := Scan(data); err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Digest(p Plan) (string, error) {
	data, err := CanonicalJSON(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validFingerprint(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(strings.TrimPrefix(value, "sha256:")) != 64 {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "sha256:") {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
