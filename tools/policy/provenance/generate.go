package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/tools/quality/buildverify"
)

// RootModulePath is this repository's module path, recorded as
// Source.Repository. It is a local constant (matching
// tools/policy/sbom.RootModulePath) rather than an import of that package,
// so this package's own logic pulls in nothing beyond buildverify's tested
// build mechanics (TOOL-016) and the Go standard library.
const RootModulePath = "github.com/monstercameron/hcm-next"

// BuilderID identifies cmd/provgen as the builder that produced a
// Statement's subjects.
const BuilderID = "github.com/monstercameron/hcm-next/tools/policy/provenance/cmd/provgen"

// DefaultSBOMPath is where TOOL-017 publishes the checked-in CycloneDX SBOM
// this package's SBOMReference points at by default.
const DefaultSBOMPath = "definitions/supply-chain/sbom.cdx.json"

// unknownSourceValue is recorded for SourceRef.Commit/Ref when no
// environment variable names a real value - see doc.go's "No git" section:
// this package never shells out to git.
const unknownSourceValue = "unknown"

// Options configures Generate. All fields are optional; the zero Options
// builds "./cmd/hcmnext" named "hcmnext" against DefaultSBOMPath.
type Options struct {
	// Pattern is the Go package pattern to build, e.g. "./cmd/hcmnext".
	Pattern string
	// SubjectName is the name recorded for the built artifact's Subject.
	SubjectName string
	// BuilderID overrides Builder.ID.
	BuilderID string
	// SBOMPath is the SBOM file path, relative to root, to hash and
	// reference.
	SBOMPath string
	// Repository overrides Source.Repository.
	Repository string
	// CommitEnvVars is checked in order for Source.Commit; the first
	// non-empty value wins. Defaults to {"GIT_COMMIT", "GITHUB_SHA"}.
	CommitEnvVars []string
	// RefEnvVars is checked in order for Source.Ref. Defaults to
	// {"GIT_REF", "GITHUB_REF"}.
	RefEnvVars []string
	// Now returns the generation timestamp; defaults to time.Now.
	Now func() time.Time
}

func (o Options) pattern() string {
	if o.Pattern != "" {
		return o.Pattern
	}
	return "./cmd/hcmnext"
}

func (o Options) subjectName() string {
	if o.SubjectName != "" {
		return o.SubjectName
	}
	return "hcmnext"
}

func (o Options) builderID() string {
	if o.BuilderID != "" {
		return o.BuilderID
	}
	return BuilderID
}

func (o Options) sbomPath() string {
	if o.SBOMPath != "" {
		return o.SBOMPath
	}
	return DefaultSBOMPath
}

func (o Options) repository() string {
	if o.Repository != "" {
		return o.Repository
	}
	return RootModulePath
}

func (o Options) commitEnvVars() []string {
	if len(o.CommitEnvVars) > 0 {
		return o.CommitEnvVars
	}
	return []string{"GIT_COMMIT", "GITHUB_SHA"}
}

func (o Options) refEnvVars() []string {
	if len(o.RefEnvVars) > 0 {
		return o.RefEnvVars
	}
	return []string{"GIT_REF", "GITHUB_REF"}
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// firstEnv returns the first non-empty environment variable named in
// names, or fallback when none is set.
func firstEnv(names []string, fallback string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return fallback
}

// Generate builds opts.Pattern from root into a fresh temporary directory,
// hashes the resulting binary, removes the temporary directory (the binary
// itself is never written anywhere durable - see doc.go), hashes the SBOM
// file at root/opts.SBOMPath, and assembles the resulting Statement. The
// returned Statement is unsigned; call SignStatement to sign it.
func Generate(root string, opts Options) (*Statement, error) {
	dir, err := os.MkdirTemp("", "provgen-*")
	if err != nil {
		return nil, fmt.Errorf("provenance: creating temp build dir: %w", err)
	}
	defer os.RemoveAll(dir)

	outPath := filepath.Join(dir, "artifact.bin")
	result, err := buildverify.Build(root, opts.pattern(), outPath, buildverify.DefaultOptions())
	if err != nil {
		return nil, fmt.Errorf("provenance: building %s: %w", opts.pattern(), err)
	}

	sbomFullPath := filepath.Join(root, opts.sbomPath())
	sbomBytes, err := os.ReadFile(sbomFullPath)
	if err != nil {
		return nil, fmt.Errorf("provenance: reading SBOM %s: %w", sbomFullPath, err)
	}
	sbomSum := sha256.Sum256(sbomBytes)

	goVersion, _ := splitVersionInfo(result.VersionInfo)
	_, settings := buildverify.ParseBuildSettings(result.VersionInfo)
	goos, goarch, flags := summarizeSettings(settings)

	in := buildInputs{
		subjectName:   opts.subjectName(),
		subjectDigest: result.Digest,
		builderID:     opts.builderID(),
		repository:    opts.repository(),
		commit:        firstEnv(opts.commitEnvVars(), unknownSourceValue),
		ref:           firstEnv(opts.refEnvVars(), unknownSourceValue),
		goVersion:     goVersion,
		goos:          goos,
		goarch:        goarch,
		flags:         flags,
		sbomPath:      opts.sbomPath(),
		sbomDigest:    hex.EncodeToString(sbomSum[:]),
		now:           opts.now(),
	}
	return buildStatement(in), nil
}

// splitVersionInfo splits a buildverify.Result.VersionInfo string (bare Go
// version on the first line, tab-indented "go version -m" fields after)
// into the bare Go version and the remaining lines.
func splitVersionInfo(versionInfo string) (goVersion, rest string) {
	parts := strings.SplitN(versionInfo, "\n", 2)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// summarizeSettings extracts GOOS/GOARCH and the reproducibility-relevant
// build flags from a parsed `go version -m` build-settings list, in a
// fixed, deterministic order.
func summarizeSettings(settings []buildverify.BuildSetting) (goos, goarch string, flags []string) {
	byKey := make(map[string]string, len(settings))
	for _, s := range settings {
		byKey[s.Key] = s.Value
	}
	goos = byKey["GOOS"]
	goarch = byKey["GOARCH"]

	// Fixed, meaningful order: reproducibility flags first, then the
	// CGO/toolchain switches that most affect whether a rebuild reproduces
	// the same bytes.
	orderedKeys := []string{"-trimpath", "-buildvcs", "CGO_ENABLED", "-compiler", "-buildmode"}
	for _, key := range orderedKeys {
		if v, ok := byKey[key]; ok {
			flags = append(flags, key+"="+v)
		}
	}
	return goos, goarch, flags
}

// buildInputs is the already-resolved, I/O-free input set buildStatement
// assembles into a Statement. Splitting Generate's real filesystem/exec
// work from this pure assembly step is what lets generate_test.go exercise
// the Statement shape deterministically without a real `go build` on every
// test run (see tools/policy/sbom.buildDocument for the same split).
type buildInputs struct {
	subjectName   string
	subjectDigest string
	builderID     string
	repository    string
	commit        string
	ref           string
	goVersion     string
	goos          string
	goarch        string
	flags         []string
	sbomPath      string
	sbomDigest    string
	now           time.Time
}

func buildStatement(in buildInputs) *Statement {
	return &Statement{
		SchemaVersion: SchemaVersion,
		PredicateType: PredicateType,
		GeneratedAt:   in.now.UTC().Format(time.RFC3339),
		Subjects: []Subject{
			{Name: in.subjectName, SHA256: in.subjectDigest},
		},
		Builder: Builder{ID: in.builderID},
		Source: SourceRef{
			Repository: in.repository,
			Ref:        in.ref,
			Commit:     in.commit,
		},
		BuildConfig: BuildConfig{
			GoVersion:    in.goVersion,
			GOOS:         in.goos,
			GOARCH:       in.goarch,
			Flags:        append([]string(nil), in.flags...),
			ConfigDigest: ConfigDigest(in.goVersion, in.goos, in.goarch, in.flags),
		},
		SBOM: SBOMReference{
			Path:   in.sbomPath,
			SHA256: in.sbomDigest,
		},
	}
}

// configDigestPayload is ConfigDigest's canonical JSON projection.
type configDigestPayload struct {
	GoVersion string   `json:"go_version"`
	GOOS      string   `json:"goos"`
	GOARCH    string   `json:"goarch"`
	Flags     []string `json:"flags"`
}

// ConfigDigest returns the hex-encoded sha256 digest of the given build
// configuration's canonical JSON projection. Two builds with the same
// toolchain version, target and flag set fold to the same ConfigDigest
// regardless of when or where they ran - this is the "build config digest"
// BuildConfig.ConfigDigest records, letting a caller compare two
// Statements' build configurations for equality without diffing every
// field by hand.
func ConfigDigest(goVersion, goos, goarch string, flags []string) string {
	b, err := json.Marshal(configDigestPayload{
		GoVersion: goVersion,
		GOOS:      goos,
		GOARCH:    goarch,
		Flags:     flags,
	})
	if err != nil {
		// configDigestPayload's fields are all plain strings/slices of
		// strings, which encoding/json never fails to marshal.
		panic(fmt.Sprintf("provenance: marshal config digest payload: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
