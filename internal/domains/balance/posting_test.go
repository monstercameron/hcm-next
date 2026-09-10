package balance

import (
	"errors"
	"sync"
	"testing"
)

func commitPlan(t *testing.T) PostingPlan {
	t.Helper()
	def := postingPlanDefinition()
	plan, err := PlanPosting(PostingPlanRequest{
		Authorized: postingAuthorizedBalance(t, "72.00"), Definition: def,
		Requested: postingEntry(t, def, Debit, "24.00", "commit-debit"),
	})
	if err != nil {
		t.Fatalf("PlanPosting: %v", err)
	}
	return plan
}

func commitTransaction(t *testing.T, plan PostingPlan) *BusinessTransaction {
	t.Helper()
	tx := NewBusinessTransaction()
	tx.SeedHead(plan.AccountID, plan.Opening.String())
	return tx
}

func TestTodo_BAL_012(t *testing.T) {
	plan := commitPlan(t)
	tx := commitTransaction(t, plan)
	receipt, err := tx.CommitPosting(plan, "commit:leave:w1:1", nil)
	if err != nil {
		t.Fatalf("CommitPosting: %v", err)
	}
	// Receipt identifies opening/ending heads and unused remainder.
	if receipt.OpeningHead != plan.Opening.String() || receipt.EndingHead != plan.Ending.String() || receipt.UnusedRemainder != plan.Remainder.String() || receipt.Entries != len(plan.Entries) {
		t.Fatalf("receipt=%+v", receipt)
	}
	head, ok := tx.Head(plan.AccountID)
	if !ok || head != plan.Ending.String() {
		t.Fatalf("head=%q ok=%v", head, ok)
	}
	if err := receipt.Verify(plan, "commit:leave:w1:1"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Repeated transactions return the original receipt: no double debit.
	again, err := tx.CommitPosting(plan, "commit:leave:w1:1", nil)
	if err != nil || again.Digest != receipt.Digest {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	head, _ = tx.Head(plan.AccountID)
	if head != plan.Ending.String() {
		t.Fatal("repeated transaction double-debited")
	}
	// RED: stale heads, changed plans and mid-plan failpoints refuse with
	// nothing committed.
	staleTx := NewBusinessTransaction()
	staleTx.SeedHead(plan.AccountID, "0.00")
	if _, err := staleTx.CommitPosting(plan, "commit:stale", nil); err == nil {
		t.Fatal("stale head committed")
	}
	changed := plan
	changed.Ending = plan.Opening
	if _, err := tx.CommitPosting(changed, "commit:changed", nil); err == nil {
		t.Fatal("changed plan committed")
	}
	failpoint := commitTransaction(t, plan)
	if _, err := failpoint.CommitPosting(plan, "commit:fail", func(step string) error {
		if step == "entry:0" {
			return errors.New("injected fault between leave fact and entry")
		}
		return nil
	}); err == nil {
		t.Fatal("failpoint committed")
	}
	head, _ = failpoint.Head(plan.AccountID)
	if head != plan.Opening.String() {
		t.Fatalf("failpoint left a partial debit: %s", head)
	}
}

func TestTodo_BAL_012_Property(t *testing.T) {
	plan := commitPlan(t)
	tx := commitTransaction(t, plan)
	first, err := tx.CommitPosting(plan, "commit:prop", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Once-or-none: the head lands on the plan ending exactly once.
	head, _ := tx.Head(plan.AccountID)
	if head != plan.Ending.String() {
		t.Fatalf("head = %s", head)
	}
	second, err := tx.CommitPosting(plan, "commit:prop", nil)
	if err != nil || second.Digest != first.Digest {
		t.Fatal("idempotent recommit diverged")
	}
}

func TestTodo_BAL_012_Race(t *testing.T) {
	plan := commitPlan(t)
	tx := commitTransaction(t, plan)
	const workers = 16
	var wg sync.WaitGroup
	receipts := make([]PostingReceipt, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipts[i], errs[i] = tx.CommitPosting(plan, "commit:race", nil)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if receipts[i].Digest != receipts[0].Digest {
			t.Fatalf("worker %d committed a second debit", i)
		}
	}
	head, _ := tx.Head(plan.AccountID)
	if head != plan.Ending.String() {
		t.Fatalf("raced head = %s", head)
	}
}

func TestTodo_BAL_012_Mutation(t *testing.T) {
	plan := commitPlan(t)
	tx := commitTransaction(t, plan)
	base, err := tx.CommitPosting(plan, "commit:base", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A different request plans and commits under its own receipt.
	def := postingPlanDefinition()
	other, err := PlanPosting(PostingPlanRequest{
		Authorized: postingAuthorizedBalance(t, "72.00"), Definition: def,
		Requested: postingEntry(t, def, Debit, "8.00", "commit-debit-2"),
	})
	if err != nil {
		t.Fatalf("PlanPosting: %v", err)
	}
	tx2 := commitTransaction(t, other)
	recomputed, err := tx2.CommitPosting(other, "commit:other", nil)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed.Digest == base.Digest {
		t.Fatal("changed plan kept the receipt")
	}
	// Forged receipts never verify.
	if err := base.Verify(plan, "commit:wrong-key"); err == nil {
		t.Fatal("forged receipt verified")
	}
}
