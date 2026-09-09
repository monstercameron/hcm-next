// Package propagation carries non-value semantics across declarative
// transformations. It is deliberately independent of execution and storage.
package propagation

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidMetadata = errors.New("transformation propagation: invalid metadata")
	ErrMissingSource   = errors.New("transformation propagation: missing source metadata")
)

// Classification is ordered by sensitivity. Unknown labels are rejected so a
// caller cannot accidentally create a label that compares below a known one.
type Classification string

const (
	ClassificationPublic       Classification = "PUBLIC"
	ClassificationInternal     Classification = "INTERNAL"
	ClassificationConfidential Classification = "CONFIDENTIAL"
	ClassificationRestricted   Classification = "RESTRICTED"
	ClassificationSecret       Classification = "SECRET"
)

var classRank = map[Classification]int{
	ClassificationPublic: 0, ClassificationInternal: 1,
	ClassificationConfidential: 2, ClassificationRestricted: 3,
	ClassificationSecret: 4,
}

// Metadata is the non-value envelope of one transformed property. Lineage and
// taint are sets represented in canonical order. A VALUE must not carry a
// presence reason; non-VALUE states must not carry value data (data is owned by
// the execution runtime, not this package).
type Metadata struct {
	Presence       values.PresenceState `json:"presence"`
	Classification Classification       `json:"classification"`
	Provenance     []string             `json:"provenance,omitempty"`
	Taint          []string             `json:"taint,omitempty"`
}

func (m Metadata) Validate() error {
	if !m.Presence.Valid() {
		return fmt.Errorf("%w: presence %s", ErrInvalidMetadata, m.Presence)
	}
	if _, ok := classRank[m.Classification]; !ok {
		return fmt.Errorf("%w: classification %q", ErrInvalidMetadata, m.Classification)
	}
	for _, set := range [][]string{m.Provenance, m.Taint} {
		seen := map[string]bool{}
		for _, item := range set {
			if strings.TrimSpace(item) == "" || seen[item] {
				return fmt.Errorf("%w: duplicate or empty lineage/taint", ErrInvalidMetadata)
			}
			seen[item] = true
		}
	}
	return nil
}

// Propagate combines metadata for one operation. Presence joins preserve the
// most informative failure state: a readable value cannot erase an unknown,
// redacted, unavailable, null, or not-applicable source. ABSENT is retained
// only when every source is absent. Classification is a max (never a
// downgrade), while provenance and taint are deterministic unions.
func Propagate(kind transformation.OperationKind, sources []Metadata, defaultMetadata *Metadata) (Metadata, error) {
	if kind == transformation.OpDefault {
		if defaultMetadata == nil {
			return Metadata{}, fmt.Errorf("%w: default metadata", ErrMissingSource)
		}
		if err := defaultMetadata.Validate(); err != nil {
			return Metadata{}, err
		}
		return canonical(*defaultMetadata), nil
	}
	if len(sources) == 0 {
		return Metadata{}, ErrMissingSource
	}
	result := Metadata{Presence: values.PresenceValue, Classification: ClassificationPublic}
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return Metadata{}, err
		}
		if classRank[source.Classification] > classRank[result.Classification] {
			result.Classification = source.Classification
		}
		result.Provenance = append(result.Provenance, source.Provenance...)
		result.Taint = append(result.Taint, source.Taint...)
		result.Presence = joinPresence(result.Presence, source.Presence)
	}
	return canonical(result), nil
}

// WithOperation appends a stable operation lineage marker after propagation.
func WithOperation(m Metadata, operationID string) (Metadata, error) {
	if err := m.Validate(); err != nil {
		return Metadata{}, err
	}
	if strings.TrimSpace(operationID) == "" {
		return Metadata{}, fmt.Errorf("%w: operation id", ErrInvalidMetadata)
	}
	m.Provenance = append(m.Provenance, "operation:"+operationID)
	return canonical(m), nil
}

func joinPresence(a, b values.PresenceState) values.PresenceState {
	// The ordering is semantic, not numeric: absent is harmless only if all
	// inputs are absent; all other non-value states must survive a transform.
	if a == values.PresenceAbsent && b == values.PresenceAbsent {
		return values.PresenceAbsent
	}
	if a == values.PresenceValue {
		return b
	}
	if b == values.PresenceValue {
		return a
	}
	return a
}

func canonical(m Metadata) Metadata {
	m.Provenance = uniqueSorted(m.Provenance)
	m.Taint = uniqueSorted(m.Taint)
	return m
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:0]
	for _, item := range in {
		if len(out) == 0 || out[len(out)-1] != item {
			out = append(out, item)
		}
	}
	return out
}
