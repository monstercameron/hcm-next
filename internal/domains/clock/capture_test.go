package clock

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const captureTestTime = "2026-01-01T12:00:00Z"

func captureRegistration() Registration {
	return Registration{
		ID: "device-1", Version: "v1", State: Active,
		OwnerRef: "owner:workforce", LocationRef: "loc:nyc",
		ClockTrustPolicy: "clock-trust/v1", OfflinePolicy: "offline:bounded",
		ReplayPolicy: "replay:sequence", SignaturePolicy: "sig:ed25519",
		FirmwarePolicy: "firmware:current", CertificatePolicy: "cert:device",
		RetentionPolicy: "retention:time-punch",
	}
}

func capturePrincipal(t *testing.T, now time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId("acme"),
		Subject:              "subject-1",
		SubjectKind:          trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodMutualTLS,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-1",
		CredentialDigest:     "sha256:credential",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func validCaptureRequest(t *testing.T) CaptureRequest {
	t.Helper()
	now, _ := time.Parse(time.RFC3339, captureTestTime)
	return CaptureRequest{
		Tenant:             values.TenantId("acme"),
		CapturedAt:         now,
		DeviceRegistration: captureRegistration(),
		DeviceProof: DeviceProof{
			DeviceID: "device-1", RegistrationVersion: "v1", ProofRef: "proof:device-1",
			Fingerprint: "sha256:device-fingerprint", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		},
		Principal:        capturePrincipal(t, now),
		ClaimedWorkerRef: "worker-1",
		Worker: WorkerResolution{
			WorkerRef: "worker-1", Tenant: values.TenantId("acme"), ResolutionRef: "resolution-1",
			EvidenceRef: "evidence:worker-1", Resolved: true, Active: true,
		},
		Location: LocationEvidence{LocationRef: "loc:nyc", PolicyVersion: "location-policy/v1", EvidenceRef: "evidence:location-1", Allowed: true},
	}
}

// TestTodo_CLOCK_002 is the primary contract test: independent device,
// principal, worker, and location evidence is required before acceptance.
func TestTodo_CLOCK_002(t *testing.T) {
	valid := validCaptureRequest(t)
	result, err := AuthenticateCapture(valid)
	if err != nil {
		t.Fatalf("valid capture rejected: %v", err)
	}
	if Version() != 1 || !result.Accepted {
		t.Fatalf("version=%d result=%+v", Version(), result)
	}
	if result.Evidence.Device.DeviceRef == result.Evidence.Principal.Subject || result.Evidence.Worker.ClaimedWorkerRef == result.Evidence.Principal.Subject {
		t.Fatal("capture collapsed device, principal, or worker identity")
	}
	if result.Evidence.Device.ProofRef == "" || result.Evidence.Principal.EvidenceID == "" || result.Evidence.Worker.EvidenceRef == "" {
		t.Fatal("accepted evidence omitted one identity plane")
	}
	if _, err := Explain(result); err != nil {
		t.Fatalf("accepted evidence is not explainable: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*CaptureRequest)
		field  string
	}{
		{name: "shared device proof", mutate: func(r *CaptureRequest) { r.DeviceProof.Shared = true }, field: "device_proof.shared"},
		{name: "expired device proof", mutate: func(r *CaptureRequest) { r.DeviceProof.ExpiresAt = r.CapturedAt }, field: "device_proof.expires_at"},
		{name: "revoked device proof", mutate: func(r *CaptureRequest) { r.DeviceProof.Revoked = true }, field: "device_proof.revoked"},
		{name: "unresolved worker", mutate: func(r *CaptureRequest) { r.Worker.Resolved = false }, field: "worker.resolved"},
		{name: "prohibited location", mutate: func(r *CaptureRequest) { r.Location.Allowed = false }, field: "location.allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validCaptureRequest(t)
			tc.mutate(&req)
			_, err := AuthenticateCapture(req)
			var rejection *CaptureRejection
			if !errors.As(err, &rejection) || rejection.Code != "CLOCK_002_REJECTED" || rejection.Field != tc.field {
				t.Fatalf("err=%v, want CLOCK_002_REJECTED field=%s", err, tc.field)
			}
		})
	}
}

func TestTodo_CLOCK_002_Property(t *testing.T) {
	req := validCaptureRequest(t)
	first, err := AuthenticateCapture(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AuthenticateCapture(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Evidence.Digest != second.Evidence.Digest {
		t.Fatal("identical capture evidence produced different digests")
	}
	req.ClaimedWorkerRef = "worker-2"
	req.Worker.WorkerRef = "worker-2"
	changed, err := AuthenticateCapture(req)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Evidence.Digest == first.Evidence.Digest {
		t.Fatal("digest did not bind the claimed worker")
	}
	changed.Evidence.Worker.ResolvedWorkerRef = "worker-tampered"
	if _, err := Explain(changed); !errors.Is(err, ErrCaptureEvidence) {
		t.Fatalf("tampered evidence err=%v, want ErrCaptureEvidence", err)
	}
}
