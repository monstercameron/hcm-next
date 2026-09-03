// Package racecheck provides a small, side-effect-free report model for race
// test qualification. It intentionally lives outside the runtime packages:
// race coverage is a quality concern and must not become a production
// dependency.
package racecheck

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner is the process boundary used by Check. Tests can inject a
// deterministic runner; the default runner executes the Go toolchain.
type CommandRunner func(context.Context, string, []string) ([]byte, error)

// PackageResult is the observed status of one requested package pattern.
type PackageResult struct {
	Pattern     string
	Eligible    bool // the toolchain can execute -race for this target
	Covered     bool // the requested race test completed successfully
	Unsupported bool
	Output      string
}

// Report is the complete result for a bounded set of concurrent packages.
type Report struct {
	Results []PackageResult
}

// Complete reports whether every requested package was race-eligible and
// passed. An empty request is invalid and therefore never complete.
func (r Report) Complete() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, result := range r.Results {
		if !result.Eligible || !result.Covered {
			return false
		}
	}
	return true
}

// Check runs a bounded race test for each package pattern. Unsupported
// toolchain/target combinations are represented explicitly instead of being
// treated as passing coverage. This lets Windows/arm64 report an honest skip
// while Linux CI still has to exercise the same targets.
func Check(ctx context.Context, root string, patterns []string, run CommandRunner) (Report, error) {
	if len(patterns) == 0 {
		return Report{}, errors.New("racecheck: at least one package pattern is required")
	}
	if run == nil {
		run = commandRunner
	}
	results := make([]PackageResult, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return Report{}, errors.New("racecheck: package pattern must not be empty")
		}
		output, err := run(ctx, root, []string{"test", "-race", "-count=1", pattern})
		text := string(output)
		unsupported := IsUnsupported(text)
		result := PackageResult{Pattern: pattern, Eligible: !unsupported, Covered: err == nil && !unsupported, Unsupported: unsupported, Output: text}
		results = append(results, result)
	}
	return Report{Results: results}, nil
}

func commandRunner(ctx context.Context, root string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	return cmd.CombinedOutput()
}

// IsUnsupported recognizes the toolchain's stable unsupported-race wording.
// Keep this deliberately broad across Go releases and platforms.
func IsUnsupported(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "-race is not supported") ||
		strings.Contains(lower, "race detector is not supported") ||
		strings.Contains(lower, "-race flag is not supported") ||
		(strings.Contains(lower, "not supported on") && strings.Contains(lower, "race"))
}

// Explain returns a concise human-readable report suitable for CI output.
func (r Report) Explain() string {
	if len(r.Results) == 0 {
		return "racecheck: no packages checked"
	}
	var b strings.Builder
	for _, result := range r.Results {
		status := "PASS"
		if result.Unsupported {
			status = "UNSUPPORTED"
		} else if !result.Covered {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "%s %s\n", status, result.Pattern)
	}
	return strings.TrimRight(b.String(), "\n")
}
