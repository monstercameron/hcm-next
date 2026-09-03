// Package buildverify implements the TOOL-016 deterministic build check.
//
// The checker builds a chosen Go source twice with a fixed reproducibility
// flag set (-trimpath, -buildvcs=false and a fixed CGO_ENABLED=0), then
// compares the resulting binaries' SHA-256 digests and their `go version
// -m` embedded build-settings output. Two builds are "reproducible" (the
// TOOL-016 GREEN state) when both match.
//
// The comparison is only meaningful if the two builds are actually capable
// of exposing nondeterminism. Building the same file twice from the same
// directory in immediate succession is not a real test: nothing differs
// between the two invocations regardless of flags. `go build` embeds each
// compilation's absolute source directory in its build ID / debug
// information unless -trimpath is passed (that is the whole reason
// -trimpath exists), so CompareIsolated deliberately builds the source
// from two freshly created, independently-named temporary directories.
// Without -trimpath those two builds produce different digests (TOOL-016's
// documented RED case); with the fixed flag set they produce identical
// digests (GREEN).
package buildverify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options controls the flags and environment passed to a single `go
// build` invocation.
type Options struct {
	// Trimpath, when true, passes -trimpath so absolute source directory
	// paths are not embedded in the resulting binary.
	Trimpath bool
	// BuildVCS is passed as -buildvcs=<value>. TOOL-016 pins this false so
	// VCS state (which does not exist verifiably identically across two
	// isolated checkouts) can never be the source of a digest mismatch.
	BuildVCS bool
	// CGOEnabled selects CGO_ENABLED=1 when true, CGO_ENABLED=0 otherwise.
	// TOOL-016 pins this false.
	CGOEnabled bool
	// ExtraEnv is appended to the build environment after the fixed
	// CGO_ENABLED entry, "KEY=VALUE" per entry.
	ExtraEnv []string
}

// DefaultOptions returns the TOOL-016 baseline flag set: -trimpath,
// -buildvcs=false and CGO_ENABLED=0.
func DefaultOptions() Options {
	return Options{Trimpath: true, BuildVCS: false, CGOEnabled: false}
}

// Result is the outcome of one `go build` attempt.
type Result struct {
	// BinaryPath is where the built binary was written.
	BinaryPath string
	// Digest is the hex-encoded SHA-256 of the binary's bytes.
	Digest string
	// VersionInfo is the `go version -m` output for the binary, with the
	// leading line's binary-specific path stripped (see goVersionM) so two
	// builds at different paths can be compared directly.
	VersionInfo string
}

// Comparison is the result of building the same source twice and diffing
// the two outcomes.
type Comparison struct {
	First, Second    Result
	DigestsMatch     bool
	VersionInfoMatch bool
}

// Reproducible reports whether both the binary digest and the embedded
// build-settings output matched between First and Second.
func (c Comparison) Reproducible() bool {
	return c.DigestsMatch && c.VersionInfoMatch
}

func compare(r1, r2 Result) Comparison {
	return Comparison{
		First:            r1,
		Second:           r2,
		DigestsMatch:     r1.Digest == r2.Digest,
		VersionInfoMatch: r1.VersionInfo == r2.VersionInfo,
	}
}

// Build compiles pattern (a package pattern such as "./cmd/migrate" or a
// single .go file path) from workDir into outputPath using opts, and
// returns its digest and embedded build-settings output.
func Build(workDir, pattern, outputPath string, opts Options) (Result, error) {
	args := []string{"build"}
	if opts.Trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, fmt.Sprintf("-buildvcs=%t", opts.BuildVCS))
	args = append(args, "-o", outputPath, pattern)

	cmd := exec.Command("go", args...)
	cmd.Dir = workDir
	cmd.Env = buildEnv(opts)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("buildverify: go build %s: %w\n%s", pattern, err, stderr.String())
	}

	digest, err := digestFile(outputPath)
	if err != nil {
		return Result{}, err
	}
	versionInfo, err := goVersionM(outputPath)
	if err != nil {
		return Result{}, err
	}
	return Result{BinaryPath: outputPath, Digest: digest, VersionInfo: versionInfo}, nil
}

// CompareInPlace builds pattern from workDir twice, into two distinct
// output files in the same freshly created temporary directory, and
// compares the results. Because both builds share the same source
// directory, this only proves "rebuilding right now, unchanged, is
// reproducible" - it cannot expose absolute-path embedding
// nondeterminism the way CompareIsolated does. Use it against a real
// command (e.g. "./cmd/migrate") as a smoke check; use CompareIsolated
// against fixture source to actually prove the checker detects
// nondeterminism.
func CompareInPlace(workDir, pattern string, opts Options) (Comparison, error) {
	dir, err := os.MkdirTemp("", "buildverify-inplace-*")
	if err != nil {
		return Comparison{}, fmt.Errorf("buildverify: creating temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	r1, err := Build(workDir, pattern, filepath.Join(dir, "first"), opts)
	if err != nil {
		return Comparison{}, fmt.Errorf("buildverify: first build: %w", err)
	}
	r2, err := Build(workDir, pattern, filepath.Join(dir, "second"), opts)
	if err != nil {
		return Comparison{}, fmt.Errorf("buildverify: second build: %w", err)
	}
	return compare(r1, r2), nil
}

// BuildIsolated writes source to a freshly created temporary directory
// under filename, builds it there with opts, and returns the result plus
// the temporary directory (the caller is responsible for removing it).
func BuildIsolated(source []byte, filename string, opts Options) (Result, string, error) {
	dir, err := os.MkdirTemp("", "buildverify-*")
	if err != nil {
		return Result{}, "", fmt.Errorf("buildverify: creating temp dir: %w", err)
	}
	srcPath := filepath.Join(dir, filename)
	if err := os.WriteFile(srcPath, source, 0o644); err != nil {
		os.RemoveAll(dir)
		return Result{}, "", fmt.Errorf("buildverify: writing fixture source: %w", err)
	}

	res, err := Build(dir, srcPath, filepath.Join(dir, "out.bin"), opts)
	if err != nil {
		os.RemoveAll(dir)
		return Result{}, "", err
	}
	return res, dir, nil
}

// CompareIsolated builds source (written under filename) twice, each time
// in its own freshly created, independently-named temporary directory,
// and compares the two binaries. This is the shape that actually exercises
// TOOL-016's RED/GREEN distinction: see the package doc comment.
func CompareIsolated(source []byte, filename string, opts Options) (Comparison, error) {
	r1, dir1, err := BuildIsolated(source, filename, opts)
	if err != nil {
		return Comparison{}, fmt.Errorf("buildverify: first build: %w", err)
	}
	defer os.RemoveAll(dir1)

	r2, dir2, err := BuildIsolated(source, filename, opts)
	if err != nil {
		return Comparison{}, fmt.Errorf("buildverify: second build: %w", err)
	}
	defer os.RemoveAll(dir2)

	return compare(r1, r2), nil
}

// buildEnv returns os.Environ() with any existing CGO_ENABLED entry
// removed and replaced with the fixed value opts.CGOEnabled selects, plus
// opts.ExtraEnv appended.
func buildEnv(opts Options) []string {
	env := make([]string, 0, len(os.Environ())+1+len(opts.ExtraEnv))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CGO_ENABLED=") {
			continue
		}
		env = append(env, kv)
	}
	cgo := "0"
	if opts.CGOEnabled {
		cgo = "1"
	}
	env = append(env, "CGO_ENABLED="+cgo)
	env = append(env, opts.ExtraEnv...)
	return env
}

func digestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("buildverify: reading binary: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// goVersionM runs `go version -m` against binaryPath and returns its
// output with the binary-specific path in the leading line replaced by
// just the reported Go version, so output from two binaries at different
// paths can be compared directly. The leading line's format is
// "<path>: go1.2.3"; the split point is the *last* ": " rather than the
// first, since an absolute Windows path itself contains a colon (e.g.
// "C:\Users\...").
func goVersionM(binaryPath string) (string, error) {
	cmd := exec.Command("go", "version", "-m", binaryPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("buildverify: go version -m %s: %w\n%s", binaryPath, err, stderr.String())
	}

	lines := strings.Split(stdout.String(), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", fmt.Errorf("buildverify: go version -m %s: empty output", binaryPath)
	}

	first := lines[0]
	goVersion := first
	if idx := strings.LastIndex(first, ": "); idx >= 0 {
		goVersion = first[idx+2:]
	}
	lines[0] = goVersion
	return strings.TrimRight(strings.Join(lines, "\n"), "\n"), nil
}

// BuildSetting is one parsed `go version -m` "build" line, e.g.
// {Key: "-trimpath", Value: "true"} or {Key: "CGO_ENABLED", Value: "0"}.
type BuildSetting struct {
	Key   string
	Value string
}

// ParseBuildSettings parses a Result.VersionInfo string into the reported
// main-package path and its ordered list of build settings. Lines that do
// not match the tab-separated "keyword\tvalue" shape (including the
// leading bare Go-version line goVersionM leaves in place) are ignored.
func ParseBuildSettings(versionInfo string) (path string, settings []BuildSetting) {
	for _, line := range strings.Split(versionInfo, "\n") {
		line = strings.TrimPrefix(strings.TrimRight(line, "\r"), "\t")
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		keyword, value := fields[0], fields[1]
		switch keyword {
		case "path":
			path = value
		case "build":
			key, val, found := strings.Cut(value, "=")
			if !found {
				key, val = value, ""
			}
			settings = append(settings, BuildSetting{Key: key, Value: val})
		}
	}
	return path, settings
}
