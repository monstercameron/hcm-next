package partition

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Strategy identifies how a partitioned table's rows are routed to its
// partitions. PostgreSQL also supports LIST, but nothing in this data plane
// declares one, so only the two migrations/00005_ledger.sql and a plausible
// future time-range split actually use are named here.
type Strategy string

const (
	// StrategyHash routes rows by hashing PartitionCount over the key
	// column(s), as migrations/00005_ledger.sql does for ledger_event
	// (PARTITION BY HASH (tenant_id), four partitions).
	StrategyHash Strategy = "HASH"
	// StrategyRange routes rows into an ordered, contiguous, half-open set
	// of Bounds over the key column(s).
	StrategyRange Strategy = "RANGE"
)

// RangeBound is one half-open partition range over the key column(s):
// [Lower, Upper). An empty Lower means PostgreSQL's MINVALUE; an empty Upper
// means MAXVALUE. Bounds in a [PartitionPlan] are declared in ascending,
// contiguous order: each bound's Lower equals the previous bound's Upper.
type RangeBound struct {
	Lower string
	Upper string
}

// PartitionPlan is operational metadata describing how one authoritative
// table's rows are physically routed to partitions: the table, the strategy,
// the partition key column(s), and either a partition count (hash) or an
// ordered list of range bounds (range). It is deliberately not domain schema
// (DATA-017 REFACTOR): a caller that only ever reads or writes Table through
// the ordinary SQL surface never needs to know a PartitionPlan exists.
type PartitionPlan struct {
	// Table is the partitioned parent relation's name.
	Table string
	// Strategy selects which of the two field groups below applies.
	Strategy Strategy
	// Key names the partition key column(s), in the order the CREATE TABLE
	// ... PARTITION BY clause declares them.
	Key []string
	// PartitionCount is the modulus for StrategyHash. Zero for StrategyRange.
	PartitionCount int
	// Bounds is the ordered range list for StrategyRange. Empty for
	// StrategyHash.
	Bounds []RangeBound
}

// Validate reports whether the plan is internally consistent: a non-blank
// table and key column(s), the field group matching the declared Strategy,
// at least two hash partitions (one partition is not a partitioning
// decision), and range bounds that are ordered, non-overlapping and
// contiguous (no gap, no overlap, at most one open end on each side).
func (p PartitionPlan) Validate() error {
	if strings.TrimSpace(p.Table) == "" {
		return fmt.Errorf("partition: table name is required")
	}
	if len(p.Key) == 0 {
		return fmt.Errorf("partition: at least one partition key column is required")
	}
	for i, k := range p.Key {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("partition: key column %d is blank", i)
		}
	}

	switch p.Strategy {
	case StrategyHash:
		if len(p.Bounds) != 0 {
			return fmt.Errorf("partition: hash strategy must not declare range bounds")
		}
		if p.PartitionCount < 2 {
			return fmt.Errorf("partition: hash strategy needs at least 2 partitions, got %d", p.PartitionCount)
		}
	case StrategyRange:
		if p.PartitionCount != 0 {
			return fmt.Errorf("partition: range strategy must not declare a partition count")
		}
		if len(p.Bounds) == 0 {
			return fmt.Errorf("partition: range strategy needs at least one bound")
		}
		for i, b := range p.Bounds {
			if b.Lower != "" && b.Upper != "" && b.Lower >= b.Upper {
				return fmt.Errorf("partition: bound %d has lower %q not below upper %q", i, b.Lower, b.Upper)
			}
			if i == 0 {
				continue
			}
			prev := p.Bounds[i-1]
			if prev.Upper == "" {
				return fmt.Errorf("partition: bound %d follows bound %d, which is already open-ended (MAXVALUE)", i, i-1)
			}
			if b.Lower != prev.Upper {
				return fmt.Errorf("partition: bound %d starts at %q, which does not continue from bound %d's end %q",
					i, b.Lower, i-1, prev.Upper)
			}
		}
	default:
		return fmt.Errorf("partition: unknown strategy %q", p.Strategy)
	}
	return nil
}

// Digest returns a stable, order-sensitive content digest of the plan: two
// plans with the same table, strategy, key columns (in the same order),
// partition count and bounds (in the same order) always produce the same
// digest, and any field difference -- including reordering Key or Bounds --
// changes it. It exists so a plan declared in Go and the physical layout a
// live database actually enforces can be compared as one opaque value
// instead of field by field.
//
// Digest does not call Validate: a caller diffing an invalid, in-flight edit
// against a known-good digest still gets a meaningful (different) answer
// rather than an error.
func (p PartitionPlan) Digest() string {
	var b strings.Builder
	fmt.Fprintf(&b, "table=%s\n", p.Table)
	fmt.Fprintf(&b, "strategy=%s\n", p.Strategy)
	fmt.Fprintf(&b, "key=%s\n", strings.Join(p.Key, "|"))
	fmt.Fprintf(&b, "count=%d\n", p.PartitionCount)
	for _, bound := range p.Bounds {
		fmt.Fprintf(&b, "bound=%s..%s\n", bound.Lower, bound.Upper)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// LedgerEventPlan is the PartitionPlan migrations/00005_ledger.sql actually
// declares for ledger_event: PARTITION BY HASH (tenant_id), four partitions
// (ledger_event_p0..p3, MODULUS 4).
var LedgerEventPlan = PartitionPlan{
	Table:          "ledger_event",
	Strategy:       StrategyHash,
	Key:            []string{"tenant_id"},
	PartitionCount: 4,
}
