package artifacts

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleReceipt() Receipt {
	r := Receipt{
		ContractVersion: ContractVersion,
		TenantID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		InstanceID:      uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		From:            Epoch{WorkflowVersion: 1, CompiledPlanDigest: "a", NodeID: "n1", Attempt: 1},
		To:              Epoch{WorkflowVersion: 2, CompiledPlanDigest: "b", NodeID: "n2", Attempt: 1},
		Entries: []Entry{
			{Kind: KindTimer, Identity: "DELAY@x", Disposition: Rekeyed, FromRef: "t1", ToRef: "t2", Deadline: wakeInstant},
			{Kind: KindLease, Identity: "instance:i", Disposition: Carried, FromRef: "l1", ToRef: "l1"},
		},
		MigratedBy: "principal:operator", MigratedAt: fixedInstant,
	}
	sortEntries(r.Entries)
	r.digest = computeReceiptDigest(r)
	return r
}

func TestSortEntries_IsCanonicalByHandlerOrderThenIdentity(t *testing.T) {
	t.Parallel()
	entries := []Entry{
		{Kind: KindReadyWork, Identity: "b"},
		{Kind: KindTimer, Identity: "z"},
		{Kind: KindLease, Identity: "m"},
		{Kind: KindTimer, Identity: "a"},
		{Kind: KindReadyWork, Identity: "a", FromRef: "second"},
		{Kind: KindReadyWork, Identity: "a", FromRef: "first"},
	}
	sortEntries(entries)
	want := []string{"LEASE/m", "TIMER/a", "TIMER/z", "READY_WORK/a", "READY_WORK/a", "READY_WORK/b"}
	for i, e := range entries {
		if got := string(e.Kind) + "/" + e.Identity; got != want[i] {
			t.Fatalf("entry %d = %s, want %s (order %v)", i, got, want[i], want)
		}
	}
	if entries[3].FromRef != "first" || entries[4].FromRef != "second" {
		t.Fatalf("entries sharing an identity are not ordered by durable reference: %+v", entries[3:5])
	}
}

func TestReceiptDigest_IsSensitiveToEveryDigestedField(t *testing.T) {
	t.Parallel()
	base := sampleReceipt()
	if len(base.Digest()) != 64 {
		t.Fatalf("digest length = %d, want 64 hex characters", len(base.Digest()))
	}
	if computeReceiptDigest(base) != base.Digest() {
		t.Fatal("digesting the same receipt twice produced two values")
	}

	mutations := map[string]func(r *Receipt){
		"contract version": func(r *Receipt) { r.ContractVersion = "other/v2" },
		"tenant":           func(r *Receipt) { r.TenantID = uuid.New() },
		"instance":         func(r *Receipt) { r.InstanceID = uuid.New() },
		"source digest":    func(r *Receipt) { r.From.CompiledPlanDigest = "changed" },
		"target node":      func(r *Receipt) { r.To.NodeID = "changed" },
		"target attempt":   func(r *Receipt) { r.To.Attempt = 9 },
		"migrated by":      func(r *Receipt) { r.MigratedBy = "principal:someone-else" },
		"disposition":      func(r *Receipt) { r.Entries[1].Disposition = Deduplicated },
		"entry owner":      func(r *Receipt) { r.Entries[0].Owner = "someone" },
		"entry deadline":   func(r *Receipt) { r.Entries[1].Deadline = wakeInstant.Add(time.Second) },
		"entry to ref":     func(r *Receipt) { r.Entries[1].ToRef = "t3" },
	}
	for name, mutate := range mutations {
		mutated := base
		mutated.Entries = append([]Entry(nil), base.Entries...)
		mutate(&mutated)
		if computeReceiptDigest(mutated) == base.Digest() {
			t.Fatalf("changing the %s did not change the receipt digest", name)
		}
	}
}

// MigratedAt is deliberately not digested: two operators running the same
// migration a second apart produced the same migration, and a receipt whose
// identity moved with the wall clock could not be compared across a replay.
func TestReceiptDigest_IgnoresTheMigrationInstant(t *testing.T) {
	t.Parallel()
	base := sampleReceipt()
	later := base
	later.MigratedAt = base.MigratedAt.Add(time.Hour)
	if computeReceiptDigest(later) != base.Digest() {
		t.Fatal("the receipt digest moved with the migration instant")
	}
}

func TestReceipt_CountAndOfKind(t *testing.T) {
	t.Parallel()
	r := sampleReceipt()
	if got := r.Count(Carried); got != 1 {
		t.Fatalf("Count(CARRIED) = %d, want 1", got)
	}
	if got := r.Count(Rekeyed); got != 1 {
		t.Fatalf("Count(REKEYED) = %d, want 1", got)
	}
	if got := r.Count(Deduplicated); got != 0 {
		t.Fatalf("Count(DEDUPLICATED) = %d, want 0", got)
	}
	timers := r.OfKind(KindTimer)
	if len(timers) != 1 || timers[0].Identity != "DELAY@x" {
		t.Fatalf("OfKind(TIMER) = %+v", timers)
	}
	if got := r.OfKind(KindApproval); len(got) != 0 {
		t.Fatalf("OfKind(APPROVAL) = %+v, want empty", got)
	}
}

func TestInstantText_DistinguishesUnsetFromEpochZero(t *testing.T) {
	t.Parallel()
	if got := instantText(time.Time{}); got != "" {
		t.Fatalf("instantText(zero) = %q, want empty", got)
	}
	if got := instantText(time.Unix(0, 0).UTC()); got == "" {
		t.Fatal("instantText(epoch) rendered empty, which would collide with an unset deadline")
	}
	if got := instantText(wakeInstant); got != "2026-10-01T09:30:00Z" {
		t.Fatalf("instantText(wakeInstant) = %q", got)
	}
}
