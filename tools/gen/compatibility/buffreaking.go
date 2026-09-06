package compatibility

// This file extends the package with API-002's remaining evidence gap:
// Check (in compatibility.go) proves the wire-compatibility *policy* against
// descriptors a caller assembles, but every one of its tests hands it
// synthetic descriptors built by hand. Nothing in this package, or in
// internal/transport/manifest's TestTodo_API_002 (which checks synthetic
// Version structs), ever asks whether the real, committed schema/proto
// sources are still compatible with what shipped before. RunBufBreaking
// closes that gap by shelling out to the local `buf` CLI, offline, against a
// checked-in descriptor-set golden — no Buf Schema Registry, no network, and
// no git-based baseline (unavailable to this lane). GateRetirement then
// layers the "removal gate requires migration evidence and zero
// unsupported active consumers" rule from API-002's GREEN criterion on top
// of either RunBufBreaking's or Check's result.
//
// Regenerating a golden: delete the target .binpb under testdata/ and rerun
// the owning test with HCMNEXT_COMPAT_REGEN_BASELINE=1 set. The test writes
// a fresh image from the live sources and passes trivially (a baseline is
// definitionally compatible with itself); rerun without the flag afterward
// to confirm the new golden is checked in correctly.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RegenBaselineEnv is the documented escape hatch that regenerates a
// checked-in descriptor-set golden instead of comparing against it. It is
// never read implicitly by production code; only the *_test.go golden tests
// in this package consult it.
const RegenBaselineEnv = "HCMNEXT_COMPAT_REGEN_BASELINE"

// BufBreakingViolation is one finding `buf breaking` emitted for a single
// Protobuf source location, decoded from its `--error-format=json` output.
type BufBreakingViolation struct {
	Path        string `json:"path"`
	StartLine   int    `json:"start_line"`
	StartColumn int    `json:"start_column"`
	EndLine     int    `json:"end_line"`
	EndColumn   int    `json:"end_column"`
	Type        string `json:"type"`
	Message     string `json:"message"`
}

// BufBreakingReport is the deterministic outcome of comparing one Protobuf
// module against one descriptor-set baseline image with `buf breaking`.
type BufBreakingReport struct {
	Module     string                 `json:"module"`
	Baseline   string                 `json:"baseline"`
	Violations []BufBreakingViolation `json:"violations"`
}

// OK reports whether buf found the module wire-compatible with the baseline.
func (r BufBreakingReport) OK() bool { return len(r.Violations) == 0 }

// ErrBufNotFound is returned by ResolveBufBinary when no `buf` executable can
// be located. Callers that treat buf as optional tooling should skip rather
// than fail when they see it.
var ErrBufNotFound = fmt.Errorf("compatibility: no buf executable found on PATH or in a known Go bin directory")

// ResolveBufBinary locates the local `buf` CLI. It never contacts a network
// service or the Buf Schema Registry; it only looks for an already-installed
// executable, first on PATH, then in the Go bin directories the toolchain
// itself would install `buf` into (GOBIN, then GOPATH/bin, then the default
// per-user Go bin directory), since a bin directory added to a user's shell
// profile is not always inherited by every process that runs `go test`.
func ResolveBufBinary() (string, error) {
	name := "buf"
	if runtime.GOOS == "windows" {
		name = "buf.exe"
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	candidates := []string{}
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		candidates = append(candidates, filepath.Join(gobin, name))
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		candidates = append(candidates, filepath.Join(gopath, "bin", name))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "go", "bin", name))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", ErrBufNotFound
}

// BuildDescriptorImage runs `buf build <modulePath> -o <outputImage>`,
// (re)writing a descriptor-set golden from the live Protobuf sources at
// modulePath. It performs no comparison; RunBufBreaking is what enforces
// compatibility against the image this produces.
func BuildDescriptorImage(bufBinary, modulePath, outputImage string) error {
	if err := os.MkdirAll(filepath.Dir(outputImage), 0o755); err != nil {
		return fmt.Errorf("compatibility: creating %s: %w", filepath.Dir(outputImage), err)
	}
	cmd := exec.Command(bufBinary, "build", modulePath, "-o", outputImage)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compatibility: buf build %s: %w: %s", modulePath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// RunBufBreaking invokes `buf breaking <modulePath> --against <baselineImage>
// --error-format=json` and decodes its findings into a BufBreakingReport.
// It runs entirely offline: modulePath is a local directory of .proto files
// and baselineImage is a local descriptor-set file previously produced by
// BuildDescriptorImage (or `buf build`) — never a live registry reference.
//
// buf exits non-zero exactly when it found at least one breaking change (or
// failed to run); RunBufBreaking distinguishes the two by requiring every
// non-empty stdout line to parse as a violation. A run that exits non-zero
// with no parseable violation is reported as an error, not a silent pass.
func RunBufBreaking(bufBinary, modulePath, baselineImage string) (BufBreakingReport, error) {
	report := BufBreakingReport{Module: modulePath, Baseline: baselineImage, Violations: []BufBreakingViolation{}}
	if _, err := os.Stat(baselineImage); err != nil {
		return report, fmt.Errorf("compatibility: baseline image %s: %w", baselineImage, err)
	}
	cmd := exec.Command(bufBinary, "breaking", modulePath, "--against", baselineImage, "--error-format=json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v BufBreakingViolation
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return report, fmt.Errorf("compatibility: decoding buf breaking output line %q: %w", line, err)
		}
		report.Violations = append(report.Violations, v)
	}

	if runErr != nil && len(report.Violations) == 0 {
		return report, fmt.Errorf("compatibility: buf breaking %s: %w: %s", modulePath, runErr, strings.TrimSpace(stderr.String()))
	}
	return report, nil
}

// Consumer records one tenant-scoped caller depending on a Protobuf contract
// version. It carries exactly the evidence API-002's retirement gate needs:
// whether the consumer still requires the retiring version and whether it is
// active, independent of whatever wire-compatibility engine (Check or
// RunBufBreaking) classified the underlying change.
type Consumer struct {
	ID             string `json:"id"`
	Tenant         string `json:"tenant"`
	Active         bool   `json:"active"`
	Required       bool   `json:"required"`
	AdoptedVersion uint32 `json:"adopted_version"`
}

// RetirementDecision is the outcome of GateRetirement.
type RetirementDecision string

const (
	RetirementAllowed RetirementDecision = "ALLOWED"
	RetirementBlocked RetirementDecision = "BLOCKED"
)

// RetirementGate is the deterministic, evidence-carrying result of
// GateRetirement: never a bare boolean, always naming what is missing.
type RetirementGate struct {
	Decision        RetirementDecision `json:"decision"`
	MissingEvidence bool               `json:"missing_evidence"`
	ActiveConsumers []Consumer         `json:"active_consumers"`
}

// OK reports whether the retirement is allowed to proceed.
func (g RetirementGate) OK() bool { return g.Decision == RetirementAllowed }

// GateRetirement enforces API-002's deprecation-evidence rule: a version may
// be retired only with recorded migration evidence and zero required,
// active consumers still adopted below targetVersion. Wire compatibility
// (whether removing the version is a *breaking* change) is a separate
// question answered by Check or RunBufBreaking; a wire-compatible removal
// still needs migration evidence, and an incompatible one is blocked
// regardless of evidence by whichever compatibility check ran first.
func GateRetirement(migrationEvidence string, targetVersion uint32, consumers []Consumer) RetirementGate {
	gate := RetirementGate{Decision: RetirementAllowed, ActiveConsumers: []Consumer{}}
	if migrationEvidence == "" {
		gate.MissingEvidence = true
	}
	for _, c := range consumers {
		if c.Required && c.Active && c.AdoptedVersion < targetVersion {
			gate.ActiveConsumers = append(gate.ActiveConsumers, c)
		}
	}
	if gate.MissingEvidence || len(gate.ActiveConsumers) > 0 {
		gate.Decision = RetirementBlocked
	}
	return gate
}
