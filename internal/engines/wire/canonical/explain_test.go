package canonical

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestExplain_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestExplain_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestExplain_ReportsStableContributorsAndErrors(t *testing.T) {
	contributions := Explanation{Contributions: []Contribution{{Path: "z", Kind: "string"}, {Path: "a", Kind: "absent"}, {Path: "z", Kind: "string"}, {Path: "b", Kind: "map"}}}.ContributingPaths()
	if len(contributions) != 2 || contributions[0] != "b" || contributions[1] != "z" {
		t.Fatalf("ContributingPaths = %v", contributions)
	}
	profile := Profile{ID: "struct", Version: 1, SchemaID: "schema", SchemaVersion: 1, MessageName: "google.protobuf.Struct", Material: []string{"fields"}}
	msg := &structpb.Struct{Fields: map[string]*structpb.Value{"x": structpb.NewStringValue("value")}}
	x, encoded, err := Explain(msg, profile)
	if err != nil || len(encoded) == 0 || x.ProfileID != profile.ID || x.CanonicalLength != len(encoded) || len(x.Contributions) == 0 {
		t.Fatalf("Explain = %+v, %x, %v", x, encoded, err)
	}
	plan, err := Compile(profile)
	if err != nil {
		t.Fatal(err)
	}
	x2, encoded2, err := plan.Explain(msg)
	if err != nil || string(encoded) != string(encoded2) || x2.CanonicalLength != x.CanonicalLength {
		t.Fatalf("Plan.Explain = %+v, %v", x2, err)
	}
	if _, _, err := Explain(nil, profile); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("nil Explain error = %v", err)
	}
	if _, _, err := plan.Explain(nil); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("nil Plan.Explain error = %v", err)
	}
}
