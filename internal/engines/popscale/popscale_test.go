package popscale_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	popscale "github.com/monstercameron/hcm-next/internal/engines/popscale"
	"github.com/monstercameron/hcm-next/internal/engines/population"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func digestFor(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func memberIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, fmt.Sprintf("worker-%06d", i))
	}
	sort.Strings(ids)
	return ids
}

// frozenSnapshot builds a snapshot the way POP-005 would have frozen it:
// sorted unique membership, no raw ids when protected, a digest, and the
// given count. Callers that want an inconsistent snapshot set Count after the
// fact; the helper never papers over a count.
func frozenSnapshot(defID string, n int, protected bool, count values.Presence[int]) population.Snapshot {
	ids := memberIDs(n)
	if protected {
		ids = nil
	}
	ka, err := values.NewKnownAt(values.NewInstant(baseTime))
	if err != nil {
		panic(err)
	}
	return population.Snapshot{
		DefinitionID:        defID,
		DefinitionDigest:    digestFor("def:" + defID),
		RevisionVersion:     "2026.1",
		AsOf:                values.NewInstant(baseTime),
		KnownAt:             ka,
		SubjectIDs:          ids,
		MembershipProtected: protected,
		Count:               count,
		Completeness:        population.CompletenessComplete,
		Digest:              digestFor("frozen:" + defID),
	}
}

func fullCaller(view population.CountDisclosure) popscale.Caller {
	return popscale.Caller{MembershipDisclosed: true, CountAuthorized: true, CountView: view}
}

// resolveRejected walks one Resolve call and asserts it returned a
// POP_010_REJECTED with exactly the named field and state, and that nothing
// was persisted.
func resolveRejected(t *testing.T, snap population.Snapshot, req popscale.Request, wantField, wantState string) {
	t.Helper()
	sink := &popscale.Sink{}
	req.Sink = sink
	_, err := popscale.Resolve(snap, req)
	if !popscale.IsRejected(err) {
		t.Fatalf("expected POP_010_REJECTED with field=%s state=%s, got %v", wantField, wantState, err)
	}
	rej, ok := err.(*popscale.Rejection)
	if !ok || rej == nil {
		t.Fatalf("error %v does not carry rejection detail", err)
	}
	if rej.Field != wantField || rej.State != wantState {
		t.Fatalf("rejection = field %q state %q, want field %q state %q", rej.Field, rej.State, wantField, wantState)
	}
	if sink.Total() != 0 {
		t.Fatalf("a rejected resolution persisted side effects: %+v", sink)
	}
}

// TestTodo_POP_010 is the PRIMARY clause: a frozen snapshot paginates
// exactly once to a caller the restriction decision authorized, discloses the
// count that authorization allows, and every seeded defect - an unauthorized
// concrete count, a membership set that would lose or duplicate members, a
// torn token or a count that disagrees with its own membership - is refused
// with POP_010_REJECTED naming the offending field, state and version while
// persisting zero authoritative rows, business events, outbox entries, human
// work and provider requests.
func TestTodo_POP_010(t *testing.T) {
	sink := &popscale.Sink{}
	snap := frozenSnapshot("pop-primary", 101, false, values.Value(101))

	t.Run("GREEN_walks_the_full_membership_exactly_once", func(t *testing.T) {
		pages := []struct {
			token   string
			wantLen int
		}{
			{"", 50},
			{"50", 50},
			{"100", 1},
		}
		var union []string
		for _, pg := range pages {
			resp, err := popscale.Resolve(snap, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50, Token: pg.token, Sink: sink})
			if err != nil {
				t.Fatalf("Resolve(token=%q): %v", pg.token, err)
			}
			if len(resp.Page.Subjects) != pg.wantLen {
				t.Fatalf("page %q has %d subjects, want %d", pg.token, len(resp.Page.Subjects), pg.wantLen)
			}
			union = append(union, resp.Page.Subjects...)
		}
		if len(union) != 101 {
			t.Fatalf("walk covered %d members, want 101", len(union))
		}
		if got := len(mapOf(union)); got != 101 {
			t.Fatalf("walk covered %d distinct members, want 101", got)
		}
		if sink.Total() != 0 {
			t.Fatalf("serving pages persisted side effects: %+v", sink)
		}
	})

	t.Run("GREEN_last_page_ends_the_walk_and_exact_count_is_served", func(t *testing.T) {
		resp, err := popscale.Resolve(snap, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50, Token: "100"})
		if err != nil {
			t.Fatalf("Resolve(final page): %v", err)
		}
		if resp.Page.Truncated || resp.Page.NextToken != "" {
			t.Fatalf("final page is not final: %+v", resp.Page)
		}
		if len(resp.Page.Subjects) != 1 || resp.Page.Subjects[0] != "worker-000101" {
			t.Fatalf("final page = %v, want [worker-000101]", resp.Page.Subjects)
		}
		if resp.Count.View != popscale.CountExact || resp.Count.Exact != 101 || resp.Count.Band != "" {
			t.Fatalf("count = %+v, want exact 101", resp.Count)
		}
	})

	t.Run("GREEN_banded_view_never_carries_the_exact_count", func(t *testing.T) {
		resp, err := popscale.Resolve(snap, popscale.Request{Caller: fullCaller(population.CountDisclosureBanded), PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(banded): %v", err)
		}
		if resp.Count.View != popscale.CountBanded || resp.Count.Band != "100-149" || resp.Count.Exact != 0 {
			t.Fatalf("banded count = %+v, want band 100-149 and no exact", resp.Count)
		}
	})

	t.Run("RED_unauthorized_concrete_count_is_refused_not_served", func(t *testing.T) {
		caller := popscale.Caller{CountView: population.CountDisclosureExact}
		resolveRejected(t, snap, popscale.Request{Caller: caller, PageSize: 50}, "count", "unauthorized")
	})

	t.Run("RED_membership_with_a_duplicate_member_is_rejected", func(t *testing.T) {
		s := frozenSnapshot("pop-dup", 3, false, values.Value(3))
		s.SubjectIDs = []string{"worker-000001", "worker-000001", "worker-000002"}
		resolveRejected(t, s, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50}, "membership", "duplicate")
	})

	t.Run("RED_out_of_order_membership_is_rejected", func(t *testing.T) {
		s := frozenSnapshot("pop-unsorted", 2, false, values.Value(2))
		s.SubjectIDs = []string{"worker-000002", "worker-000001"}
		resolveRejected(t, s, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50}, "membership", "unsorted")
	})

	t.Run("RED_malformed_token_is_rejected", func(t *testing.T) {
		resolveRejected(t, snap, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50, Token: "abc"}, "token", "malformed")
	})

	t.Run("RED_token_past_the_end_is_rejected", func(t *testing.T) {
		resolveRejected(t, snap, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50, Token: "1000"}, "token", "past_end")
	})

	t.Run("RED_nonpositive_page_size_is_rejected", func(t *testing.T) {
		resolveRejected(t, snap, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 0}, "page_size", "not_positive")
	})

	t.Run("RED_a_snapshot_that_was_never_frozen_is_rejected", func(t *testing.T) {
		s := snap
		s.Digest = ""
		resolveRejected(t, s, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50}, "snapshot", "not_frozen")
	})

	t.Run("RED_a_count_that_disagrees_with_its_own_membership_is_rejected", func(t *testing.T) {
		s := frozenSnapshot("pop-mismatch", 5, false, values.Value(5))
		s.Count = values.Value(6)
		resolveRejected(t, s, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50}, "count", "inconsistent")
	})
}

// TestTodo_POP_010_Mutation is the MUTATION clause: mutating any single part
// of a valid walk - the token, the membership order, the recorded count or
// the page size - is refused or provably lossless, and never served as a
// losing or duplicating page; re-asking for a page returns the identical
// page, and tampering with a served page cannot steal the next one.
func TestTodo_POP_010_Mutation(t *testing.T) {
	snap := frozenSnapshot("pop-mutation", 101, false, values.Value(101))
	base := popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50}

	t.Run("re_asking_for_the_first_page_serves_the_same_page_twice", func(t *testing.T) {
		sess, err := popscale.NewSession(snap, base.Caller, base.PageSize)
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		first, err := sess.Page("")
		if err != nil {
			t.Fatalf("Page(first): %v", err)
		}
		again, err := sess.Page("") // empty token again
		if err != nil {
			t.Fatalf("Page(repeat): %v", err)
		}
		if string(joinPage(first)) != string(joinPage(again)) {
			t.Fatalf("re-asking for the first page differs:\n%v\n%v", first.Subjects, again.Subjects)
		}
	})

	t.Run("mutating_a_token_one_past_the_end_is_refused", func(t *testing.T) {
		base.Token = "102" // 101 members, so one past the end
		resolveRejected(t, snap, base, "token", "past_end")
		base.Token = ""
	})

	t.Run("moving_one_member_out_of_order_breaks_the_walk", func(t *testing.T) {
		s := snap
		ids := memberIDs(101)
		s.SubjectIDs = append(ids[1:], ids[0])
		resolveRejected(t, s, base, "membership", "unsorted")
	})

	t.Run("changing_the_recorded_count_breaks_the_walk", func(t *testing.T) {
		s := snap
		s.Count = values.Value(102)
		resolveRejected(t, s, base, "count", "inconsistent")
	})

	t.Run("shrinking_the_page_size_loses_no_member_and_duplicates_none", func(t *testing.T) {
		sess, err := popscale.NewSession(snap, base.Caller, 7)
		if err != nil {
			t.Fatalf("NewSession(7): %v", err)
		}
		var union []string
		token := ""
		for {
			page, err := sess.Page(token)
			if err != nil {
				t.Fatalf("Page(%q): %v", token, err)
			}
			union = append(union, page.Subjects...)
			if !page.Truncated {
				break
			}
			token = page.NextToken
		}
		if len(union) != 101 {
			t.Fatalf("page size 7 walk covered %d members, want 101", len(union))
		}
		if got := len(mapOf(union)); got != 101 {
			t.Fatalf("page size 7 walk covered %d distinct members, want 101", got)
		}
	})

	t.Run("tampering_with_a_served_page_cannot_steal_the_next_page", func(t *testing.T) {
		sess, err := popscale.NewSession(snap, base.Caller, 50)
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		first, err := sess.Page("")
		if err != nil {
			t.Fatalf("Page(first): %v", err)
		}
		nextToken := first.NextToken
		first.Subjects = []string{"attacker-injected"} // tamper with the served copy
		second, err := sess.Page(nextToken)
		if err != nil {
			t.Fatalf("Page(second): %v", err)
		}
		want := memberIDs(101)[50:100]
		if len(second.Subjects) != len(want) {
			t.Fatalf("second page has %d subjects, want %d", len(second.Subjects), len(want))
		}
		for i := range want {
			if second.Subjects[i] != want[i] {
				t.Fatalf("second page[%d] = %q, want %q (tampered first page altered the walk)", i, second.Subjects[i], want[i])
			}
		}
	})
}

// TestTodo_POP_010_Property is the PROPERTY clause: for any membership size
// and any positive page size, a walk driven only by emitted tokens covers
// every member exactly once in sorted order - never losing, never
// duplicating.
func TestTodo_POP_010_Property(t *testing.T) {
	sizes := []int{0, 1, 2, 37, 1000}
	pageSizes := []int{1, 2, 3, 50, 500, 100000}
	for _, n := range sizes {
		for _, ps := range pageSizes {
			t.Run(fmt.Sprintf("n=%d_page=%d", n, ps), func(t *testing.T) {
				snap := frozenSnapshot(fmt.Sprintf("pop-prop-%d", n), n, false, values.Value(n))
				sess, err := popscale.NewSession(snap, fullCaller(population.CountDisclosureExact), ps)
				if err != nil {
					t.Fatalf("NewSession: %v", err)
				}
				var union []string
				token := ""
				for {
					page, err := sess.Page(token)
					if err != nil {
						t.Fatalf("Page(%q): %v", token, err)
					}
					union = append(union, page.Subjects...)
					if !page.Truncated {
						break
					}
					token = page.NextToken
				}
				if len(union) != n {
					t.Fatalf("walk covered %d members, want %d", len(union), n)
				}
				if !sort.StringsAreSorted(union) {
					t.Fatal("walk is not in sorted order")
				}
				for i := 1; i < len(union); i++ {
					if union[i] == union[i-1] {
						t.Fatalf("member %q served twice", union[i])
					}
				}
			})
		}
	}
}

// sharedWire reports whether two values serialize to identical bytes, which
// is the only sense in which one response can fail to distinguish another.
func sharedWire(t *testing.T, a, b any) {
	t.Helper()
	ra, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal %T: %v", a, err)
	}
	rb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal %T: %v", b, err)
	}
	if string(ra) != string(rb) {
		t.Fatalf("responses differ on the wire:\n%s\n%s", ra, rb)
	}
}

// TestTodo_POP_010_Security is the SECURITY clause: a caller that may not see
// a count and a population that is empty or unknown all receive the same,
// countless answer; a caller that may not see membership, a protected
// snapshot and an empty population all receive the same empty page; and a
// banded count never carries the exact number.
func TestTodo_POP_010_Security(t *testing.T) {
	full := frozenSnapshot("pop-security", 101, false, values.Value(101))
	empty := frozenSnapshot("pop-empty", 0, false, values.Value(0))
	unknown := frozenSnapshot("pop-unknown", 3, false, values.Value(3))
	unknown.Count = values.Unknown[int]("source_unavailable")
	protected := frozenSnapshot("pop-protected", 101, true, values.Value(101))

	t.Run("denied_empty_and_unknown_counts_are_the_same_wire_answer", func(t *testing.T) {
		hungry := popscale.Caller{CountAuthorized: true, CountView: population.CountDisclosureExact}
		relaxed := popscale.Caller{CountAuthorized: false, CountView: population.CountDisclosureSuppressed}

		deniedResp, err := popscale.Resolve(full, popscale.Request{Caller: relaxed, PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(denied): %v", err)
		}
		emptyResp, err := popscale.Resolve(empty, popscale.Request{Caller: hungry, PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(empty): %v", err)
		}
		unknownResp, err := popscale.Resolve(unknown, popscale.Request{Caller: hungry, PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(unknown): %v", err)
		}
		sharedWire(t, deniedResp.Count, emptyResp.Count)
		sharedWire(t, deniedResp.Count, unknownResp.Count)
		if deniedResp.Count.View != popscale.CountSuppressed {
			t.Fatalf("the shared answer is not suppressed: %+v", deniedResp.Count)
		}
		if deniedResp.Count.Exact != 0 || deniedResp.Count.Band != "" {
			t.Fatal("the suppressed answer carries count data")
		}
	})

	t.Run("denied_protected_and_empty_pages_are_the_same_wire_answer", func(t *testing.T) {
		deniedResp, err := popscale.Resolve(full, popscale.Request{Caller: popscale.Caller{}, PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(denied): %v", err)
		}
		protectedResp, err := popscale.Resolve(protected, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(protected): %v", err)
		}
		emptyResp, err := popscale.Resolve(empty, popscale.Request{Caller: fullCaller(population.CountDisclosureExact), PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(empty): %v", err)
		}
		sharedWire(t, deniedResp.Page, protectedResp.Page)
		sharedWire(t, deniedResp.Page, emptyResp.Page)
	})

	t.Run("an_authorized_banded_count_never_carries_the_exact_count", func(t *testing.T) {
		resp, err := popscale.Resolve(full, popscale.Request{Caller: fullCaller(population.CountDisclosureBanded), PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve(banded): %v", err)
		}
		if resp.Count.View != popscale.CountBanded || resp.Count.Band != "100-149" {
			t.Fatalf("banded count = %+v, want band 100-149", resp.Count)
		}
		raw, _ := json.Marshal(resp.Count)
		if strings.Contains(string(raw), "101") {
			t.Fatalf("banded wire answer leaks the exact count: %s", raw)
		}
	})

	t.Run("membership_without_count_authorization_serves_pages_but_no_number", func(t *testing.T) {
		caller := popscale.Caller{MembershipDisclosed: true, CountView: population.CountDisclosureSuppressed}
		resp, err := popscale.Resolve(full, popscale.Request{Caller: caller, PageSize: 50})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(resp.Page.Subjects) != 50 {
			t.Fatalf("page has %d subjects, want 50", len(resp.Page.Subjects))
		}
		if resp.Count.View != popscale.CountSuppressed {
			t.Fatalf("count view = %s, want SUPPRESSED", resp.Count.View)
		}
	})
}

// Declared limits for the benchmark clause: a representative multi-tenant
// bulk fixture must keep per-page resolution inside a small-millisecond
// budget, and one full multi-tenant walk must allocate inside a bounded heap
// budget.
const (
	benchTenants      = 3
	benchMembersPerT  = 20000
	benchPageSize     = 500
	declaredP95Page   = 25 * time.Millisecond
	declaredWalkHeap  = 64 << 20 // 64 MiB allocated per full multi-tenant walk
	batchMinWall      = 50 * time.Millisecond
	batchMaxWalks     = 2048
	benchPagesPerWalk = benchTenants * benchMembersPerT / benchPageSize
)

// batchAvgPage times a batch of full walks until the measured wall time is at
// least minWall (or maxWalks walks) and derives the average per-page time from
// the batch. Individual pages cost a few microseconds, which is below the
// tick of coarse clocks - on this Windows host every single Page() wall-time
// sample reads 0s - so the declared per-page budget can only be enforced over
// a batch: if the batch-derived average page time fits the budget, the walk is
// within it.
func batchAvgPage(walk func() int, minWall time.Duration, maxWalks int) (time.Duration, int, int) {
	var pages int
	walks := 0
	w0 := time.Now()
	for walks < maxWalks && (walks == 0 || time.Since(w0) < minWall) {
		pages += walk()
		walks++
	}
	elapsed := time.Since(w0)
	if pages == 0 {
		return 0, 0, walks
	}
	return elapsed / time.Duration(pages), pages, walks
}

func benchSnapshots() []population.Snapshot {
	snaps := make([]population.Snapshot, 0, benchTenants)
	for tenant := 0; tenant < benchTenants; tenant++ {
		defID := fmt.Sprintf("pop-bench-tenant-%d", tenant)
		s := frozenSnapshot(defID, benchMembersPerT, false, values.Value(benchMembersPerT))
		ids := s.SubjectIDs
		for i, id := range ids {
			ids[i] = fmt.Sprintf("tenant-%d-%s", tenant, id)
		}
		sort.Strings(ids)
		snaps = append(snaps, s)
	}
	return snaps
}

// TestTodo_POP_010_Benchmark_Once is the functional guard behind the
// benchmark: a real walk of the representative multi-tenant/bulk fixture must
// cover every member exactly once and stay inside the declared per-page and
// heap budgets.
func TestTodo_POP_010_Benchmark_Once(t *testing.T) {
	snaps := benchSnapshots()
	caller := fullCaller(population.CountDisclosureExact)

	sessions := make([]*popscale.Session, 0, len(snaps))
	for ti := range snaps {
		sess, err := popscale.NewSession(snaps[ti], caller, benchPageSize)
		if err != nil {
			t.Fatalf("NewSession(tenant %d): %v", ti, err)
		}
		sessions = append(sessions, sess)
	}
	walk := func() int {
		pages := 0
		for _, sess := range sessions {
			token := ""
			for {
				page, err := sess.Page(token)
				if err != nil {
					t.Fatalf("Page: %v", err)
				}
				pages++
				if !page.Truncated {
					break
				}
				token = page.NextToken
			}
		}
		return pages
	}

	// Exactly-once proof over one cold walk, with the heap budget measured
	// over that same walk.
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var union []string
	for _, sess := range sessions {
		token := ""
		for {
			page, err := sess.Page(token)
			if err != nil {
				t.Fatalf("Page: %v", err)
			}
			union = append(union, page.Subjects...)
			if !page.Truncated {
				break
			}
			token = page.NextToken
		}
	}
	runtime.ReadMemStats(&after)

	wantTotal := benchTenants * benchMembersPerT
	if len(union) != wantTotal {
		t.Fatalf("bulk walk covered %d members, want %d", len(union), wantTotal)
	}
	if got := len(mapOf(union)); got != wantTotal {
		t.Fatalf("bulk walk served %d distinct members, want %d (loss or duplication)", got, wantTotal)
	}
	heapDelta := int64(after.TotalAlloc - before.TotalAlloc)
	if heapDelta > declaredWalkHeap {
		t.Errorf("bulk walk allocated %d bytes, exceeds the declared limit %d", heapDelta, declaredWalkHeap)
	}

	// One un-timed walk warms the pages, then the per-page budget is enforced
	// over a timed batch of full walks.
	walk()
	avgPage, pages, walks := batchAvgPage(walk, batchMinWall, batchMaxWalks)
	if avgPage > declaredP95Page {
		t.Errorf("average per-page resolution %s exceeds the declared limit %s (%d walks, %d pages)", avgPage, declaredP95Page, walks, pages)
	}
	t.Logf("bulk walk: %d pages, avg page %s over %d walks, heap delta %d bytes", benchPagesPerWalk, avgPage, walks, heapDelta)
}

// BenchmarkTodo_POP_010 is the BENCHMARK clause: it benchmarks the same
// representative multi-tenant/bulk fixture the functional guard verifies and
// reports the batch-derived average per-page time as a metric.
func BenchmarkTodo_POP_010(b *testing.B) {
	snaps := benchSnapshots()
	caller := fullCaller(population.CountDisclosureExact)

	sessions := make([]*popscale.Session, 0, len(snaps))
	for ti := range snaps {
		sess, err := popscale.NewSession(snaps[ti], caller, benchPageSize)
		if err != nil {
			b.Fatalf("NewSession: %v", err)
		}
		sessions = append(sessions, sess)
	}
	walk := func() int {
		pages := 0
		for _, sess := range sessions {
			token := ""
			for {
				page, err := sess.Page(token)
				if err != nil {
					b.Fatalf("Page: %v", err)
				}
				pages++
				if !page.Truncated {
					break
				}
				token = page.NextToken
			}
		}
		return pages
	}

	// One un-timed walk warms the pages to steady state.
	if got := walk(); got != benchPagesPerWalk {
		b.Fatalf("warm walk served %d pages, want %d", got, benchPagesPerWalk)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := walk(); got != benchPagesPerWalk {
			b.Fatalf("walk %d served %d pages, want %d", i, got, benchPagesPerWalk)
		}
	}
	b.StopTimer()

	avgPage, pages, walks := batchAvgPage(walk, 20*time.Millisecond, 512)
	b.ReportMetric(float64(avgPage.Microseconds()), "avg-page/us")
	b.ReportMetric(float64(pages/walks), "pages/walk")
}

func joinPage(p popscale.Page) []byte { return []byte(strings.Join(p.Subjects, "\n")) }

func mapOf(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}
