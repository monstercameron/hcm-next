// Package covergate is the unit-test and coverage floor gate for the root Go
// module. It runs `go test -cover` on a set of packages (the packages touched
// by the staged files before a commit, or every package in CI), parses the
// per-package result lines, and refuses when any test fails, any package has
// no test files, or any package covers fewer statements than the floor in
// definitions/toolchain/coverage-gate.yaml. Every waiver is an explicit,
// owner-bearing, expiring exception in that file; nothing is waived by
// prefix or by default.
//
// The gate judges by the result lines go test prints, not by its exit code:
// on this project's Windows hosts go test exits non-zero when it cannot
// unlink its own test binary even though every package printed ok.
package covergate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Module is the root module path every package result is reported under.
const Module = "github.com/monstercameron/hcm-next"

// DefaultConfigPath is the gate's configuration, relative to the repo root.
const DefaultConfigPath = "definitions/toolchain/coverage-gate.yaml"

// Exception kinds.
const (
	KindNoTests    = "no_tests"
	KindBelowFloor = "below_floor"
)

// Finding kinds.
const (
	FindingTestFailure = "test_failure"
	FindingNoTests     = "no_tests"
	FindingBelowFloor  = "below_floor"
)

// Config is the checked-in gate policy.
type Config struct {
	// Threshold is the statement-coverage floor in percent, exclusive of
	// nothing: a package at exactly the threshold passes.
	Threshold  float64     `yaml:"threshold"`
	Exceptions []Exception `yaml:"exceptions"`
}

// Exception waives one finding kind for one exact package until Expiry.
type Exception struct {
	Package string `yaml:"package"`
	Kind    string `yaml:"kind"`
	Owner   string `yaml:"owner"`
	Reason  string `yaml:"reason"`
	Expiry  string `yaml:"expiry"`
}

// Result is one package's parsed go test line.
type Result struct {
	Package     string
	Status      string // ok, fail, notests
	Coverage    float64
	HasCoverage bool
	Line        string
	// FailedTests names the tests go test reported as failing for this
	// package, so a hook log says which test broke, not just which package.
	FailedTests []string
	// FailureLog holds the first test log lines (file_test.go:N: message)
	// go test printed for this package's failures, so the gate says why a
	// test failed, not only that it did. It is capped at MaxFailureLogLines.
	FailureLog []string
}

// MaxFailureLogLines bounds FailureLog so one noisy package cannot flood a
// hook log.
const MaxFailureLogLines = 12

// Finding is one refusal the gate reports.
type Finding struct {
	Package string
	Kind    string
	Detail  string
}

// Report is the gate's full outcome for one run.
type Report struct {
	Threshold float64
	Packages  int
	Results   []Result
	Findings  []Finding
	Waived    []Finding
}

// LoadConfig reads and validates the gate policy.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("covergate: read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("covergate: parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate refuses a policy that could waive silently: the threshold must
// be a real percentage and every exception must name an exact package, a
// known kind, an owner, a reason and a parseable expiry.
func (c Config) Validate() error {
	if c.Threshold <= 0 || c.Threshold > 100 {
		return fmt.Errorf("covergate: threshold %v is not a percentage in (0, 100]", c.Threshold)
	}
	seen := map[string]bool{}
	for i, e := range c.Exceptions {
		switch {
		case strings.TrimSpace(e.Package) == "" || strings.Contains(e.Package, "*") || strings.Contains(e.Package, "..."):
			return fmt.Errorf("covergate: exception %d must name one exact package, got %q", i, e.Package)
		case e.Kind != KindNoTests && e.Kind != KindBelowFloor:
			return fmt.Errorf("covergate: exception %d (%s) has unknown kind %q", i, e.Package, e.Kind)
		case strings.TrimSpace(e.Owner) == "" || strings.TrimSpace(e.Reason) == "":
			return fmt.Errorf("covergate: exception %d (%s) needs an owner and a reason", i, e.Package)
		}
		if _, err := time.Parse("2006-01-02", e.Expiry); err != nil {
			return fmt.Errorf("covergate: exception %d (%s) expiry %q is not YYYY-MM-DD", i, e.Package, e.Expiry)
		}
		key := e.Package + "|" + e.Kind
		if seen[key] {
			return fmt.Errorf("covergate: duplicate exception for %s %s", e.Package, e.Kind)
		}
		seen[key] = true
	}
	return nil
}

var (
	okLine      = regexp.MustCompile(`^ok\s+(\S+)\s+(.*)$`)
	coverageTag = regexp.MustCompile(`coverage:\s+([0-9.]+)% of statements`)
	noTestsLine = regexp.MustCompile(`^\?\s+(\S+)\s+\[no test files\]`)
	failLine    = regexp.MustCompile(`^FAIL\s+(\S+)(?:\s+.*)?$`)
	// failedTestLine is go test's per-test failure marker, printed before
	// the package summary line it belongs to.
	failedTestLine = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	// failureLogLine matches the indented "file_test.go:12: message" lines go
	// test prints under a failing test.
	failureLogLine = regexp.MustCompile(`^\s+\S+_test\.go:\d+: `)
)

// ParseGoTestOutput turns go test's per-package summary lines into results.
// Lines that are not package summaries (test logs, the trailing FAIL, the
// Windows unlink complaint) are ignored.
func ParseGoTestOutput(out string) []Result {
	var results []Result
	var failed, logs []string
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if m := failedTestLine.FindStringSubmatch(line); m != nil {
			failed = append(failed, m[1])
			continue
		}
		if failureLogLine.MatchString(line) {
			if len(logs) < MaxFailureLogLines {
				logs = append(logs, strings.TrimSpace(line))
			}
			continue
		}
		if m := okLine.FindStringSubmatch(line); m != nil {
			r := Result{Package: m[1], Status: "ok", Line: line}
			if c := coverageTag.FindStringSubmatch(m[2]); c != nil {
				if v, err := strconv.ParseFloat(c[1], 64); err == nil {
					r.Coverage, r.HasCoverage = v, true
				}
			}
			results = append(results, r)
			failed, logs = nil, nil
			continue
		}
		if m := noTestsLine.FindStringSubmatch(line); m != nil {
			results = append(results, Result{Package: m[1], Status: "notests", Line: line})
			failed, logs = nil, nil
			continue
		}
		if m := failLine.FindStringSubmatch(line); m != nil && m[1] != "" {
			detail := line
			if len(failed) > 0 {
				detail = line + " (" + strings.Join(failed, ", ") + ")"
			}
			if len(logs) > 0 {
				detail += " [" + strings.Join(logs, " | ") + "]"
			}
			results = append(results, Result{Package: m[1], Status: "fail", Line: detail,
				FailedTests: append([]string(nil), failed...), FailureLog: append([]string(nil), logs...)})
			failed, logs = nil, nil
		}
	}
	return results
}

// Relative strips the module prefix so exceptions and reports use the same
// repo-relative package path the rest of the toolchain uses.
func Relative(pkg string) string {
	if pkg == Module {
		return "."
	}
	return strings.TrimPrefix(pkg, Module+"/")
}

// Evaluate applies the policy to parsed results. An exception waives only
// its own kind for its exact package and only while unexpired; an expired
// exception is reported as if it did not exist.
func Evaluate(cfg Config, results []Result, now time.Time) (findings, waived []Finding) {
	active := map[string]bool{}
	for _, e := range cfg.Exceptions {
		expiry, err := time.Parse("2006-01-02", e.Expiry)
		if err == nil && !now.After(expiry.Add(24*time.Hour-time.Nanosecond)) {
			active[e.Package+"|"+e.Kind] = true
		}
	}
	for _, r := range results {
		rel := Relative(r.Package)
		var f *Finding
		switch {
		case r.Status == "fail":
			f = &Finding{Package: rel, Kind: FindingTestFailure, Detail: r.Line}
		case r.Status == "notests":
			f = &Finding{Package: rel, Kind: FindingNoTests, Detail: "package has no test files"}
		case r.Status == "ok" && r.HasCoverage && r.Coverage < cfg.Threshold:
			f = &Finding{Package: rel, Kind: FindingBelowFloor, Detail: fmt.Sprintf("%.1f%% of statements covered, floor is %.0f%%", r.Coverage, cfg.Threshold)}
		}
		if f == nil {
			continue
		}
		if f.Kind != FindingTestFailure && active[rel+"|"+f.Kind] {
			waived = append(waived, *f)
			continue
		}
		findings = append(findings, *f)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Package < findings[j].Package
	})
	return findings, waived
}

// excludedDir reports directories the gate never measures: generated code,
// fixtures, the nested module and lane scratch directories.
func excludedDir(rel string) bool {
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, ".") || rel == "" {
		return rel != "" && rel != "."
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "testdata" || part == "node_modules" || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return strings.HasPrefix(rel, "gen/") || strings.HasPrefix(rel, "src/blocks/go") || strings.HasPrefix(rel, "tmp/")
}

// StagedPackages lists the package directories (as ./dir patterns) that hold
// staged Go files. Deleted files are ignored; a directory that no longer
// exists is skipped.
func StagedPackages(root string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("covergate: git diff --cached: %w", err)
	}
	return packagesFromFiles(root, strings.Split(string(out), "\n")), nil
}

func packagesFromFiles(root string, files []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range files {
		f = strings.TrimSpace(strings.ReplaceAll(f, "\\", "/"))
		if f == "" || !strings.HasSuffix(f, ".go") {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(f))
		if dir == "." || excludedDir(dir) || seen[dir] {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir))); err != nil || !info.IsDir() {
			continue
		}
		seen[dir] = true
		pkgs = append(pkgs, "./"+dir)
	}
	sort.Strings(pkgs)
	return pkgs
}

// AllPackages lists every measurable package in the root module.
func AllPackages(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("covergate: resolve root: %w", err)
	}
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("covergate: go list: %w", err)
	}
	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel, err := filepath.Rel(root, line)
		if err != nil || excludedDir(rel) {
			continue
		}
		pkgs = append(pkgs, "./"+filepath.ToSlash(rel))
	}
	sort.Strings(pkgs)
	return pkgs, nil
}

// RunGoTest runs go test -cover over pkgs and returns its combined output.
// The exit error is returned alongside the output and is only decisive when
// no package line could be parsed, so the Windows unlink quirk is tolerated.
func RunGoTest(root string, pkgs []string, timeout time.Duration) (string, error) {
	args := append([]string{"test", "-count=1", "-cover", "-timeout", timeout.String()}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// Gate runs the whole check for pkgs under root with the policy at cfgPath.
func Gate(root, cfgPath string, pkgs []string, now time.Time, timeout time.Duration) (Report, error) {
	cfg, err := LoadConfig(filepath.Join(root, cfgPath))
	if err != nil {
		return Report{}, err
	}
	report := Report{Threshold: cfg.Threshold, Packages: len(pkgs)}
	if len(pkgs) == 0 {
		return report, nil
	}
	out, runErr := RunGoTest(root, pkgs, timeout)
	report.Results = ParseGoTestOutput(out)
	if len(report.Results) == 0 {
		if runErr != nil {
			return report, fmt.Errorf("covergate: go test produced no package results: %w\n%s", runErr, out)
		}
		return report, errors.New("covergate: go test produced no package results")
	}
	report.Findings, report.Waived = Evaluate(cfg, report.Results, now)
	return report, nil
}

// Format renders the report for a terminal: one line per finding, then the
// waivers, then the verdict.
func (r Report) Format() string {
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "covergate: GAP %s %s: %s\n", f.Kind, f.Package, f.Detail)
	}
	for _, w := range r.Waived {
		fmt.Fprintf(&b, "covergate: waived %s %s: %s\n", w.Kind, w.Package, w.Detail)
	}
	measured := 0
	for _, res := range r.Results {
		if res.HasCoverage {
			measured++
		}
	}
	if len(r.Findings) == 0 {
		fmt.Fprintf(&b, "covergate: PASS %d package(s), %d measured, floor %.0f%%\n", r.Packages, measured, r.Threshold)
	} else {
		fmt.Fprintf(&b, "covergate: FAIL %d finding(s) across %d package(s), floor %.0f%%\n", len(r.Findings), r.Packages, r.Threshold)
	}
	return b.String()
}
