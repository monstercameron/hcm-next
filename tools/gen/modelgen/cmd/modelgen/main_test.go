package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunWritesGeneratedFile exercises run against the real repository
// (the working directory a `go test` invocation of this package uses is
// this cmd/modelgen directory, which sits under the real repo root), so it
// both proves run succeeds and leaves the checked-in gen/go/hcmnext/model
// file exactly as tools/gen/modelgen's own drift test expects — run
// regenerates byte-identical output, so this is idempotent with the
// checked-in file already in the tree.
func TestRunWritesGeneratedFile(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(&out, &errOut)
	if code != 0 {
		t.Fatalf("run exit code = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "wrote") {
		t.Fatalf("run stdout missing confirmation: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("run wrote to stderr on success: %q", errOut.String())
	}
}
