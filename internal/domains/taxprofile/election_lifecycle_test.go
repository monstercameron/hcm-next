package taxprofile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTaxElectionChangeRequiresEvidenceEffectiveDateAndAppendOnlyCorrection(t *testing.T) {
	original := validElection(t)
	check := ValidateElectionAt(original, taxInstant(t, "2026-06-01T00:00:00Z"))
	if check.Status != ElectionValidationValid {
		t.Fatalf("valid election status = %s", check.Status)
	}
	missing := original
	missing.FormRevisionRef = ""
	if got := ValidateElectionAt(missing, taxInstant(t, "2026-06-01T00:00:00Z")); got.Status != ElectionValidationIncomplete {
		t.Fatalf("missing form status = %s", got.Status)
	}
	successor := original
	successor.Kind = ElectionAdditionalAmount
	successor.Amount = decimalForTaxTest(t, "25.00")
	successor.Effective = taxInterval(t, "2025-12-01T00:00:00Z", "")
	successor.KnownAt = taxInstant(t, "2026-06-05T00:00:00Z")
	successor, err := NewWithholdingElectionRevision(successor)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := CorrectElection(original, successor, true)
	if err != nil || !intent.Retroactive || intent.PayrollAction != "RETROACTIVE_PAYROLL_CORRECTION_REQUIRED" || intent.Digest == "" {
		t.Fatalf("correction intent = %+v, err=%v", intent, err)
	}
	store := NewMemoryStore()
	if err := store.SaveElection(t.Context(), "tenant-a", original); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveElectionSuccessor(t.Context(), "tenant-a", original.CanonicalDigest, successor); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadElection(t.Context(), "tenant-a", original.ElectionID)
	if err != nil || loaded.CanonicalDigest != successor.CanonicalDigest || len(store.elections) != 2 {
		t.Fatalf("current successor/original retention = %+v rows=%d, err=%v", loaded, len(store.elections), err)
	}
	if _, err := store.LoadElection(t.Context(), "tenant-a", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing election err = %v", err)
	}
}

func decimalForTaxTest(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTodo_TAXPROFILE_002_Property(t *testing.T) {
	for day := 1; day <= 20; day++ {
		start := taxInstant(t, fmt.Sprintf("2026-01-%02dT00:00:00Z", day))
		end := taxInstant(t, fmt.Sprintf("2026-01-%02dT00:00:00Z", day+1))
		e := validElection(t)
		e.Effective, _ = values.NewInstantInterval(start, end)
		e, err := NewWithholdingElectionRevision(e)
		if err != nil {
			t.Fatal(err)
		}
		if got := ValidateElectionAt(e, start); got.Status != ElectionValidationValid {
			t.Fatalf("day %d start status = %s", day, got.Status)
		}
		if got := ValidateElectionAt(e, end); got.Status != ElectionValidationExpired {
			t.Fatalf("day %d end status = %s", day, got.Status)
		}
	}
	current := validElection(t)
	backdated := current
	backdated.Effective = taxInterval(t, "2025-12-01T00:00:00Z", "")
	backdated.KnownAt = taxInstant(t, "2026-02-01T00:00:00Z")
	backdated.Kind = ElectionMultipleJobs
	backdated, _ = NewWithholdingElectionRevision(backdated)
	for _, input := range [][]WithholdingElectionRevision{{current, backdated}, {backdated, current}} {
		resolved, err := ResolveEffectiveElection(input, taxInstant(t, "2026-06-01T00:00:00Z"))
		if err != nil || resolved.CanonicalDigest != backdated.CanonicalDigest {
			t.Fatalf("permutation resolved %+v, err=%v", resolved, err)
		}
	}
}

func TestTodo_TAXPROFILE_002_Golden(t *testing.T) {
	e := validElection(t)
	next := e
	next.Kind = ElectionAdditionalAmount
	next.Amount = decimalForTaxTest(t, "10.00")
	var err error
	next, err = NewWithholdingElectionRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	a, err := CorrectElection(e, next, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", a.Canonical()); got != "0724736368656d613368636d6e6578742e646f6d61696e732e74617870726f66696c652e456c656374696f6e436f7272656374696f6e496e74656e740f24736368656d615f76657273696f6e01020f6f726967696e616c5f646967657374477368613235363a6130326666313332396263393231393162353933623837343565623837396461346138353434336461323066336338663665623434323566313330633863663610737563636573736f725f646967657374477368613235363a35656263323766633832323533633037366266376235656136383431666232346230323637666233343136396433626533346435616631303361393530336632096566666563746976652105020001000000006955b9000000000000000000000000000000000000000000000b726574726f61637469766501010e706179726f6c6c5f616374696f6e27524554524f4143544956455f504159524f4c4c5f434f5252454354494f4e5f5245515549524544" {
		t.Fatalf("canonical bytes = %s; digest=%s", got, a.Digest)
	}
	if a.Digest != "sha256:4cd07f9b6f1c820b299062c3f12f05583c8d5efd1832a8b569c5cbd0c7eadb80" {
		t.Fatalf("canonical digest = %s", a.Digest)
	}
	if !a.Retroactive || a.PayrollAction != "RETROACTIVE_PAYROLL_CORRECTION_REQUIRED" {
		t.Fatalf("closed-payroll correction = %+v", a)
	}
}

func TestTodo_TAXPROFILE_002_Race(t *testing.T) {
	e := validElection(t)
	store := NewMemoryStore()
	if err := store.SaveElection(t.Context(), "tenant-a", e); err != nil {
		t.Fatal(err)
	}
	successors := make([]WithholdingElectionRevision, 8)
	for i := 0; i < 8; i++ {
		next := e
		next.Effective = taxInterval(t, fmt.Sprintf("2025-01-%02dT00:00:00Z", i+1), "")
		next.KnownAt = taxInstant(t, "2026-06-02T00:00:00Z")
		var err error
		successors[i], err = NewWithholdingElectionRevision(next)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, len(successors))
	for _, successor := range successors {
		wg.Add(1)
		go func(candidate WithholdingElectionRevision) {
			defer wg.Done()
			results <- store.SaveElectionSuccessor(context.Background(), "tenant-a", e.CanonicalDigest, candidate)
		}(successor)
	}
	wg.Wait()
	close(results)
	accepted, stale := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrStoreStaleCAS) {
			stale++
		} else {
			t.Fatalf("unexpected CAS result: %v", err)
		}
	}
	if accepted != 1 || stale != 7 {
		t.Fatalf("CAS results accepted=%d stale=%d", accepted, stale)
	}
}

func TestTodo_TAXPROFILE_002_Fault(t *testing.T) {
	e := validElection(t)
	if got := ValidateElectionAt(e, values.Instant{}); got.Status != ElectionValidationUnknown {
		t.Fatalf("unset instant status = %s", got.Status)
	}
	if _, err := ResolveEffectiveElection(nil, taxInstant(t, "2026-06-01T00:00:00Z")); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("no election err = %v", err)
	}
	if got := ValidateElectionAt(e, taxInstant(t, "2025-01-01T00:00:00Z")); got.Status != ElectionValidationUnknown {
		t.Fatalf("before effective status = %s", got.Status)
	}
	ended := e
	ended.Effective = taxInterval(t, "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z")
	ended, _ = NewWithholdingElectionRevision(ended)
	if got := ValidateElectionAt(ended, taxInstant(t, "2026-03-01T00:00:00Z")); got.Status != ElectionValidationExpired {
		t.Fatalf("expired status = %s", got.Status)
	}
	if _, err := CorrectElection(WithholdingElectionRevision{}, e, false); !errors.Is(err, ErrElectionCorrection) {
		t.Fatalf("invalid original err = %v", err)
	}
}

func TestTodo_TAXPROFILE_002_Security(t *testing.T) {
	e := validElection(t)
	other := e
	other.ElectionID = "other"
	other, _ = NewWithholdingElectionRevision(other)
	if _, err := CorrectElection(e, other, false); !errors.Is(err, ErrElectionCorrection) {
		t.Fatalf("identity change err = %v", err)
	}
	store := NewMemoryStore()
	if err := store.SaveElection(t.Context(), "tenant-a", e); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveElectionSuccessor(t.Context(), "tenant-b", e.CanonicalDigest, other); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("cross-tenant predecessor err = %v", err)
	}
}

func TestTodo_TAXPROFILE_002_Conformance(t *testing.T) {
	e := validElection(t)
	if _, err := ResolveEffectiveElection([]WithholdingElectionRevision{e}, taxInstant(t, "2026-06-01T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	duplicate := e
	duplicate.Kind = ElectionAdditionalAmount
	duplicate, _ = NewWithholdingElectionRevision(duplicate)
	if _, err := ResolveEffectiveElection([]WithholdingElectionRevision{e, duplicate}, taxInstant(t, "2026-06-01T00:00:00Z")); !errors.Is(err, ErrElectionAmbiguous) {
		t.Fatalf("same effective date err = %v", err)
	}
	mixedWorker := e
	mixedWorker.WorkerRef = "worker-other"
	mixedWorker.Effective = taxInterval(t, "2026-02-01T00:00:00Z", "")
	mixedWorker.KnownAt = taxInstant(t, "2026-02-02T00:00:00Z")
	mixedWorker, _ = NewWithholdingElectionRevision(mixedWorker)
	if _, err := ResolveEffectiveElection([]WithholdingElectionRevision{e, mixedWorker}, taxInstant(t, "2026-06-01T00:00:00Z")); !errors.Is(err, ErrElectionCorrection) {
		t.Fatalf("mixed worker scope err = %v", err)
	}
	mixedJurisdiction := mixedWorker
	mixedJurisdiction.WorkerRef = e.WorkerRef
	mixedJurisdiction.Jurisdiction = "US-NY"
	mixedJurisdiction, _ = NewWithholdingElectionRevision(mixedJurisdiction)
	if _, err := ResolveEffectiveElection([]WithholdingElectionRevision{e, mixedJurisdiction}, taxInstant(t, "2026-06-01T00:00:00Z")); !errors.Is(err, ErrElectionCorrection) {
		t.Fatalf("mixed jurisdiction scope err = %v", err)
	}
	mixedElection := mixedWorker
	mixedElection.WorkerRef = e.WorkerRef
	mixedElection.ElectionID = "election-other"
	mixedElection, _ = NewWithholdingElectionRevision(mixedElection)
	if _, err := ResolveEffectiveElection([]WithholdingElectionRevision{e, mixedElection}, taxInstant(t, "2026-06-01T00:00:00Z")); !errors.Is(err, ErrElectionCorrection) {
		t.Fatalf("mixed election scope err = %v", err)
	}
	invalidNewer := e
	invalidNewer.Effective = taxInterval(t, "2026-02-01T00:00:00Z", "")
	invalidNewer.KnownAt = taxInstant(t, "2026-02-02T00:00:00Z")
	invalidNewer.EvidenceRef = ""
	if _, err := ResolveEffectiveElection([]WithholdingElectionRevision{e, invalidNewer}, taxInstant(t, "2026-06-01T00:00:00Z")); !errors.Is(err, ErrElectionCorrection) {
		t.Fatalf("invalid newer election err = %v", err)
	}
	backdated := e
	backdated.Effective = taxInterval(t, "2025-12-01T00:00:00Z", "")
	backdated.KnownAt = taxInstant(t, "2026-02-02T00:00:00Z")
	backdated.Kind = ElectionMultipleJobs
	backdated, _ = NewWithholdingElectionRevision(backdated)
	resolved, err := ResolveEffectiveElection([]WithholdingElectionRevision{e, backdated}, taxInstant(t, "2026-06-01T00:00:00Z"))
	if err != nil || resolved.CanonicalDigest != backdated.CanonicalDigest {
		t.Fatalf("backdated current resolution = %+v, err=%v", resolved, err)
	}
}

func TestTodo_TAXPROFILE_002_Mutation(t *testing.T) {
	e := validElection(t)
	old := e.CanonicalDigest
	intent, err := CorrectElection(e, e, false)
	if err == nil || intent.Digest != "" || e.CanonicalDigest != old {
		t.Fatal("original election was mutated or equal correction accepted")
	}
	rawOriginal, rawSuccessor := e, e
	rawOriginal.CanonicalDigest = ""
	rawSuccessor.CanonicalDigest = ""
	rawSuccessor.Kind = ElectionMultipleJobs
	intent, err = CorrectElection(rawOriginal, rawSuccessor, false)
	if err != nil || intent.OriginalDigest == "" || intent.SuccessorDigest == "" || intent.OriginalDigest == intent.SuccessorDigest {
		t.Fatalf("raw correction pins = %+v, err=%v", intent, err)
	}
}

func TestTodo_TAXPROFILE_002_FaultReplayAndValidation(t *testing.T) {
	e := validElection(t)
	store := NewMemoryStore()
	if err := store.SaveElection(t.Context(), "tenant-a", e); err != nil {
		t.Fatal(err)
	}
	next := e
	next.Effective = taxInterval(t, "2025-12-01T00:00:00Z", "")
	next.KnownAt = taxInstant(t, "2026-06-02T00:00:00Z")
	next, _ = NewWithholdingElectionRevision(next)
	if err := store.SaveElection(t.Context(), "tenant-a", next); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("unfenced successor err = %v", err)
	}
	if err := store.SaveElectionSuccessor(t.Context(), "tenant-a", e.CanonicalDigest, next); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveElectionSuccessor(t.Context(), "tenant-a", e.CanonicalDigest, next); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("replayed predecessor err = %v", err)
	}
	forged := next
	forged.CanonicalDigest = "forged"
	if err := store.SaveElectionSuccessor(t.Context(), "tenant-a", next.CanonicalDigest, forged); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("forged successor err = %v", err)
	}
}
