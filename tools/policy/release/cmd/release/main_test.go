package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"unknown"}, &stdout, &stderr); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q, want unknown command", stderr.String())
	}
}

func TestRunBundleRejectsMalformedBinaryFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"bundle", "-out", "out", "-binary", "not-an-assignment"}, &stdout, &stderr); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "name=path") {
		t.Fatalf("stderr = %q, want name=path", stderr.String())
	}
}

func TestRunVerifyRequiresBundle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"verify"}, &stdout, &stderr); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "-bundle is required") {
		t.Fatalf("stderr = %q, want missing bundle", stderr.String())
	}
}
