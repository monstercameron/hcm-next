package balance

import (
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func instant(y int, m time.Month, d, hh, mm, ss int) values.Instant {
	return values.NewInstant(time.Date(y, m, d, hh, mm, ss, 0, time.UTC))
}

func decimal2(text string) values.Decimal {
	return values.MustDecimal(text, 2, values.RoundingExactRequired)
}

// bal003Entry builds one otherwise-valid PTO entry with explicit bitemporal
// instants, ready to post through a clocked EntryStore.
func bal003Entry(kind EntryKind, entryType, amount, idemKey string, effectiveAt, authorizedAt values.Instant) BalanceEntry {
	return BalanceEntry{
		AccountID:           "acct-1",
		DefinitionID:        "pto",
		DefinitionVersion:   "1.0.0",
		Unit:                "hour",
		Currency:            "USD",
		Subject:             "worker",
		Period:              string(PeriodCalendarYear),
		Dimensions:          map[string]string{"worker_id": "acct-1", "program": "pto"},
		Kind:                kind,
		Amount:              decimal2(amount),
		EntryType:           entryType,
		SourceTransactionID: "tx-" + idemKey,
		IdempotencyKey:      idemKey,
		EffectiveAt:         effectiveAt,
		AuthorizedAt:        authorizedAt,
	}
}

// bal003Ledger posts the fixed eight-entry scenario used by the PRIMARY,
// PROPERTY and GOLDEN tests, through a store whose clock advances once per
// successful post in exactly the order given, so each entry's stamped
// RecordedAt is deterministic and independently reviewable.
func bal003Ledger(t *testing.T) *EntryStore {
	t.Helper()
	d := validDefinition()
	recordedAt := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),  // e1
		time.Date(2026, 1, 15, 0, 0, 1, 0, time.UTC), // e2
		time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC),  // e3 (recorded early, authorized later)
		time.Date(2026, 1, 1, 0, 0, 3, 0, time.UTC),  // e4 (recorded early, effective later)
		time.Date(2026, 1, 15, 0, 0, 2, 0, time.UTC), // e5 (never authorized)
		time.Date(2026, 1, 15, 0, 0, 3, 0, time.UTC), // e6 (adjustment)
		time.Date(2026, 1, 22, 0, 0, 0, 0, time.UTC), // e7 (recorded after K1)
		time.Date(2026, 1, 1, 0, 0, 4, 0, time.UTC),  // e8 (exactly on the as-of boundary)
	}
	i := 0
	s := NewEntryStoreWithClock(func() time.Time {
		ts := recordedAt[i]
		i++
		return ts
	})

	t0 := instant(2026, 1, 1, 0, 0, 0)
	t1 := instant(2026, 1, 15, 0, 0, 0)
	t2 := instant(2026, 2, 1, 0, 0, 0)        // the as-of instant every test below queries
	t3 := instant(2026, 3, 1, 0, 0, 0)        // beyond the as-of instant
	authLate := instant(2026, 1, 25, 0, 0, 0) // between K1 and K2

	entries := []BalanceEntry{
		bal003Entry(Credit, "GRANT", "10.00", "e1", t0, t0),             // ordinary, authorized immediately
		bal003Entry(Debit, "USAGE", "2.50", "e2", t1, t1),               // ordinary, authorized immediately
		bal003Entry(Credit, "GRANT", "5.00", "e3", t1, authLate),        // authorized between K1 and K2
		bal003Entry(Debit, "USAGE", "1.00", "e4", t3, t0),               // effective after the as-of instant
		bal003Entry(Debit, "USAGE", "3.00", "e5", t1, values.Instant{}), // never authorized
		bal003Entry(Credit, "CORRECTION", "0.75", "e6", t1, t1),         // counted adjustment
		bal003Entry(Credit, "GRANT", "2.00", "e7", t1, t1),              // recorded after K1, before K2
		bal003Entry(Debit, "USAGE", "1.25", "e8", t2, t0),               // effective exactly on the as-of boundary
	}
	for idx, e := range entries {
		if _, err := s.Post(PostRequest{Entry: e, ExpectedHead: int64(idx)}, d); err != nil {
			t.Fatalf("post %s: %v", e.IdempotencyKey, err)
		}
	}
	return s
}

func bal003Request(knownAt values.Instant) AuthorizedBalanceRequest {
	return AuthorizedBalanceRequest{
		AccountID:     "acct-1",
		EffectiveAsOf: instant(2026, 2, 1, 0, 0, 0),
		KnownAt:       knownAt,
		Opening:       decimal2("0.00"),
		Scale:         2,
		Rounding:      values.RoundingExactRequired,
	}
}

func keysOf(result AuthorizedBalance) map[string]bool {
	out := map[string]bool{}
	for _, e := range result.Entries {
		out[e.IdempotencyKey] = true
	}
	for _, e := range result.Adjustments {
		out[e.IdempotencyKey] = true
	}
	return out
}

// TestTodo_BAL_003 is the PRIMARY test: an authorized balance calculated
// as-of 2026-02-01 and known-at two different instants must count and
// exclude the right entries for the right reasons, with the right ending
// balance and a non-empty digest. It directly proves the RED scenario: an
// entry effective after the as-of instant (e4) never leaks in, no matter how
// early it was recorded or authorized, and an entry authorized only in the
// future relative to known-at (e3) is excluded until known-at catches up.
func TestTodo_BAL_003(t *testing.T) {
	s := bal003Ledger(t)
	entries := s.Entries("acct-1")

	k1 := instant(2026, 1, 20, 0, 0, 0)
	got1, err := CalculateAuthorizedBalance(bal003Request(k1), entries)
	if err != nil {
		t.Fatalf("known-at k1: %v", err)
	}
	if got1.Ending.String() != "7.00" {
		t.Fatalf("known-at k1 ending = %s, want 7.00", got1.Ending.String())
	}
	wantEntries1 := []string{"e1", "e2", "e8"}
	wantAdjustments1 := []string{"e6"}
	if !sameKeys(got1.Entries, wantEntries1) {
		t.Fatalf("known-at k1 entries = %+v, want %v", got1.Entries, wantEntries1)
	}
	if !sameKeys(got1.Adjustments, wantAdjustments1) {
		t.Fatalf("known-at k1 adjustments = %+v, want %v", got1.Adjustments, wantAdjustments1)
	}
	wantExcluded1 := map[string]ExclusionReason{
		"e3": ExcludedNotAuthorized,
		"e4": ExcludedFutureEffective,
		"e5": ExcludedNotAuthorized,
		"e7": ExcludedNotRecorded,
	}
	assertExcluded(t, got1.Excluded, wantExcluded1)
	if got1.Digest == "" {
		t.Fatal("known-at k1: digest is empty")
	}

	k2 := instant(2026, 2, 5, 0, 0, 0)
	got2, err := CalculateAuthorizedBalance(bal003Request(k2), entries)
	if err != nil {
		t.Fatalf("known-at k2: %v", err)
	}
	if got2.Ending.String() != "14.00" {
		t.Fatalf("known-at k2 ending = %s, want 14.00", got2.Ending.String())
	}
	wantEntries2 := []string{"e1", "e2", "e3", "e7", "e8"}
	wantAdjustments2 := []string{"e6"}
	if !sameKeys(got2.Entries, wantEntries2) {
		t.Fatalf("known-at k2 entries = %+v, want %v", got2.Entries, wantEntries2)
	}
	if !sameKeys(got2.Adjustments, wantAdjustments2) {
		t.Fatalf("known-at k2 adjustments = %+v, want %v", got2.Adjustments, wantAdjustments2)
	}
	wantExcluded2 := map[string]ExclusionReason{
		"e4": ExcludedFutureEffective,
		"e5": ExcludedNotAuthorized,
	}
	assertExcluded(t, got2.Excluded, wantExcluded2)
	if got2.Digest == got1.Digest {
		t.Fatal("digests for two different known-at instants must not collide")
	}
}

func sameKeys(entries []BalanceEntry, want []string) bool {
	if len(entries) != len(want) {
		return false
	}
	for i, e := range entries {
		if e.IdempotencyKey != want[i] {
			return false
		}
	}
	return true
}

func assertExcluded(t *testing.T, got []ExcludedEntry, want map[string]ExclusionReason) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("excluded = %+v, want keys %v", got, want)
	}
	for _, x := range got {
		reason, ok := want[x.Entry.IdempotencyKey]
		if !ok {
			t.Fatalf("unexpected exclusion of %s: %s", x.Entry.IdempotencyKey, x.Reason)
		}
		if x.Reason != reason {
			t.Fatalf("entry %s excluded for %s, want %s", x.Entry.IdempotencyKey, x.Reason, reason)
		}
	}
}

// TestTodo_BAL_003_Property proves as-of/known-at monotonicity: the set of
// counted (Entries+Adjustments) idempotency keys only ever grows as
// known-at advances (holding as-of fixed), and only ever grows as as-of
// advances (holding known-at fixed at a point where every entry's own
// authorization/recording is already settled). A later known-at, or a later
// as-of, must never remove an entry an earlier instant had already counted.
func TestTodo_BAL_003_Property(t *testing.T) {
	s := bal003Ledger(t)
	entries := s.Entries("acct-1")

	knownAts := []values.Instant{
		instant(2026, 1, 10, 0, 0, 0),
		instant(2026, 1, 20, 0, 0, 0), // k1
		instant(2026, 1, 23, 0, 0, 0),
		instant(2026, 2, 5, 0, 0, 0), // k2
		instant(2026, 6, 1, 0, 0, 0), // far future: every recordable/authorizable fact is settled
	}
	var previous map[string]bool
	for _, k := range knownAts {
		res, err := CalculateAuthorizedBalance(bal003Request(k), entries)
		if err != nil {
			t.Fatalf("known-at %s: %v", k, err)
		}
		current := keysOf(res)
		if previous != nil {
			for key := range previous {
				if !current[key] {
					t.Fatalf("known-at %s dropped previously-counted entry %s", k, key)
				}
			}
		}
		previous = current
	}

	// Now hold known-at fixed far in the future (so authorization/recording
	// can never be the limiting factor) and advance as-of instead.
	farKnownAt := instant(2026, 6, 1, 0, 0, 0)
	asOfs := []values.Instant{
		instant(2026, 1, 10, 0, 0, 0),
		instant(2026, 1, 15, 0, 0, 0),
		instant(2026, 2, 1, 0, 0, 0),
		instant(2026, 3, 1, 0, 0, 0),
	}
	previous = nil
	for _, asOf := range asOfs {
		req := bal003Request(farKnownAt)
		req.EffectiveAsOf = asOf
		res, err := CalculateAuthorizedBalance(req, entries)
		if err != nil {
			t.Fatalf("as-of %s: %v", asOf, err)
		}
		current := keysOf(res)
		if previous != nil {
			for key := range previous {
				if !current[key] {
					t.Fatalf("as-of %s dropped previously-counted entry %s", asOf, key)
				}
			}
		}
		previous = current
	}
}

// TestTodo_BAL_003_Golden pins the stable, machine-readable shape of a
// calculation: the exclusion-reason wire tokens never drift, two
// independently-run calculations over identical inputs digest identically,
// and changing the counted amount changes the digest.
func TestTodo_BAL_003_Golden(t *testing.T) {
	wireTokens := map[ExclusionReason]string{
		ExcludedNotRecorded:     "NOT_RECORDED_BY_KNOWN_AT",
		ExcludedNoEffectiveTime: "EFFECTIVE_AT_UNSET",
		ExcludedFutureEffective: "EFFECTIVE_AFTER_AS_OF",
		ExcludedNotAuthorized:   "NOT_AUTHORIZED_BY_KNOWN_AT",
	}
	for reason, wire := range wireTokens {
		if string(reason) != wire {
			t.Fatalf("exclusion reason %v drifted from pinned wire token %q", reason, wire)
		}
	}

	s := bal003Ledger(t)
	entries := s.Entries("acct-1")
	req := bal003Request(instant(2026, 2, 5, 0, 0, 0))

	first, err := CalculateAuthorizedBalance(req, entries)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CalculateAuthorizedBalance(req, append([]BalanceEntry(nil), entries...))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("identical inputs digested differently: %q vs %q", first.Digest, second.Digest)
	}

	mutated := append([]BalanceEntry(nil), entries...)
	for i, e := range mutated {
		if e.IdempotencyKey == "e1" {
			e.Amount = decimal2("11.00")
			mutated[i] = e
		}
	}
	third, err := CalculateAuthorizedBalance(req, mutated)
	if err != nil {
		t.Fatal(err)
	}
	if third.Digest == first.Digest {
		t.Fatal("changing a counted entry's amount did not change the digest")
	}
}

// TestTodo_BAL_003_Race runs many concurrent calculations over the same
// immutable ledger and request; every goroutine must observe the identical
// ending balance and digest, proving the calculation has no shared mutable
// state.
func TestTodo_BAL_003_Race(t *testing.T) {
	s := bal003Ledger(t)
	entries := s.Entries("acct-1")
	req := bal003Request(instant(2026, 2, 5, 0, 0, 0))

	var wg sync.WaitGroup
	var mu sync.Mutex
	var endings []string
	var digests []string
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := CalculateAuthorizedBalance(req, entries)
			if err != nil {
				t.Errorf("concurrent calculate: %v", err)
				return
			}
			mu.Lock()
			endings = append(endings, res.Ending.String())
			digests = append(digests, res.Digest)
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Strings(endings)
	sort.Strings(digests)
	for i := 1; i < len(endings); i++ {
		if endings[i] != endings[0] {
			t.Fatalf("concurrent calculations disagree on ending: %v", endings)
		}
		if digests[i] != digests[0] {
			t.Fatalf("concurrent calculations disagree on digest: %v", digests)
		}
	}
}

// TestTodo_BAL_003_Mutation is table-driven: each mutation of an otherwise
// countable entry, or of the request itself, must be excluded (with the
// exact declared reason) or rejected outright -- never silently counted.
func TestTodo_BAL_003_Mutation(t *testing.T) {
	base := func() (AuthorizedBalanceRequest, BalanceEntry) {
		asOf := instant(2026, 2, 1, 0, 0, 0)
		knownAt := instant(2026, 2, 1, 0, 0, 0)
		req := AuthorizedBalanceRequest{
			AccountID: "acct-1", EffectiveAsOf: asOf, KnownAt: knownAt,
			Opening: decimal2("0.00"), Scale: 2, Rounding: values.RoundingExactRequired,
		}
		e := bal003Entry(Credit, "GRANT", "10.00", "e1", asOf, asOf)
		e.RecordedAt = asOf
		return req, e
	}

	t.Run("baseline_counts", func(t *testing.T) {
		req, e := base()
		res, err := CalculateAuthorizedBalance(req, []BalanceEntry{e})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Excluded) != 0 || res.Ending.String() != "10.00" {
			t.Fatalf("baseline should count: %+v", res)
		}
	})

	exclusionCases := []struct {
		name   string
		mutate func(*BalanceEntry)
		reason ExclusionReason
	}{
		{"never_authorized", func(e *BalanceEntry) { e.AuthorizedAt = values.Instant{} }, ExcludedNotAuthorized},
		{"authorized_in_future", func(e *BalanceEntry) { e.AuthorizedAt = instant(2026, 2, 2, 0, 0, 0) }, ExcludedNotAuthorized},
		{"recorded_in_future", func(e *BalanceEntry) { e.RecordedAt = instant(2026, 2, 2, 0, 0, 0) }, ExcludedNotRecorded},
		{"never_recorded", func(e *BalanceEntry) { e.RecordedAt = values.Instant{} }, ExcludedNotRecorded},
		{"no_effective_time", func(e *BalanceEntry) { e.EffectiveAt = values.Instant{} }, ExcludedNoEffectiveTime},
		{"effective_in_future", func(e *BalanceEntry) { e.EffectiveAt = instant(2026, 2, 2, 0, 0, 0) }, ExcludedFutureEffective},
	}
	for _, tc := range exclusionCases {
		t.Run(tc.name, func(t *testing.T) {
			req, e := base()
			tc.mutate(&e)
			res, err := CalculateAuthorizedBalance(req, []BalanceEntry{e})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Ending.String() != "0.00" {
				t.Fatalf("mutated entry counted: ending=%s", res.Ending.String())
			}
			if len(res.Excluded) != 1 || res.Excluded[0].Reason != tc.reason {
				t.Fatalf("excluded = %+v, want reason %s", res.Excluded, tc.reason)
			}
		})
	}

	errorCases := []struct {
		name   string
		mutate func(*AuthorizedBalanceRequest, *BalanceEntry)
	}{
		{"blank_account", func(r *AuthorizedBalanceRequest, e *BalanceEntry) { r.AccountID = "" }},
		{"scale_mismatch", func(r *AuthorizedBalanceRequest, e *BalanceEntry) {
			r.Opening = values.MustDecimal("0.000", 3, values.RoundingExactRequired)
		}},
		{"rounding_unspecified", func(r *AuthorizedBalanceRequest, e *BalanceEntry) { r.Rounding = values.RoundingUnspecified }},
		{"unset_as_of", func(r *AuthorizedBalanceRequest, e *BalanceEntry) { r.EffectiveAsOf = values.Instant{} }},
		{"unset_known_at", func(r *AuthorizedBalanceRequest, e *BalanceEntry) { r.KnownAt = values.Instant{} }},
		{"invalid_kind", func(r *AuthorizedBalanceRequest, e *BalanceEntry) { e.Kind = "OTHER" }},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			req, e := base()
			tc.mutate(&req, &e)
			if _, err := CalculateAuthorizedBalance(req, []BalanceEntry{e}); err == nil {
				t.Fatal("invalid request/entry accepted")
			} else if !errors.Is(err, ErrBalanceRequestInvalid) {
				t.Fatalf("error = %v, want ErrBalanceRequestInvalid", err)
			}
		})
	}
}
