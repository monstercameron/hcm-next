package partition_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/partition"
)

func TestPartitionPlan_Validate_Hash(t *testing.T) {
	t.Parallel()

	valid := partition.PartitionPlan{
		Table:          "ledger_event",
		Strategy:       partition.StrategyHash,
		Key:            []string{"tenant_id"},
		PartitionCount: 4,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a plan matching migrations/00005_ledger.sql's real DDL failed to validate: %v", err)
	}

	cases := []struct {
		name string
		plan partition.PartitionPlan
		want string
	}{
		{
			name: "blank table",
			plan: partition.PartitionPlan{Strategy: partition.StrategyHash, Key: []string{"tenant_id"}, PartitionCount: 4},
			want: "table name is required",
		},
		{
			name: "no key columns",
			plan: partition.PartitionPlan{Table: "t", Strategy: partition.StrategyHash, PartitionCount: 4},
			want: "at least one partition key column",
		},
		{
			name: "blank key column",
			plan: partition.PartitionPlan{Table: "t", Strategy: partition.StrategyHash, Key: []string{""}, PartitionCount: 4},
			want: "is blank",
		},
		{
			name: "one partition is not partitioning",
			plan: partition.PartitionPlan{Table: "t", Strategy: partition.StrategyHash, Key: []string{"tenant_id"}, PartitionCount: 1},
			want: "at least 2 partitions",
		},
		{
			name: "zero partitions",
			plan: partition.PartitionPlan{Table: "t", Strategy: partition.StrategyHash, Key: []string{"tenant_id"}},
			want: "at least 2 partitions",
		},
		{
			name: "hash strategy must not declare bounds",
			plan: partition.PartitionPlan{
				Table: "t", Strategy: partition.StrategyHash, Key: []string{"tenant_id"}, PartitionCount: 4,
				Bounds: []partition.RangeBound{{Lower: "a", Upper: "b"}},
			},
			want: "must not declare range bounds",
		},
		{
			name: "unknown strategy",
			plan: partition.PartitionPlan{Table: "t", Strategy: "BOGUS", Key: []string{"tenant_id"}},
			want: "unknown strategy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.plan.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted an invalid plan, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestPartitionPlan_Validate_Range(t *testing.T) {
	t.Parallel()

	valid := partition.PartitionPlan{
		Table:    "ledger_event_by_month",
		Strategy: partition.StrategyRange,
		Key:      []string{"effective_at"},
		Bounds: []partition.RangeBound{
			{Lower: "", Upper: "2026-02-01"},
			{Lower: "2026-02-01", Upper: "2026-03-01"},
			{Lower: "2026-03-01", Upper: ""},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a contiguous, ascending, open-ended-on-both-sides range plan failed to validate: %v", err)
	}

	cases := []struct {
		name string
		plan partition.PartitionPlan
		want string
	}{
		{
			name: "range strategy must not declare a partition count",
			plan: partition.PartitionPlan{
				Table: "t", Strategy: partition.StrategyRange, Key: []string{"k"}, PartitionCount: 2,
				Bounds: []partition.RangeBound{{Lower: "", Upper: "z"}},
			},
			want: "must not declare a partition count",
		},
		{
			name: "range strategy needs at least one bound",
			plan: partition.PartitionPlan{Table: "t", Strategy: partition.StrategyRange, Key: []string{"k"}},
			want: "needs at least one bound",
		},
		{
			name: "lower not below upper",
			plan: partition.PartitionPlan{
				Table: "t", Strategy: partition.StrategyRange, Key: []string{"k"},
				Bounds: []partition.RangeBound{{Lower: "b", Upper: "a"}},
			},
			want: "not below",
		},
		{
			name: "gap between bounds",
			plan: partition.PartitionPlan{
				Table: "t", Strategy: partition.StrategyRange, Key: []string{"k"},
				Bounds: []partition.RangeBound{{Lower: "", Upper: "a"}, {Lower: "b", Upper: "c"}},
			},
			want: "does not continue from bound",
		},
		{
			name: "bound after an open-ended (MAXVALUE) bound",
			plan: partition.PartitionPlan{
				Table: "t", Strategy: partition.StrategyRange, Key: []string{"k"},
				Bounds: []partition.RangeBound{{Lower: "a", Upper: ""}, {Lower: "", Upper: "z"}},
			},
			want: "already open-ended",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.plan.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted an invalid range plan, want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestPartitionPlan_Digest(t *testing.T) {
	t.Parallel()

	base := partition.PartitionPlan{
		Table:          "ledger_event",
		Strategy:       partition.StrategyHash,
		Key:            []string{"tenant_id"},
		PartitionCount: 4,
	}

	if base.Digest() != base.Digest() {
		t.Fatal("Digest() is not deterministic across calls on the identical value")
	}

	identicalFields := partition.PartitionPlan{
		Table: "ledger_event", Strategy: partition.StrategyHash,
		Key: []string{"tenant_id"}, PartitionCount: 4,
	}
	if base.Digest() != identicalFields.Digest() {
		t.Fatal("two independently constructed plans with identical fields produced different digests")
	}

	variants := []partition.PartitionPlan{
		{Table: "other_table", Strategy: partition.StrategyHash, Key: []string{"tenant_id"}, PartitionCount: 4},
		{Table: "ledger_event", Strategy: partition.StrategyRange, Key: []string{"tenant_id"}, Bounds: []partition.RangeBound{{Lower: "", Upper: "z"}}},
		{Table: "ledger_event", Strategy: partition.StrategyHash, Key: []string{"cell_id"}, PartitionCount: 4},
		{Table: "ledger_event", Strategy: partition.StrategyHash, Key: []string{"tenant_id"}, PartitionCount: 8},
		{Table: "ledger_event", Strategy: partition.StrategyHash, Key: []string{"tenant_id", "stream_key"}, PartitionCount: 4},
	}
	seen := map[string]string{base.Digest(): "base"}
	for i, v := range variants {
		d := v.Digest()
		if owner, ok := seen[d]; ok {
			t.Fatalf("variant %d collided with %s's digest %s -- Digest() is not sensitive to this field change", i, owner, d)
		}
		seen[d] = "variant " + string(rune('0'+i))
	}

	// Key order is significant: swapping two key columns is a different
	// physical routing decision and must not share a digest.
	forward := partition.PartitionPlan{Table: "t", Strategy: partition.StrategyHash, Key: []string{"a", "b"}, PartitionCount: 4}
	reversed := partition.PartitionPlan{Table: "t", Strategy: partition.StrategyHash, Key: []string{"b", "a"}, PartitionCount: 4}
	if forward.Digest() == reversed.Digest() {
		t.Fatal("Digest() is not sensitive to partition key column order")
	}
}

func TestPartitionPlan_LedgerEventPlan_MatchesMigration00005(t *testing.T) {
	t.Parallel()
	if err := partition.LedgerEventPlan.Validate(); err != nil {
		t.Fatalf("LedgerEventPlan does not validate: %v", err)
	}
	if partition.LedgerEventPlan.Table != "ledger_event" {
		t.Fatalf("Table = %q, want ledger_event", partition.LedgerEventPlan.Table)
	}
	if partition.LedgerEventPlan.Strategy != partition.StrategyHash {
		t.Fatalf("Strategy = %q, want HASH (migrations/00005_ledger.sql: PARTITION BY HASH (tenant_id))", partition.LedgerEventPlan.Strategy)
	}
	if len(partition.LedgerEventPlan.Key) != 1 || partition.LedgerEventPlan.Key[0] != "tenant_id" {
		t.Fatalf("Key = %v, want [tenant_id]", partition.LedgerEventPlan.Key)
	}
	if partition.LedgerEventPlan.PartitionCount != 4 {
		t.Fatalf("PartitionCount = %d, want 4 (ledger_event_p0..p3)", partition.LedgerEventPlan.PartitionCount)
	}
}
