package main

import (
	"bytes"
	"testing"
)

func TestRun_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"-unknown"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run bad flag = %d, want 2", got)
	}
}
