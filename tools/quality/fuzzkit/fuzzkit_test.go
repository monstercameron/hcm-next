package fuzzkit

import "testing"

// FuzzTodo_TOOL_013 fuzzes the fixed, bounds-checked ParseEnvelope. A plain
// `go test ./tools/quality/fuzzkit/...` runs only the seed corpus below (no
// real mutation) and must never panic; TestTodo_TOOL_013
// (tools/quality/fuzz_test.go) separately runs this same seed corpus
// against the planted-defect parser in tools/quality/testdata/fuzzdefect in
// a subprocess with -fuzz enabled, and against this package with -fuzz
// enabled too, to prove both halves of the TOOL-013 RED/GREEN contract.
func FuzzTodo_TOOL_013(f *testing.F) {
	for _, seed := range SeedCorpus() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// ParseEnvelope must return an error for malformed input, never
		// panic. The fuzz target's only assertion is "did not panic";
		// `f.Fuzz` fails the run automatically if the function under test
		// panics.
		_, _ = ParseEnvelope(input)
	})
}
