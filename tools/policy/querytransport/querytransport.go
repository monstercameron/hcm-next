// Package querytransport checks the cross-surface policy for the authorized
// product-query and invalidation contracts. It consumes semantic envelopes,
// rather than parsing a renderer or a database, so the check can be applied to
// gRPC, HTTP, SSR, and browser-edge evidence alike.
package querytransport

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
)

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// Finding is one cross-surface semantic or bounded-message violation.
type Finding struct {
	Surface string `json:"surface,omitempty"`
	Code    string `json:"code"`
	Detail  string `json:"detail"`
}

func (f Finding) Error() string {
	if f.Surface == "" {
		return fmt.Sprintf("querytransport: %s: %s", f.Code, f.Detail)
	}
	return fmt.Sprintf("querytransport: %s: %s: %s", f.Surface, f.Code, f.Detail)
}

// Observation is one adapter's semantic response. Surface is evidence
// metadata only; it is not part of the envelope digest.
type Observation struct {
	Surface  string
	Envelope productquery.Envelope
}

// Report is a policy result retained by callers for evidence and diagnostics.
type Report struct {
	Findings []Finding
}

func (r Report) OK() bool { return len(r.Findings) == 0 }

func (r Report) Explain() string {
	return fmt.Sprintf("query transport policy v%d with %d finding(s)", schemaVersion, len(r.Findings))
}

// ValidateParity requires every represented surface to carry the same
// canonical semantic envelope. A mismatch is a policy failure even when each
// surface's local payload looks internally valid.
func ValidateParity(observations []Observation) []Finding {
	if len(observations) == 0 {
		return []Finding{{Code: "NO_SURFACES", Detail: "at least one transport observation is required"}}
	}
	findings := make([]Finding, 0)
	seen := make(map[string]struct{}, len(observations))
	baseline := observations[0].Envelope.Digest()
	for _, observation := range observations {
		surface := strings.TrimSpace(observation.Surface)
		if surface == "" {
			findings = append(findings, Finding{Code: "SURFACE_REQUIRED", Detail: "transport observation has no surface"})
			continue
		}
		if _, ok := seen[surface]; ok {
			findings = append(findings, Finding{Surface: surface, Code: "DUPLICATE_SURFACE", Detail: "surface appears more than once"})
			continue
		}
		seen[surface] = struct{}{}
		if observation.Envelope.ContractVersion != productquery.Version() {
			findings = append(findings, Finding{Surface: surface, Code: "CONTRACT_VERSION_MISMATCH", Detail: fmt.Sprintf("got %d, want %d", observation.Envelope.ContractVersion, productquery.Version())})
		}
		if baseline == "" || observation.Envelope.Digest() != baseline {
			findings = append(findings, Finding{Surface: surface, Code: "SEMANTIC_DIGEST_MISMATCH", Detail: "surface does not carry the shared product-query envelope"})
		}
	}
	return sortFindings(findings)
}

// ValidateInvalidation checks the wire-safe shape of an invalidation message.
// Authorization is established by productquery.EmitInvalidation before a
// message reaches this policy boundary; this check ensures no adapter widens
// it or removes its bounds afterward.
func ValidateInvalidation(message productquery.InvalidationMessage) []Finding {
	findings := make([]Finding, 0)
	if message.ContractVersion != productquery.Version() {
		findings = append(findings, Finding{Code: "CONTRACT_VERSION_MISMATCH", Detail: "invalidation uses an unknown contract version"})
	}
	if message.Tenant.Validate() != nil {
		findings = append(findings, Finding{Code: "TENANT_INVALID", Detail: "invalidation tenant is not canonical"})
	}
	if len(message.Items) == 0 {
		findings = append(findings, Finding{Code: "ITEMS_REQUIRED", Detail: "an emitted invalidation must carry at least one authorized item"})
	}
	if len(message.Items) > productquery.MaxInvalidationItems {
		findings = append(findings, Finding{Code: "ITEM_LIMIT_EXCEEDED", Detail: fmt.Sprintf("got %d, want at most %d", len(message.Items), productquery.MaxInvalidationItems)})
	}
	previous := ""
	for _, item := range message.Items {
		key := item.Subject.String()
		if item.Subject.Validate() != nil || item.Subject.Tenant != message.Tenant {
			findings = append(findings, Finding{Code: "ITEM_TENANT_MISMATCH", Detail: "invalidation item is not a valid reference in the message tenant"})
		}
		if item.Revision == 0 {
			findings = append(findings, Finding{Code: "REVISION_REQUIRED", Detail: "invalidation item has no revision"})
		}
		if previous != "" && key <= previous {
			findings = append(findings, Finding{Code: "ITEM_ORDER_INVALID", Detail: "invalidation items must be unique and sorted by subject"})
		}
		previous = key
	}
	if bytes, err := message.CanonicalBytes(); err != nil || len(bytes) > productquery.MaxInvalidationBytes {
		findings = append(findings, Finding{Code: "BYTE_LIMIT_EXCEEDED", Detail: fmt.Sprintf("invalidation exceeds %d bytes", productquery.MaxInvalidationBytes)})
	}
	return sortFindings(findings)
}

func sortFindings(findings []Finding) []Finding {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Surface != findings[j].Surface {
			return findings[i].Surface < findings[j].Surface
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}
