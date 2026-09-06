package promotion

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTodo_CONFIG_003_Golden(t *testing.T) {
	p := testPackage(t, "golden", "production")
	v, err := Validate(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Simulate(p, v)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Approve(p, v, s, "reviewer", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	if err := r.Put(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Simulate(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Approve(p.ID, "reviewer", time.Unix(11, 0)); err != nil {
		t.Fatal(err)
	}
	active, err := r.ActivateAs(p.ID, "release-operator", time.Unix(12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if active.Evidence.Digest != active.Package.Bundle.Digest || active.Evidence.EvidenceDigest == "" || active.Evidence.EvidenceDigest == active.Package.Bundle.Digest || active.Evidence.ActivatedBy != "release-operator" || a.Digest != p.Bundle.Digest {
		t.Fatalf("activation evidence = %+v", active.Evidence)
	}
	second, ok := r.Get(p.ID)
	if !ok || !second.Evidence.ActivatedAt.Equal(time.Unix(12, 0).UTC()) {
		t.Fatalf("stored activation evidence = %+v, err=%v", second.Evidence, err)
	}
}

func TestTodo_CONFIG_003_Mutation(t *testing.T) {
	p := testPackage(t, "mutation", "production")
	v, err := Validate(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Simulate(p, v)
	if err != nil {
		t.Fatal(err)
	}
	p.Bundle.Bundle.Provenance = "changed-after-review"
	if _, err := Approve(p, v, s, "reviewer", time.Unix(1, 0)); !errors.Is(err, ErrStale) {
		t.Fatalf("changed package approval = %v", err)
	}
}

func TestTodo_CONFIG_003_Fault(t *testing.T) {
	p := testPackage(t, "fault", "production")
	v, err := Validate(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Simulate(p, v)
	if err != nil {
		t.Fatal(err)
	}
	s.Passed = false
	if _, err := Approve(p, v, s, "reviewer", time.Unix(1, 0)); !errors.Is(err, ErrNotSimulated) {
		t.Fatalf("failed simulation approval = %v", err)
	}
	if _, err := Approve(p, Validation{PackageID: p.ID, Digest: v.Digest, Compatible: false}, s, "reviewer", time.Unix(1, 0)); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("incompatible validation approval = %v", err)
	}
}

func TestTodo_CONFIG_003_Race(t *testing.T) {
	p := testPackage(t, "race", "production")
	r := NewRegistry()
	if err := r.Put(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Simulate(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Approve(p.ID, "reviewer", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Activate(p.ID, time.Unix(2, 0))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var successes, already int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyActive):
			already++
		default:
			t.Fatalf("concurrent activation error = %v", err)
		}
	}
	if successes != 1 || already != 1 {
		t.Fatalf("concurrent activation outcomes = successes %d already %d", successes, already)
	}
}
