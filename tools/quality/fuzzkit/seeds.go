package fuzzkit

// SeedCorpus returns the shared malformed envelope/schema/time/decimal
// seed inputs TOOL-013 requires. Both FuzzTodo_TOOL_013 (fixed parser,
// tools/quality/fuzzkit) and its counterpart in
// tools/quality/testdata/fuzzdefect (planted-defect parser) start from this
// exact list, so the same seed that proves the defective parser panics also
// proves the fixed parser does not.
func SeedCorpus() []string {
	return []string{
		// well-formed
		"tenant-1:promotion:1:12.34",
		"tenant-2:compensation:42:0.01",

		// empty / degenerate
		"",
		":::",
		"tenant-1:kind:1:",

		// malformed envelope shape
		"tenant-1:kind:1",
		"tenant-1:kind:1:12.34:extra",
		":kind:1:12.34",
		"tenant-1::1:12.34",

		// malformed schema/time-ish sequence field
		"tenant-1:kind:not-a-number:12.34",
		"tenant-1:kind:1.5:12.34",

		// malformed decimal field: this is the planted-defect trigger. The
		// defective parser (testdata/fuzzdefect) splits on "." and indexes
		// parts[1] unconditionally; a decimal field with no "." makes that
		// split return a single-element slice and panics on the index.
		"tenant-1:kind:1:notadecimal",
		"tenant-1:kind:1:12.34.56",
		"tenant-1:kind:1:.",
	}
}
