package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

const captureSchemaVersion = 1

// Version reports the CLOCK-002 capture contract version.
func Version() int { return captureSchemaVersion }

var (
	// ErrCaptureRejected identifies a capture authentication that cannot create
	// authoritative time evidence.
	ErrCaptureRejected = errors.New("clock: capture authentication rejected")
	// ErrCaptureEvidence identifies an invalid result that cannot be used as
	// the evidence for a capture.
	ErrCaptureEvidence = errors.New("clock: capture evidence is invalid")
)

// DeviceProof is the reference-only proof presented by a registered device.
// Raw keys, certificates, tokens, and signatures never enter this contract.
// ProofRef and Fingerprint identify material held by the approved custody or
// transport boundary.
type DeviceProof struct {
	DeviceID            string
	RegistrationVersion string
	ProofRef            string
	Fingerprint         string
	IssuedAt            time.Time
	ExpiresAt           time.Time
	Shared              bool
	Revoked             bool
}

// WorkerResolution is the already-resolved worker projection consumed at the
// capture boundary. A caller cannot turn an unresolved or inactive worker
// claim into a worker by supplying a display name or an unscoped identifier.
type WorkerResolution struct {
	WorkerRef     string
	Tenant        values.TenantId
	ResolutionRef string
	EvidenceRef   string
	Resolved      bool
	Active        bool
}

// LocationEvidence is the pinned location-policy result for a capture. The
// location policy is supplied by its semantic owner; this package only
// consumes its explicit allow/deny outcome.
type LocationEvidence struct {
	LocationRef   string
	PolicyVersion string
	EvidenceRef   string
	Allowed       bool
}

// CaptureRequest is the complete, side-effect-free authentication question at
// the instant a device claims a worker event. Principal must have come from a
// trust.Verifier; Worker must have come from identity resolution; and Location
// must have come from a location policy evaluation.
type CaptureRequest struct {
	Tenant             values.TenantId
	CapturedAt         time.Time
	DeviceRegistration Registration
	DeviceProof        DeviceProof
	Principal          *trust.Principal
	ClaimedWorkerRef   string
	Worker             WorkerResolution
	Location           LocationEvidence
}

// DeviceEvidence identifies the independently verified device proof.
type DeviceEvidence struct {
	DeviceRef           string
	RegistrationVersion string
	ProofRef            string
	Fingerprint         string
	VerifiedAt          time.Time
}

// PrincipalEvidence identifies the independently verified authenticated
// principal. It deliberately contains no raw credential or role expansion.
type PrincipalEvidence struct {
	Tenant         values.TenantId
	Subject        string
	EvidenceID     string
	Fingerprint    string
	SessionRef     string
	Authentication string
}

// WorkerEvidence preserves the claimed worker and the separate resolution
// evidence that established it. The worker is never inferred from Principal.
type WorkerEvidence struct {
	ClaimedWorkerRef  string
	ResolvedWorkerRef string
	ResolutionRef     string
	EvidenceRef       string
}

// CaptureEvidence is the auditable, separated identity proof produced after a
// successful capture authentication.
type CaptureEvidence struct {
	Device     DeviceEvidence
	Principal  PrincipalEvidence
	Worker     WorkerEvidence
	Location   LocationEvidence
	CapturedAt time.Time
	Digest     string
}

// CaptureResult is an accepted capture authentication and its immutable
// evidence projection. No authoritative punch or other domain effect is
// created by AuthenticateCapture.
type CaptureResult struct {
	Accepted bool
	Evidence CaptureEvidence
}

// CaptureRejection is the stable CLOCK-002 failure shape. Field, state, and
// version make the failed governance fact machine-readable without exposing
// credential material.
type CaptureRejection struct {
	Code    string
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CaptureRejection) Error() string {
	return fmt.Sprintf("%s field=%s state=%s version=%s: %s", r.Code, r.Field, r.State, r.Version, r.Reason)
}

func (r *CaptureRejection) Unwrap() error { return ErrCaptureRejected }

func captureReject(field, state, version, reason string) error {
	return &CaptureRejection{Code: "CLOCK_002_REJECTED", Field: field, State: state, Version: version, Reason: reason}
}

// AuthenticateCapture verifies the device proof, server-derived principal,
// resolved worker claim, and location policy in that order. Every condition
// is explicit and fail-closed; no row, event, outbox item, or provider call
// is produced.
func AuthenticateCapture(req CaptureRequest) (CaptureResult, error) {
	version := req.DeviceRegistration.Version
	state := string(req.DeviceRegistration.State)
	if state == "" {
		state = string(Active)
	}
	if err := req.Tenant.Validate(); err != nil {
		return CaptureResult{}, captureReject("tenant", state, version, "tenant is invalid")
	}
	if req.CapturedAt.IsZero() {
		return CaptureResult{}, captureReject("captured_at", state, version, "capture time is required")
	}
	if err := req.DeviceRegistration.Validate(); err != nil {
		return CaptureResult{}, captureReject("device_registration", state, version, "device registration is invalid")
	}
	if req.DeviceRegistration.State == Revoked {
		return CaptureResult{}, captureReject("device_registration.state", state, version, "device registration is revoked")
	}
	if err := validateDeviceProof(req.DeviceRegistration, req.DeviceProof, req.CapturedAt); err != nil {
		return CaptureResult{}, err
	}
	if err := validatePrincipal(req.Principal, req.Tenant, req.CapturedAt, state, version); err != nil {
		return CaptureResult{}, err
	}
	if err := validateWorker(req.Tenant, req.ClaimedWorkerRef, req.Worker, state, version); err != nil {
		return CaptureResult{}, err
	}
	if err := validateLocation(req.DeviceRegistration, req.Location, state, version); err != nil {
		return CaptureResult{}, err
	}

	capturedAt := req.CapturedAt.UTC()
	result := CaptureResult{
		Accepted: true,
		Evidence: CaptureEvidence{
			Device: DeviceEvidence{
				DeviceRef:           req.DeviceProof.DeviceID,
				RegistrationVersion: req.DeviceProof.RegistrationVersion,
				ProofRef:            req.DeviceProof.ProofRef,
				Fingerprint:         req.DeviceProof.Fingerprint,
				VerifiedAt:          capturedAt,
			},
			Principal: PrincipalEvidence{
				Tenant:         req.Principal.Tenant(),
				Subject:        req.Principal.Subject(),
				EvidenceID:     req.Principal.EvidenceID(),
				Fingerprint:    req.Principal.Fingerprint(),
				SessionRef:     req.Principal.SessionRef(),
				Authentication: req.Principal.AuthenticationMethod().String(),
			},
			Worker: WorkerEvidence{
				ClaimedWorkerRef:  req.ClaimedWorkerRef,
				ResolvedWorkerRef: req.Worker.WorkerRef,
				ResolutionRef:     req.Worker.ResolutionRef,
				EvidenceRef:       req.Worker.EvidenceRef,
			},
			Location:   req.Location,
			CapturedAt: capturedAt,
		},
	}
	result.Evidence.Digest = captureDigest(result.Evidence)
	return result, nil
}

func validateDeviceProof(registration Registration, proof DeviceProof, at time.Time) error {
	state := string(registration.State)
	if state == "" {
		state = string(Active)
	}
	if proof.DeviceID == "" || proof.DeviceID != registration.ID {
		return captureReject("device_proof.device_id", state, registration.Version, "proof does not name the registered device")
	}
	if proof.RegistrationVersion == "" || proof.RegistrationVersion != registration.Version {
		return captureReject("device_proof.registration_version", state, registration.Version, "proof is not bound to the registered revision")
	}
	if strings.TrimSpace(proof.ProofRef) == "" {
		return captureReject("device_proof.proof_ref", state, registration.Version, "opaque proof reference is required")
	}
	if strings.TrimSpace(proof.Fingerprint) == "" {
		return captureReject("device_proof.fingerprint", state, registration.Version, "device binding fingerprint is required")
	}
	if proof.Shared {
		return captureReject("device_proof.shared", state, registration.Version, "shared device proof is prohibited")
	}
	if proof.Revoked {
		return captureReject("device_proof.revoked", state, registration.Version, "device proof is revoked")
	}
	if proof.IssuedAt.IsZero() || proof.ExpiresAt.IsZero() || !proof.ExpiresAt.After(proof.IssuedAt) {
		return captureReject("device_proof.validity", state, registration.Version, "proof validity window is incomplete")
	}
	if at.Before(proof.IssuedAt) || !at.Before(proof.ExpiresAt) {
		return captureReject("device_proof.expires_at", state, registration.Version, "device proof is expired or not yet valid")
	}
	return nil
}

func validatePrincipal(principal *trust.Principal, tenant values.TenantId, at time.Time, state, version string) error {
	if principal == nil {
		return captureReject("principal", state, version, "verified principal is required")
	}
	if principal.Tenant() != tenant {
		return captureReject("principal.tenant", state, version, "principal tenant does not match capture tenant")
	}
	if principal.Subject() == "" || principal.EvidenceID() == "" || principal.Fingerprint() == "" {
		return captureReject("principal.evidence", state, version, "principal lacks verified evidence")
	}
	if at.Before(principal.IssuedAt()) || !at.Before(principal.ExpiresAt()) {
		return captureReject("principal.expires_at", state, version, "principal is expired or not yet valid")
	}
	return nil
}

func validateWorker(tenant values.TenantId, claimed string, worker WorkerResolution, state, version string) error {
	if strings.TrimSpace(claimed) == "" {
		return captureReject("claimed_worker_ref", state, version, "claimed worker reference is required")
	}
	if !worker.Resolved {
		return captureReject("worker.resolved", state, version, "worker claim is unresolved")
	}
	if !worker.Active {
		return captureReject("worker.active", state, version, "worker is not active")
	}
	if worker.Tenant != tenant {
		return captureReject("worker.tenant", state, version, "worker belongs to a different tenant")
	}
	if strings.TrimSpace(worker.WorkerRef) == "" || worker.WorkerRef != claimed {
		return captureReject("worker.worker_ref", state, version, "resolved worker does not match the claim")
	}
	if strings.TrimSpace(worker.ResolutionRef) == "" {
		return captureReject("worker.resolution_ref", state, version, "worker resolution evidence is required")
	}
	if strings.TrimSpace(worker.EvidenceRef) == "" {
		return captureReject("worker.evidence_ref", state, version, "worker evidence reference is required")
	}
	return nil
}

func validateLocation(registration Registration, location LocationEvidence, state, version string) error {
	if strings.TrimSpace(location.LocationRef) == "" {
		return captureReject("location.location_ref", state, version, "location reference is required")
	}
	if location.LocationRef != registration.LocationRef {
		return captureReject("location.location_ref", state, version, "capture location is not the registered device location")
	}
	if strings.TrimSpace(location.PolicyVersion) == "" {
		return captureReject("location.policy_version", state, version, "location policy version is required")
	}
	if strings.TrimSpace(location.EvidenceRef) == "" {
		return captureReject("location.evidence_ref", state, version, "location policy evidence is required")
	}
	if !location.Allowed {
		return captureReject("location.allowed", state, version, "location is prohibited by policy")
	}
	return nil
}

func captureDigest(e CaptureEvidence) string {
	h := sha256.New()
	write := func(label, value string) {
		fmt.Fprintf(h, "%s=%d:%s;", label, len(value), value)
	}
	write("device", e.Device.DeviceRef)
	write("registration_version", e.Device.RegistrationVersion)
	write("proof", e.Device.ProofRef)
	write("device_fingerprint", e.Device.Fingerprint)
	write("principal_tenant", string(e.Principal.Tenant))
	write("principal_subject", e.Principal.Subject)
	write("principal_evidence", e.Principal.EvidenceID)
	write("principal_fingerprint", e.Principal.Fingerprint)
	write("session", e.Principal.SessionRef)
	write("authentication", e.Principal.Authentication)
	write("claimed_worker", e.Worker.ClaimedWorkerRef)
	write("resolved_worker", e.Worker.ResolvedWorkerRef)
	write("resolution", e.Worker.ResolutionRef)
	write("worker_evidence", e.Worker.EvidenceRef)
	write("location", e.Location.LocationRef)
	write("location_policy", e.Location.PolicyVersion)
	write("location_evidence", e.Location.EvidenceRef)
	write("captured_at", e.CapturedAt.UTC().Format(time.RFC3339Nano))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// CaptureExplanation is the safe operator-facing explanation of an accepted
// result. It names each evidence plane without returning credential material.
type CaptureExplanation struct {
	Accepted          bool
	DeviceRef         string
	PrincipalEvidence string
	ClaimedWorkerRef  string
	ResolvedWorkerRef string
	LocationRef       string
	PolicyVersion     string
	Digest            string
}

// Explain validates and summarizes accepted capture evidence.
func Explain(result CaptureResult) (CaptureExplanation, error) {
	if !result.Accepted || result.Evidence.Digest == "" {
		return CaptureExplanation{}, ErrCaptureEvidence
	}
	if expected := captureDigest(result.Evidence); expected != result.Evidence.Digest {
		return CaptureExplanation{}, fmt.Errorf("%w: digest mismatch", ErrCaptureEvidence)
	}
	return CaptureExplanation{
		Accepted:          true,
		DeviceRef:         result.Evidence.Device.DeviceRef,
		PrincipalEvidence: result.Evidence.Principal.EvidenceID,
		ClaimedWorkerRef:  result.Evidence.Worker.ClaimedWorkerRef,
		ResolvedWorkerRef: result.Evidence.Worker.ResolvedWorkerRef,
		LocationRef:       result.Evidence.Location.LocationRef,
		PolicyVersion:     result.Evidence.Location.PolicyVersion,
		Digest:            result.Evidence.Digest,
	}, nil
}
