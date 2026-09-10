// Package evidence produces the BusinessExecutionReceipt for a closed
// intent (EVIDENCE-001): one immutable logical evidence object assembled
// from owner records. The receipt names every terminal dimension with an
// explicit status, references exact digests and authority/provenance
// lineage, verifies its own integrity and exposes an authorized summary
// without copying unrestricted payloads. It is not a second ledger or a
// denormalized source of truth: digests point at owner records, and
// redaction changes status, never verification truth.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrDimensionOmitted reports a receipt that does not name every
	// terminal dimension.
	ErrDimensionOmitted = errors.New("evidence: terminal dimension omitted")

	// ErrDimensionUndigested reports a PRESENT dimension without its
	// exact digest.
	ErrDimensionUndigested = errors.New("evidence: present dimension has no digest")

	// ErrDimensionUnexplained reports a non-present dimension without its
	// reason.
	ErrDimensionUnexplained = errors.New("evidence: non-present dimension has no reason")

	// ErrSealBroken reports a receipt whose digest no longer matches its
	// dimensions and lineage.
	ErrSealBroken = errors.New("evidence: receipt seal is broken")
)

// Status distinguishes why a dimension reads the way it does: absent,
// not-applicable, redacted and unknown are all explicit, never silent.
type Status string

const (
	StatusPresent       Status = "PRESENT"
	StatusAbsent        Status = "ABSENT"
	StatusNotApplicable Status = "NOT_APPLICABLE"
	StatusRedacted      Status = "REDACTED"
	StatusUnknown       Status = "UNKNOWN"
)

// Dimensions is the closed terminal vocabulary every receipt must name.
var Dimensions = []string{
	"intent", "request", "snapshot", "simulation", "proposal",
	"workflow-version", "approvals", "transaction-heads", "domain-revisions",
	"effects", "observations", "reconciliation", "repair", "obligations", "terminal",
}

// Dimension is one named terminal fact: a digest when present, a reason
// otherwise. Payloads never enter the receipt.
type Dimension struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Digest string `json:"digest,omitempty"`
	Note   string `json:"note,omitempty"`
}

// DimensionInput is one caller-supplied dimension.
type DimensionInput struct {
	Name   string
	Status Status
	Digest string
	Note   string
}

// BusinessExecutionReceipt is the immutable evidence object for one closed
// intent.
type BusinessExecutionReceipt struct {
	Tenant           string      `json:"tenant"`
	IntentRef        string      `json:"intent_ref"`
	LineageDigest    string      `json:"lineage_digest"`
	AuthorityLineage []string    `json:"authority_lineage"`
	Dimensions       []Dimension `json:"dimensions"`
	Digest           string      `json:"digest"`
}

func receiptDigest(tenant, intentRef, lineageDigest string, authority []string, dimensions []Dimension) string {
	parts := []string{"execution-receipt", tenant, intentRef, lineageDigest}
	parts = append(parts, authority...)
	for _, dimension := range dimensions {
		parts = append(parts, dimension.Name, string(dimension.Status), dimension.Digest, dimension.Note)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Assemble seals one receipt: every terminal dimension exactly once with
// its status, a digest for every PRESENT dimension and a reason for every
// other one, plus the provenance lineage digest and authority lineage.
func Assemble(tenant, intentRef, lineageDigest string, authorityLineage []string, inputs []DimensionInput) (BusinessExecutionReceipt, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(intentRef) == "" || strings.TrimSpace(lineageDigest) == "" {
		return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble: %w", ErrDimensionOmitted)
	}
	if len(authorityLineage) == 0 {
		return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble: %w", ErrDimensionOmitted)
	}
	byName := make(map[string]DimensionInput, len(inputs))
	for _, input := range inputs {
		if _, dup := byName[input.Name]; dup {
			return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble duplicate %s: %w", input.Name, ErrDimensionOmitted)
		}
		byName[input.Name] = input
	}
	dimensions := make([]Dimension, 0, len(Dimensions))
	for _, name := range Dimensions {
		input, ok := byName[name]
		if !ok {
			return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble %s: %w", name, ErrDimensionOmitted)
		}
		switch input.Status {
		case StatusPresent:
			if strings.TrimSpace(input.Digest) == "" {
				return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble %s: %w", name, ErrDimensionUndigested)
			}
		case StatusAbsent, StatusNotApplicable, StatusRedacted, StatusUnknown:
			if strings.TrimSpace(input.Note) == "" {
				return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble %s: %w", name, ErrDimensionUnexplained)
			}
		default:
			return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Assemble %s status %q: %w", name, input.Status, ErrDimensionOmitted)
		}
		dimensions = append(dimensions, Dimension{Name: name, Status: input.Status, Digest: input.Digest, Note: input.Note})
	}
	authority := append([]string(nil), authorityLineage...)
	sort.Strings(authority)
	receipt := BusinessExecutionReceipt{
		Tenant: tenant, IntentRef: intentRef, LineageDigest: lineageDigest,
		AuthorityLineage: authority, Dimensions: dimensions,
	}
	receipt.Digest = receiptDigest(tenant, intentRef, lineageDigest, authority, dimensions)
	return receipt, nil
}

// Verify recomputes the seal: edited dimensions, swapped lineage or a
// restated authority all fail. Redaction stays verifiable: it changes a
// status through Redact, never behind Verify's back.
func (r BusinessExecutionReceipt) Verify() error {
	if len(r.Dimensions) != len(Dimensions) {
		return fmt.Errorf("evidence: Verify: %w", ErrDimensionOmitted)
	}
	for i, name := range Dimensions {
		if r.Dimensions[i].Name != name {
			return fmt.Errorf("evidence: Verify: %w", ErrDimensionOmitted)
		}
	}
	if receiptDigest(r.Tenant, r.IntentRef, r.LineageDigest, r.AuthorityLineage, r.Dimensions) != r.Digest {
		return fmt.Errorf("evidence: Verify: %w", ErrSealBroken)
	}
	return nil
}

// Redacted returns the receipt with the named dimensions redacted for a
// caller above whose clearance they sit: statuses move to REDACTED with
// the reason, digests drop, and the seal is recomputed. The dimension
// count never changes, so redaction cannot silently erase a fact.
func (r BusinessExecutionReceipt) Redacted(reason string, names ...string) (BusinessExecutionReceipt, error) {
	if strings.TrimSpace(reason) == "" {
		return BusinessExecutionReceipt{}, fmt.Errorf("evidence: Redacted: %w", ErrDimensionUnexplained)
	}
	target := make(map[string]bool, len(names))
	for _, name := range names {
		target[name] = true
	}
	redacted := r
	redacted.Dimensions = append([]Dimension(nil), r.Dimensions...)
	for i, dimension := range redacted.Dimensions {
		if target[dimension.Name] {
			redacted.Dimensions[i] = Dimension{Name: dimension.Name, Status: StatusRedacted, Note: reason}
		}
	}
	redacted.Digest = receiptDigest(redacted.Tenant, redacted.IntentRef, redacted.LineageDigest, redacted.AuthorityLineage, redacted.Dimensions)
	return redacted, nil
}

// RedactionCount reports how many dimensions read redacted.
func (r BusinessExecutionReceipt) RedactionCount() int {
	count := 0
	for _, dimension := range r.Dimensions {
		if dimension.Status == StatusRedacted {
			count++
		}
	}
	return count
}

// Summary exposes the authorized summary: identity, terminal state and
// status counts. It carries digests and counts, never payloads.
func (r BusinessExecutionReceipt) Summary() string {
	terminal := ""
	counts := make(map[Status]int)
	for _, dimension := range r.Dimensions {
		counts[dimension.Status]++
		if dimension.Name == "terminal" {
			terminal = dimension.Digest
		}
	}
	return fmt.Sprintf("receipt %s intent=%s terminal=%s present=%d absent=%d not-applicable=%d redacted=%d unknown=%d",
		r.Digest, r.IntentRef, terminal,
		counts[StatusPresent], counts[StatusAbsent], counts[StatusNotApplicable],
		counts[StatusRedacted], counts[StatusUnknown])
}
