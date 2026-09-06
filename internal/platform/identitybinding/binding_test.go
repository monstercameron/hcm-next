package identitybinding

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func validBindingPlan() Plan {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return Plan{
		Identity:     WorkloadIdentity{ID: "identity-cell-a-api", Workload: "api", CellID: "cell-a", Service: "api", Destination: "provider-a", ExpiresAt: now.Add(15 * time.Minute)},
		Secrets:      []SecretLease{{ReferenceOnly: true, Name: "provider-client", Version: "v3", Audience: "provider-a", ExpiresAt: now.Add(10 * time.Minute)}},
		Certificates: []CertificateBinding{{ReferenceOnly: true, Reference: "certref://cell-a/api", Subject: "api", Issuer: "issuer-v2", Fingerprint: "sha256:" + strings.Repeat("a", 64), Destination: "provider-a", NotAfter: now.Add(24 * time.Hour)}},
		EvaluatedAt:  now,
	}
}

func TestTodo_IAC_008(t *testing.T) {
	plan := validBindingPlan()
	if err := Check(plan); err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(plan)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("binding digest = %q, err=%v", digest, err)
	}
}

func TestTodo_IAC_008_Golden(t *testing.T) {
	first, err := CanonicalJSON(validBindingPlan())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalJSON(validBindingPlan())
	if err != nil || string(first) != string(second) || strings.Contains(string(first), "RawValue") {
		t.Fatalf("canonical binding plan is unstable or contains raw values: %q", first)
	}
}

func TestTodo_IAC_008_Race(t *testing.T) {
	plan := validBindingPlan()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Check(plan); err != nil {
				t.Errorf("concurrent binding validation = %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_IAC_008_Integration(t *testing.T) {
	plan := validBindingPlan()
	if err := Scan([]byte(`{"identity":"identity-cell-a-api","secret_ref":"provider-client","certificate_ref":"certref://cell-a/api"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Digest(plan); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_IAC_008_Security(t *testing.T) {
	plan := validBindingPlan()
	plan.Secrets[0].RawValue = "do-not-store"
	if err := Check(plan); err == nil {
		t.Fatal("raw secret value was accepted")
	}
	if err := Scan([]byte("client_secret=raw-value")); err == nil {
		t.Fatal("raw secret in serialized state was accepted")
	}
	if err := Scan([]byte(`{"password":"raw-value"}`)); err == nil {
		t.Fatal("JSON raw secret in serialized state was accepted")
	}
	plan = validBindingPlan()
	plan.Identity.Shared = true
	if err := Check(plan); err == nil {
		t.Fatal("shared workload identity was accepted")
	}
}
