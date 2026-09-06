// Package providerexit owns the selected-provider side of a dry-run exit.
// It turns provider inventory and adapter evidence into a fail-closed plan;
// it does not call a provider or delete remote state.
package providerexit

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

// Version reports the selected-provider exit contract version.
func Version() int { return contractVersion }

// Explain describes the package without exposing remote payloads or secrets.
func Explain() string {
	return "providerexit v1: complete remote inventory, export verification, authority revocation and unknown-state obligations"
}

// Adapter is the provider boundary. Implementations perform I/O; Reconcile
// remains pure and consumes the returned snapshot and receipts.
type Adapter interface {
	Enumerate(tenant, provider string) (Snapshot, error)
	Export(tenant, provider string, resourceIDs []string) ([]ExportReceipt, error)
	Revoke(tenant, provider string, resourceIDs []string) ([]RevocationReceipt, error)
}

type ResourceKind string

const (
	Object          ResourceKind = "OBJECT"
	RequestResource ResourceKind = "REQUEST"
	Webhook         ResourceKind = "WEBHOOK"
	Credential      ResourceKind = "CREDENTIAL"
	Endpoint        ResourceKind = "ENDPOINT"
	Grant           ResourceKind = "GRANT"
)

// Resource is a metadata-only remote inventory row.
type Resource struct {
	ID                  string       `json:"id"`
	Kind                ResourceKind `json:"kind"`
	ExternalRef         string       `json:"external_ref"`
	Enumerated          bool         `json:"enumerated"`
	Reachable           bool         `json:"reachable"`
	ActiveAuthority     bool         `json:"active_authority,omitempty"`
	Revoked             bool         `json:"revoked"`
	ExportRequired      bool         `json:"export_required"`
	Retained            bool         `json:"retained"`
	Held                bool         `json:"held"`
	DeletionDisposition string       `json:"deletion_disposition"`
}

type OperationState string

const (
	OperationComplete  OperationState = "COMPLETE"
	OperationFailed    OperationState = "FAILED"
	OperationInFlight  OperationState = "IN_FLIGHT"
	OperationAmbiguous OperationState = "AMBIGUOUS"
)

// Operation retains timeout-after-send ambiguity instead of treating a
// provider outage as evidence that the operation did not happen.
type Operation struct {
	ID             string         `json:"id"`
	State          OperationState `json:"state"`
	Observed       bool           `json:"observed"`
	ObservationRef string         `json:"observation_ref"`
	Disposition    string         `json:"disposition"`
}

type ExportReceipt struct {
	ResourceID     string `json:"resource_id"`
	ExpectedDigest string `json:"expected_digest"`
	ActualDigest   string `json:"actual_digest"`
	SchemaVersion  string `json:"schema_version"`
	Verified       bool   `json:"verified"`
}

type RevocationReceipt struct {
	ResourceID string       `json:"resource_id"`
	Kind       ResourceKind `json:"kind"`
	At         time.Time    `json:"at"`
	Success    bool         `json:"success"`
}

type Snapshot struct {
	Resources  []Resource  `json:"resources"`
	Operations []Operation `json:"operations"`
}

// Request is the provider-specific evidence envelope.
type Request struct {
	Tenant          string              `json:"tenant"`
	Provider        string              `json:"provider"`
	RequestedBy     string              `json:"requested_by"`
	At              time.Time           `json:"at"`
	Reachable       bool                `json:"reachable"`
	Resources       []Resource          `json:"resources"`
	Operations      []Operation         `json:"operations"`
	Exports         []ExportReceipt     `json:"exports"`
	Revocations     []RevocationReceipt `json:"revocations"`
	HoldResourceIDs []string            `json:"hold_resource_ids"`
}

type Obligation struct {
	Code    string `json:"code"`
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Detail  string `json:"detail"`
}

type Blocker struct {
	Code    string `json:"code"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

type Plan struct {
	Tenant              string       `json:"tenant"`
	Provider            string       `json:"provider"`
	Status              string       `json:"status"`
	ResourceCount       int          `json:"resource_count"`
	RevocationCount     int          `json:"revocation_count"`
	ExportCount         int          `json:"export_count"`
	RetainedResourceIDs []string     `json:"retained_resource_ids"`
	Obligations         []Obligation `json:"obligations"`
	Blockers            []Blocker    `json:"blockers"`
	Digest              string       `json:"digest"`
}

const (
	StatusCertifiable = "CERTIFIABLE"
	StatusBlocked     = "BLOCKED"
)

var ErrInvalidRequest = errors.New("providerexit: invalid request")

// Reconcile validates a provider exit snapshot and returns a dry-run plan.
// A provider outage or ambiguous operation is represented as an explicit
// obligation and blocker, never as zero remote authority.
func Reconcile(req Request) (Plan, error) {
	if strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.Provider) == "" || strings.TrimSpace(req.RequestedBy) == "" || req.At.IsZero() {
		return Plan{}, fmt.Errorf("%w: tenant, provider, requester and time are required", ErrInvalidRequest)
	}
	if len(req.Resources) == 0 {
		return Plan{}, fmt.Errorf("%w: remote inventory is required", ErrInvalidRequest)
	}
	resources := append([]Resource(nil), req.Resources...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	blockers, obligations, retained := validate(req, resources)
	plan := Plan{Tenant: req.Tenant, Provider: req.Provider, Status: StatusCertifiable, ResourceCount: len(resources), RevocationCount: len(req.Revocations), ExportCount: len(req.Exports), RetainedResourceIDs: retained, Obligations: obligations, Blockers: blockers}
	if len(blockers) > 0 {
		plan.Status = StatusBlocked
	}
	plan.Digest = digest(plan)
	return plan, nil
}

func (p Plan) Explain() string {
	return fmt.Sprintf("provider exit v%d tenant=%s provider=%s status=%s resources=%d exports=%d revocations=%d obligations=%d blockers=%d digest=%s", Version(), p.Tenant, p.Provider, p.Status, p.ResourceCount, p.ExportCount, p.RevocationCount, len(p.Obligations), len(p.Blockers), p.Digest)
}

func validate(req Request, resources []Resource) ([]Blocker, []Obligation, []string) {
	var blockers []Blocker
	var obligations []Obligation
	var retained []string
	seen := map[string]bool{}
	holds := map[string]bool{}
	for _, id := range req.HoldResourceIDs {
		holds[id] = true
	}
	validKinds := map[ResourceKind]bool{Object: true, RequestResource: true, Webhook: true, Credential: true, Endpoint: true, Grant: true}
	for _, resource := range resources {
		if resource.ID == "" || seen[resource.ID] {
			blockers = append(blockers, Blocker{"RESOURCE_ID_INVALID", resource.ID, "remote resource identity is not unique"})
			continue
		}
		seen[resource.ID] = true
		if !validKinds[resource.Kind] || resource.ExternalRef == "" {
			blockers = append(blockers, Blocker{"RESOURCE_ID_INVALID", resource.ID, "kind and external reference are required"})
		}
		if !resource.Enumerated {
			blockers = append(blockers, Blocker{"RESOURCE_UNENUMERATED", resource.ID, "provider inventory did not account for this resource"})
		}
		if !resource.Reachable || !req.Reachable {
			obligations = append(obligations, Obligation{"MANUAL_PROVIDER_RECONCILIATION", resource.ID, "MANUAL", "remote state could not be independently verified"})
			blockers = append(blockers, Blocker{"REMOTE_STATE_UNKNOWN", resource.ID, "provider reachability is not sufficient for certification"})
		}
		if resource.ExportRequired || resource.Retained || resource.Held || holds[resource.ID] {
			receipt, ok := exportFor(req.Exports, resource.ID)
			if !ok || !receipt.Verified || receipt.ExpectedDigest == "" || receipt.ActualDigest == "" || receipt.ExpectedDigest != receipt.ActualDigest || receipt.SchemaVersion == "" {
				blockers = append(blockers, Blocker{"EXPORT_UNVERIFIED", resource.ID, "retained or held resource lacks a matching verified export digest"})
			}
		}
		if resource.Retained || resource.Held || holds[resource.ID] {
			retained = append(retained, resource.ID)
		}
		if resource.ActiveAuthority && !hasRevocation(req.Revocations, resource.ID) {
			blockers = append(blockers, Blocker{"ACTIVE_AUTHORITY", resource.ID, "credential, endpoint, webhook or grant remains active"})
		}
		if resource.Retained || resource.Held || holds[resource.ID] {
			if resource.DeletionDisposition == "" {
				obligations = append(obligations, Obligation{"LEGAL_RETENTION", resource.ID, "LEGAL", "resource is retained under hold or obligation"})
			}
		}
	}
	for _, id := range req.HoldResourceIDs {
		if !seen[id] {
			blockers = append(blockers, Blocker{"HOLD_RESOURCE_UNKNOWN", id, "legal hold references an uninventoried remote resource"})
		}
	}
	for _, op := range req.Operations {
		if op.ID == "" || op.Disposition == "" {
			blockers = append(blockers, Blocker{"OPERATION_UNDISPOSITIONED", op.ID, "every in-flight operation needs a disposition"})
		}
		if op.State == OperationAmbiguous || op.State == OperationInFlight {
			if !op.Observed || op.ObservationRef == "" {
				obligations = append(obligations, Obligation{"AMBIGUOUS_OPERATION", op.ID, "MANUAL", "observation or governed repair is required before redrive"})
				blockers = append(blockers, Blocker{"AMBIGUOUS_OPERATION", op.ID, "provider operation outcome is not verified"})
			}
		}
	}
	sort.Strings(retained)
	sort.SliceStable(obligations, func(i, j int) bool {
		if obligations[i].Code != obligations[j].Code {
			return obligations[i].Code < obligations[j].Code
		}
		return obligations[i].Subject < obligations[j].Subject
	})
	sort.SliceStable(blockers, func(i, j int) bool {
		if blockers[i].Code != blockers[j].Code {
			return blockers[i].Code < blockers[j].Code
		}
		return blockers[i].Subject < blockers[j].Subject
	})
	return blockers, obligations, retained
}

func exportFor(receipts []ExportReceipt, id string) (ExportReceipt, bool) {
	for _, receipt := range receipts {
		if receipt.ResourceID == id {
			return receipt, true
		}
	}
	return ExportReceipt{}, false
}
func hasRevocation(receipts []RevocationReceipt, id string) bool {
	for _, receipt := range receipts {
		if receipt.ResourceID == id && receipt.Success {
			return true
		}
	}
	return false
}
func digest(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(append([]byte("hcmnext.providerexit/v1\x00"), b...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
