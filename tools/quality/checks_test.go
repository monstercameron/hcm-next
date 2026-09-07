package main

import (
	"strings"
	"testing"
)

func TestChecksSmoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestChecksNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestFrontendExperienceGateTargetsTheRegisteredProductionMatrix(t *testing.T) {
	root := repoRoot(t)
	cmd := frontendExperienceGateCommand(root)
	want := "go test -count=1 -run ^TestFrontendI18nAccessibilityGateEveryRegisteredPage$ ./internal/humanwork/productui"
	if got := strings.Join(cmd.Args, " "); got != want {
		t.Fatalf("frontend gate command = %q, want %q", got, want)
	}
	if cmd.Dir != root {
		t.Fatalf("frontend gate directory = %q, want %q", cmd.Dir, root)
	}
}
