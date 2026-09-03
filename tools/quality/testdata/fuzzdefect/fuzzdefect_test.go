package fuzzdefect

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/fuzzkit"
)

// FuzzTodo_TOOL_013 fuzzes the planted-defect ParseEnvelope using the exact
// same seed corpus as tools/quality/fuzzkit's FuzzTodo_TOOL_013. It is only
// ever invoked explicitly (by path) from TestTodo_TOOL_013
// (tools/quality/fuzz_test.go) with -fuzz enabled; because this package
// lives under testdata, it is never reached by a plain `go test ./...`
// sweep.
func FuzzTodo_TOOL_013(f *testing.F) {
	for _, seed := range fuzzkit.SeedCorpus() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParseEnvelope(input)
	})
}
