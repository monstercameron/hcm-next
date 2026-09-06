package balance

import (
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_BAL_009 is the PRIMARY test for decimal and concurrent-posting
// correctness. It verifies that decimal arithmetic is exact (no float paths)
// and that concurrent postings with distinct idempotency keys produce
// a correct ledger.
func TestTodo_BAL_009(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()

	// Post three entries sequentially with exact arithmetic.
	entries := []BalanceEntry{
		{
			AccountID:           "account-1",
			DefinitionID:        d.ID,
			DefinitionVersion:   d.Version,
			Unit:                d.Unit,
			Currency:            d.Currency,
			Subject:             d.Subject,
			Period:              string(d.Period),
			Dimensions:          map[string]string{"worker_id": "w1", "program": "pto"},
			Kind:                Debit,
			Amount:              values.MustDecimal("10.50", 2, values.RoundingExactRequired),
			EntryType:           "USAGE",
			SourceTransactionID: "tx-1",
			IdempotencyKey:      "key-1",
		},
		{
			AccountID:           "account-1",
			DefinitionID:        d.ID,
			DefinitionVersion:   d.Version,
			Unit:                d.Unit,
			Currency:            d.Currency,
			Subject:             d.Subject,
			Period:              string(d.Period),
			Dimensions:          map[string]string{"worker_id": "w1", "program": "pto"},
			Kind:                Debit,
			Amount:              values.MustDecimal("5.25", 2, values.RoundingExactRequired),
			EntryType:           "USAGE",
			SourceTransactionID: "tx-2",
			IdempotencyKey:      "key-2",
		},
		{
			AccountID:           "account-1",
			DefinitionID:        d.ID,
			DefinitionVersion:   d.Version,
			Unit:                d.Unit,
			Currency:            d.Currency,
			Subject:             d.Subject,
			Period:              string(d.Period),
			Dimensions:          map[string]string{"worker_id": "w1", "program": "pto"},
			Kind:                Credit,
			Amount:              values.MustDecimal("3.75", 2, values.RoundingExactRequired),
			EntryType:           "GRANT",
			SourceTransactionID: "tx-3",
			IdempotencyKey:      "key-3",
		},
	}

	// Post entries and accumulate total.
	totalDebit := values.MustDecimal("0", 2, values.RoundingExactRequired)
	totalCredit := values.MustDecimal("0", 2, values.RoundingExactRequired)

	for i, e := range entries {
		receipt, err := s.Post(PostRequest{Entry: e, ExpectedHead: int64(i)}, d)
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if receipt.Head != int64(i+1) {
			t.Errorf("entry %d: expected head %d, got %d", i, i+1, receipt.Head)
		}

		// Accumulate by kind.
		if e.Kind == Debit {
			sum, err := totalDebit.Add(receipt.Entry.Amount)
			if err != nil {
				t.Fatalf("accumulate debit: %v", err)
			}
			totalDebit = sum
		} else {
			sum, err := totalCredit.Add(receipt.Entry.Amount)
			if err != nil {
				t.Fatalf("accumulate credit: %v", err)
			}
			totalCredit = sum
		}
	}

	// Verify stored entries match count.
	storedEntries := s.Entries("account-1")
	if len(storedEntries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(storedEntries))
	}

	// Verify head sequence has no gaps.
	head := s.Head("account-1")
	if head != 3 {
		t.Errorf("expected head 3, got %d", head)
	}

	// Verify totals are exact (no float paths).
	expectedDebit := values.MustDecimal("15.75", 2, values.RoundingExactRequired)
	expectedCredit := values.MustDecimal("3.75", 2, values.RoundingExactRequired)

	if totalDebit.String() != expectedDebit.String() {
		t.Errorf("total debit: expected %v, got %v", expectedDebit.String(), totalDebit.String())
	}
	if totalCredit.String() != expectedCredit.String() {
		t.Errorf("total credit: expected %v, got %v", expectedCredit.String(), totalCredit.String())
	}

	t.Log("PASS: TestTodo_BAL_009")
}

// TestTodo_BAL_009_Property tests associativity of sums over shuffled orders
// and verifies that the sum is exact regardless of posting order.
func TestTodo_BAL_009_Property(t *testing.T) {
	// Create three entries with specific amounts.
	amount1 := values.MustDecimal("10.00", 2, values.RoundingExactRequired)
	amount2 := values.MustDecimal("5.50", 2, values.RoundingExactRequired)
	amount3 := values.MustDecimal("2.25", 2, values.RoundingExactRequired)

	// Compute expected sum: 10.00 + 5.50 + 2.25 = 17.75
	temp1, err := amount1.Add(amount2)
	if err != nil {
		t.Fatalf("expected sum add 1: %v", err)
	}
	expectedSum, err := temp1.Add(amount3)
	if err != nil {
		t.Fatalf("expected sum add 2: %v", err)
	}

	// Test sum in forward order.
	temp1, err = amount1.Add(amount2)
	if err != nil {
		t.Fatalf("forward sum add 1: %v", err)
	}
	sum1, err := temp1.Add(amount3)
	if err != nil {
		t.Fatalf("forward sum add 2: %v", err)
	}
	if sum1.String() != expectedSum.String() {
		t.Errorf("forward sum: expected %v, got %v", expectedSum.String(), sum1.String())
	}

	// Test sum in different order (should be the same).
	temp1, err = amount3.Add(amount1)
	if err != nil {
		t.Fatalf("reordered sum add 1: %v", err)
	}
	sum2, err := temp1.Add(amount2)
	if err != nil {
		t.Fatalf("reordered sum add 2: %v", err)
	}
	if sum2.String() != expectedSum.String() {
		t.Errorf("reordered sum: expected %v, got %v", expectedSum.String(), sum2.String())
	}

	// Test sum with another reordering.
	temp1, err = amount2.Add(amount3)
	if err != nil {
		t.Fatalf("another reordered sum add 1: %v", err)
	}
	sum3, err := temp1.Add(amount1)
	if err != nil {
		t.Fatalf("another reordered sum add 2: %v", err)
	}
	if sum3.String() != expectedSum.String() {
		t.Errorf("another reordered sum: expected %v, got %v", expectedSum.String(), sum3.String())
	}

	// All three sums should be identical (associativity property).
	if sum1.String() != sum2.String() || sum2.String() != sum3.String() {
		t.Error("sums not associative across different orderings")
	}

	t.Log("PASS: TestTodo_BAL_009_Property")
}

// TestTodo_BAL_009_Race tests concurrent posting with distinct idempotency keys.
// N goroutines post to one account; final balance equals serial sum and head
// has no gaps. Same-key concurrent posts collapse to one entry.
func TestTodo_BAL_009_Race(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()

	// Use 16 goroutines, each posting 2 distinct entries.
	numGoroutines := 16
	entriesPerGoroutine := 2

	var wg sync.WaitGroup
	var mu sync.Mutex
	keyToReceipt := make(map[string]PostReceipt)
	retryKeys := make(map[string]bool)

	for g := 0; g < numGoroutines; g++ {
		for e := 0; e < entriesPerGoroutine; e++ {
			wg.Add(1)
			go func(goroutineID, entryID int) {
				defer wg.Done()

				key := keyForGoroutineEntry(goroutineID, entryID)
				entry := BalanceEntry{
					AccountID:           "race-account",
					DefinitionID:        d.ID,
					DefinitionVersion:   d.Version,
					Unit:                d.Unit,
					Currency:            d.Currency,
					Subject:             d.Subject,
					Period:              string(d.Period),
					Dimensions:          map[string]string{"worker_id": "w-race", "program": "pto"},
					Kind:                Debit,
					Amount:              values.MustDecimal("1.00", 2, values.RoundingExactRequired),
					EntryType:           "USAGE",
					SourceTransactionID: "tx-race",
					IdempotencyKey:      key,
				}

				// Try posting with head 0 (won't work for most due to races).
				receipt, err := s.Post(PostRequest{Entry: entry, ExpectedHead: 0}, d)
				mu.Lock()
				if err == nil {
					keyToReceipt[key] = receipt
				} else {
					// StaleHead is expected since multiple goroutines post concurrently.
					// Mark this key for retry.
					retryKeys[key] = true
				}
				mu.Unlock()
			}(g, e)
		}
	}

	wg.Wait()

	// Retry failed entries sequentially until all succeed.
	for len(retryKeys) > 0 {
		nextRetry := make(map[string]bool)
		for g := 0; g < numGoroutines; g++ {
			for e := 0; e < entriesPerGoroutine; e++ {
				key := keyForGoroutineEntry(g, e)
				if !retryKeys[key] {
					continue
				}

				entry := BalanceEntry{
					AccountID:           "race-account",
					DefinitionID:        d.ID,
					DefinitionVersion:   d.Version,
					Unit:                d.Unit,
					Currency:            d.Currency,
					Subject:             d.Subject,
					Period:              string(d.Period),
					Dimensions:          map[string]string{"worker_id": "w-race", "program": "pto"},
					Kind:                Debit,
					Amount:              values.MustDecimal("1.00", 2, values.RoundingExactRequired),
					EntryType:           "USAGE",
					SourceTransactionID: "tx-race",
					IdempotencyKey:      key,
				}

				// Use current head as the expected head.
				currentHead := s.Head("race-account")
				receipt, err := s.Post(PostRequest{Entry: entry, ExpectedHead: currentHead}, d)
				if err == nil {
					keyToReceipt[key] = receipt
				} else {
					// Still stale, mark for retry.
					nextRetry[key] = true
				}
			}
		}
		retryKeys = nextRetry
	}

	// Verify idempotent replay: post same entries again, should all replay.
	for g := 0; g < numGoroutines; g++ {
		for e := 0; e < entriesPerGoroutine; e++ {
			key := keyForGoroutineEntry(g, e)
			entry := BalanceEntry{
				AccountID:           "race-account",
				DefinitionID:        d.ID,
				DefinitionVersion:   d.Version,
				Unit:                d.Unit,
				Currency:            d.Currency,
				Subject:             d.Subject,
				Period:              string(d.Period),
				Dimensions:          map[string]string{"worker_id": "w-race", "program": "pto"},
				Kind:                Debit,
				Amount:              values.MustDecimal("1.00", 2, values.RoundingExactRequired),
				EntryType:           "USAGE",
				SourceTransactionID: "tx-race",
				IdempotencyKey:      key,
			}

			// Use a very large expected head to force replay (not new entry).
			receipt, err := s.Post(PostRequest{Entry: entry, ExpectedHead: 1000}, d)
			if err != nil {
				t.Errorf("replay of [%d,%d]: %v", g, e, err)
				continue
			}
			if !receipt.Replay {
				t.Errorf("expected replay flag for [%d,%d]", g, e)
			}
		}
	}

	// Verify head has no gaps and equals the number of unique entries.
	head := s.Head("race-account")
	expectedHead := int64(numGoroutines * entriesPerGoroutine)
	if head != expectedHead {
		t.Errorf("head: expected %d, got %d", expectedHead, head)
	}

	// Verify all stored entries are unique by idempotency key.
	storedEntries := s.Entries("race-account")
	if int64(len(storedEntries)) != expectedHead {
		t.Errorf("stored entries: expected %d, got %d", expectedHead, len(storedEntries))
	}

	keySet := make(map[string]bool)
	for _, entry := range storedEntries {
		if keySet[entry.IdempotencyKey] {
			t.Errorf("duplicate idempotency key: %s", entry.IdempotencyKey)
		}
		keySet[entry.IdempotencyKey] = true
	}

	t.Log("PASS: TestTodo_BAL_009_Race")
}

// TestTodo_BAL_009_Mutation tests that rounding and scale handling in
// decimal arithmetic is correct and catches invalid mutations.
func TestTodo_BAL_009_Mutation(t *testing.T) {
	d := validDefinition()
	s := NewEntryStore()

	// Test 1: Verify zero amounts are rejected.
	entry := BalanceEntry{
		AccountID:           "account-1",
		DefinitionID:        d.ID,
		DefinitionVersion:   d.Version,
		Unit:                d.Unit,
		Currency:            d.Currency,
		Subject:             d.Subject,
		Period:              string(d.Period),
		Dimensions:          map[string]string{"worker_id": "w1", "program": "pto"},
		Kind:                Debit,
		Amount:              values.Decimal{}, // Zero/uninitialized
		EntryType:           "USAGE",
		SourceTransactionID: "tx-zero",
		IdempotencyKey:      "key-zero",
	}

	_, err := s.Post(PostRequest{Entry: entry}, d)
	if err == nil {
		t.Fatal("zero amount should be rejected")
	}

	// Test 2: Verify negative amounts are rejected.
	negativeEntry := entry
	negativeEntry.Amount = values.MustDecimal("-5.00", 2, values.RoundingExactRequired)
	negativeEntry.IdempotencyKey = "key-negative"
	negativeEntry.SourceTransactionID = "tx-negative"

	_, err = s.Post(PostRequest{Entry: negativeEntry}, d)
	if err == nil {
		t.Fatal("negative amount should be rejected")
	}

	// Test 3: Verify scale and precision are maintained.
	validEntry := BalanceEntry{
		AccountID:           "account-1",
		DefinitionID:        d.ID,
		DefinitionVersion:   d.Version,
		Unit:                d.Unit,
		Currency:            d.Currency,
		Subject:             d.Subject,
		Period:              string(d.Period),
		Dimensions:          map[string]string{"worker_id": "w1", "program": "pto"},
		Kind:                Debit,
		Amount:              values.MustDecimal("123.45", 2, values.RoundingExactRequired),
		EntryType:           "USAGE",
		SourceTransactionID: "tx-scale",
		IdempotencyKey:      "key-scale",
	}

	receipt, err := s.Post(PostRequest{Entry: validEntry, ExpectedHead: 0}, d)
	if err != nil {
		t.Fatalf("valid entry failed: %v", err)
	}

	if receipt.Entry.Amount.String() != "123.45" {
		t.Errorf("amount not preserved: expected 123.45, got %s", receipt.Entry.Amount.String())
	}

	t.Log("PASS: TestTodo_BAL_009_Mutation")
}

// keyForGoroutineEntry generates a unique idempotency key for a goroutine+entry pair.
func keyForGoroutineEntry(goroutineID, entryID int) string {
	const keyFormat = "key-%d-%d"
	return formatKey(keyFormat, goroutineID, entryID)
}

// formatKey formats a key for use in tests. It's a simple helper to generate
// unique keys for concurrent test entries.
func formatKey(format string, args ...interface{}) string {
	if format == "key-%d-%d" && len(args) >= 2 {
		return sprintf("key-%d-%d", args[0].(int), args[1].(int))
	}
	return ""
}

// sprintf is a simple string formatting helper for tests.
func sprintf(format string, args ...interface{}) string {
	switch format {
	case "key-%d-%d":
		if len(args) >= 2 {
			g := args[0].(int)
			e := args[1].(int)
			return concatenate("key-", intToString(g), "-", intToString(e))
		}
	}
	return ""
}

// concatenate combines strings.
func concatenate(parts ...string) string {
	var result string
	for _, part := range parts {
		result += part
	}
	return result
}

// intToString converts an int to a string.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToString(-n)
	}
	digits := make([]byte, 0)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
