// Package fuzzdefect is a TOOL-013 fixture: a near-copy of
// tools/quality/fuzzkit's ParseEnvelope with one planted defect (it indexes
// the decimal field's dot-split unconditionally instead of checking its
// length first), used to prove the shared fuzz seed corpus in
// tools/quality/fuzzkit.SeedCorpus finds a real, planted bug. This package
// lives under testdata so `go build/vet/test ./...` skip it; it is only
// reachable by tools/quality's TestTodo_TOOL_013 via an explicit path.
package fuzzdefect

import (
	"fmt"
	"strconv"
	"strings"
)

// Envelope mirrors fuzzkit.Envelope.
type Envelope struct {
	Tenant   string
	Kind     string
	Sequence int64
	Decimal  string
}

// ParseEnvelope is fuzzkit.ParseEnvelope with the decimal-field bounds
// check removed: it assumes strings.Split(decimalField, ".") always
// produces exactly two elements and indexes parts[1] unconditionally,
// panicking on any decimal field without exactly one ".".
func ParseEnvelope(input string) (Envelope, error) {
	parts := strings.Split(input, ":")
	if len(parts) != 4 {
		return Envelope{}, fmt.Errorf("fuzzdefect: expected 4 colon-delimited fields, got %d", len(parts))
	}

	tenant, kind, seqField, decimalField := parts[0], parts[1], parts[2], parts[3]
	if tenant == "" {
		return Envelope{}, fmt.Errorf("fuzzdefect: tenant field is empty")
	}
	if kind == "" {
		return Envelope{}, fmt.Errorf("fuzzdefect: kind field is empty")
	}

	seq, err := strconv.ParseInt(seqField, 10, 64)
	if err != nil {
		return Envelope{}, fmt.Errorf("fuzzdefect: invalid sequence field %q: %w", seqField, err)
	}

	// PLANTED DEFECT: no length check before indexing decimalParts[1].
	decimalParts := strings.Split(decimalField, ".")
	whole, fraction := decimalParts[0], decimalParts[1]
	if whole == "" || fraction == "" {
		return Envelope{}, fmt.Errorf("fuzzdefect: invalid decimal field %q", decimalField)
	}

	return Envelope{Tenant: tenant, Kind: kind, Sequence: seq, Decimal: decimalField}, nil
}
