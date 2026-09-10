package rolloutplan

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTodo_ROLLOUT_001_Golden(t *testing.T) {
	compiled, err := Compile(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalReport(compiled)
	if err != nil {
		t.Fatal(err)
	}
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func FuzzTodo_ROLLOUT_001(f *testing.F) {
	raw, err := json.Marshal(validPlan())
	if err != nil {
		f.Fatal(err)
	}
	seeds := []string{
		string(raw),
		`{}`,
		`{"artifact":{"type":"WORKFLOW","version":"v1.0.0"}}`,
		"",
		"not json",
		`{"artifact":{"type":"CRONJOB","version":"latest"},"target":""}`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		plan, err := DecodePlan(data)
		if err != nil {
			return
		}
		first := Validate(plan)
		second := Validate(plan)
		if len(first) != len(second) {
			t.Fatalf("nondeterministic findings: %d then %d", len(first), len(second))
		}
		for i := range first {
			if first[i].String() != second[i].String() {
				t.Fatalf("nondeterministic finding: %q then %q", first[i], second[i])
			}
			if first[i].Code == "" || first[i].Field == "" {
				t.Fatalf("unlocated finding: %+v", first[i])
			}
			if i > 0 && first[i].String() < first[i-1].String() {
				t.Fatalf("findings not sorted: %q before %q", first[i-1], first[i])
			}
		}
	})
}

func TestTodo_ROLLOUT_001_Integration(t *testing.T) {
	before, err := Compile(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalReport(before)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Compiled
	if err := json.Unmarshal(wire, &roundTrip); err != nil {
		t.Fatalf("compiled plan does not survive serialization: %v", err)
	}
	after, err := Compile(roundTrip.Plan)
	if err != nil {
		t.Fatalf("round-tripped plan rejected: %v", err)
	}
	if before.Digest != after.Digest {
		t.Fatalf("digest changed across round trip: %q vs %q", before.Digest, after.Digest)
	}
	encoded, err := json.Marshal(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlan(encoded)
	if err != nil {
		t.Fatalf("decode of valid plan failed: %v", err)
	}
	if findings := Validate(decoded); len(findings) != 0 {
		t.Fatalf("decoded valid plan rejected: %+v", findings)
	}
}
