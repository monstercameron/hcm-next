package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs_RequiresExactlyOneMode(t *testing.T) {
	for _, args := range [][]string{{}, {"-changed", "-all"}, {"-all", "-pkg", "./x"}} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("args %v must be refused", args)
		}
	}
	o, err := parseArgs([]string{"-root", "r", "-pkg", "./a", "-pkg", "./b", "-timeout", "1m"})
	if err != nil || o.root != "r" || len(o.pkgs) != 2 || o.timeout.Minutes() != 1 {
		t.Fatalf("parsed %+v, %v", o, err)
	}
	if _, err := parseArgs([]string{"-bogus"}); err == nil {
		t.Fatal("unknown flag must be refused")
	}
}

func TestRun_ReportsUsageErrorsAndGatesARealPackage(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"-changed", "-all"}, &out); code != 2 || !strings.Contains(out.String(), "exactly one") {
		t.Fatalf("usage error: code=%d out=%q", code, out.String())
	}
	root := repoRoot(t)
	out.Reset()
	code := run([]string{"-root", root, "-pkg", "./tools/quality/covergate/testdata/gatedpkg", "-timeout", "5m"}, &out)
	if code != 0 || !strings.Contains(out.String(), "PASS") {
		t.Fatalf("real gate run: code=%d out=%q", code, out.String())
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
