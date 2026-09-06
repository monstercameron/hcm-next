package replay

import (
	"context"
	"errors"
	"testing"
)

// TestMemorySource_ClonesInBothDirections is the property the PROPERTY case
// leans on: neither the caller that built the record nor a caller that reads
// one back can change what a later Load returns.
func TestMemorySource_ClonesInBothDirections(t *testing.T) {
	rec := minimalRecord()
	src := NewMemorySource(rec)

	// Mutating the record handed in.
	rec.Nodes[0].NodeID = "mutated-by-the-builder"
	loaded, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Nodes[0].NodeID != "a" {
		t.Fatalf("the builder's mutation reached the source")
	}

	// Mutating a loaded copy.
	loaded.Nodes[0].NodeID = "mutated-by-a-reader"
	again, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if again.Nodes[0].NodeID != "a" {
		t.Fatalf("a reader's mutation reached the source")
	}

	// And through the exported accessor.
	held := src.Record()
	held.Nodes[0].NodeID = "mutated-through-Record"
	if got, _ := src.Load(context.Background()); got.Nodes[0].NodeID != "a" {
		t.Fatalf("a mutation through Record reached the source")
	}
}

// TestMemorySource_NilIsARefusalNotAPanic keeps a zero-value source from
// crashing a caller that built one wrong.
func TestMemorySource_NilIsARefusalNotAPanic(t *testing.T) {
	var src *MemorySource
	_, err := src.Load(context.Background())
	if CodeOf(err) != CodeSourceFailed {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeSourceFailed)
	}
}

// TestSourceFunc_Adapts covers the function adapter the fault fixtures use.
func TestSourceFunc_Adapts(t *testing.T) {
	want := minimalRecord()
	var s Source = sourceFunc(func(context.Context) (Record, error) { return want, nil })
	got, err := s.Load(context.Background())
	if err != nil || got.WorkflowID != want.WorkflowID {
		t.Fatalf("got %+v err %v", got.WorkflowID, err)
	}

	boom := errors.New("down")
	s = sourceFunc(func(context.Context) (Record, error) { return Record{}, boom })
	if _, err := s.Load(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
