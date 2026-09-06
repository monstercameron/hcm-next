package provenance

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/quality/buildverify"
)

// TestTodo_TOOL_018_Golden note: the byte-exact golden coverage for signed
// statements lives in sign_test.go (external package), since it needs the
// checked-in testdata key fixture. This file's tests are the pure,
// exec-free assembly and parsing helpers Generate delegates to.

func TestBuildStatementAssemblesEveryField(t *testing.T) {
	fixedNow := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	in := buildInputs{
		subjectName:   "hcmnext",
		subjectDigest: "deadbeef",
		builderID:     BuilderID,
		repository:    RootModulePath,
		commit:        "abc123",
		ref:           "refs/heads/main",
		goVersion:     "go1.26.3",
		goos:          "windows",
		goarch:        "arm64",
		flags:         []string{"-trimpath=true", "CGO_ENABLED=0"},
		sbomPath:      DefaultSBOMPath,
		sbomDigest:    "feedface",
		now:           fixedNow,
	}

	s := buildStatement(in)

	if s.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", s.SchemaVersion, SchemaVersion)
	}
	if s.PredicateType != PredicateType {
		t.Errorf("PredicateType = %q, want %q", s.PredicateType, PredicateType)
	}
	if s.GeneratedAt != "2026-09-05T12:00:00Z" {
		t.Errorf("GeneratedAt = %q, want 2026-09-05T12:00:00Z", s.GeneratedAt)
	}
	if len(s.Subjects) != 1 || s.Subjects[0].Name != "hcmnext" || s.Subjects[0].SHA256 != "deadbeef" {
		t.Errorf("Subjects = %+v, want one {hcmnext, deadbeef}", s.Subjects)
	}
	if s.Builder.ID != BuilderID {
		t.Errorf("Builder.ID = %q, want %q", s.Builder.ID, BuilderID)
	}
	if s.Source != (SourceRef{Repository: RootModulePath, Ref: "refs/heads/main", Commit: "abc123"}) {
		t.Errorf("Source = %+v", s.Source)
	}
	if s.BuildConfig.GoVersion != "go1.26.3" || s.BuildConfig.GOOS != "windows" || s.BuildConfig.GOARCH != "arm64" {
		t.Errorf("BuildConfig = %+v", s.BuildConfig)
	}
	wantConfigDigest := ConfigDigest("go1.26.3", "windows", "arm64", []string{"-trimpath=true", "CGO_ENABLED=0"})
	if s.BuildConfig.ConfigDigest != wantConfigDigest {
		t.Errorf("BuildConfig.ConfigDigest = %s, want %s", s.BuildConfig.ConfigDigest, wantConfigDigest)
	}
	if s.SBOM.Path != DefaultSBOMPath || s.SBOM.SHA256 != "feedface" {
		t.Errorf("SBOM = %+v", s.SBOM)
	}
	if s.Signature != nil {
		t.Error("buildStatement must return an unsigned Statement")
	}
}

func TestBuildStatementFlagsAreCopiedNotAliased(t *testing.T) {
	flags := []string{"-trimpath=true"}
	in := buildInputs{flags: flags, now: time.Unix(0, 0)}
	s := buildStatement(in)

	flags[0] = "mutated"
	if s.BuildConfig.Flags[0] == "mutated" {
		t.Error("buildStatement must copy the flags slice, not alias the caller's backing array")
	}
}

func TestConfigDigestIsDeterministicAndSensitiveToEveryField(t *testing.T) {
	base := ConfigDigest("go1.26.3", "windows", "arm64", []string{"-trimpath=true"})
	again := ConfigDigest("go1.26.3", "windows", "arm64", []string{"-trimpath=true"})
	if base != again {
		t.Fatal("ConfigDigest is not deterministic for identical inputs")
	}

	variants := []string{
		ConfigDigest("go1.26.4", "windows", "arm64", []string{"-trimpath=true"}),
		ConfigDigest("go1.26.3", "linux", "arm64", []string{"-trimpath=true"}),
		ConfigDigest("go1.26.3", "windows", "amd64", []string{"-trimpath=true"}),
		ConfigDigest("go1.26.3", "windows", "arm64", []string{"-trimpath=false"}),
		ConfigDigest("go1.26.3", "windows", "arm64", nil),
	}
	for i, v := range variants {
		if v == base {
			t.Errorf("variant %d unexpectedly produced the same digest as the base config", i)
		}
	}
}

func TestSplitVersionInfoSeparatesBareVersionFromRest(t *testing.T) {
	goVersion, rest := splitVersionInfo("go1.26.3\n\tpath\texample.com/x\n\tbuild\tGOOS=windows")
	if goVersion != "go1.26.3" {
		t.Errorf("goVersion = %q, want go1.26.3", goVersion)
	}
	if rest != "\tpath\texample.com/x\n\tbuild\tGOOS=windows" {
		t.Errorf("rest = %q", rest)
	}
}

func TestSplitVersionInfoHandlesNoNewline(t *testing.T) {
	goVersion, rest := splitVersionInfo("go1.26.3")
	if goVersion != "go1.26.3" || rest != "" {
		t.Errorf("got (%q, %q), want (go1.26.3, \"\")", goVersion, rest)
	}
}

func TestSummarizeSettingsExtractsGOOSGOARCHAndOrdersFlags(t *testing.T) {
	settings := []buildverify.BuildSetting{
		{Key: "GOARCH", Value: "arm64"},
		{Key: "GOOS", Value: "windows"},
		{Key: "CGO_ENABLED", Value: "0"},
		{Key: "-buildmode", Value: "exe"},
		{Key: "-trimpath", Value: "true"},
		{Key: "-compiler", Value: "gc"},
		{Key: "GOARM64", Value: "v8.0"}, // not in the ordered extraction list; must be ignored
	}

	goos, goarch, flags := summarizeSettings(settings)
	if goos != "windows" {
		t.Errorf("goos = %q, want windows", goos)
	}
	if goarch != "arm64" {
		t.Errorf("goarch = %q, want arm64", goarch)
	}
	wantFlags := []string{"-trimpath=true", "CGO_ENABLED=0", "-compiler=gc", "-buildmode=exe"}
	if len(flags) != len(wantFlags) {
		t.Fatalf("flags = %v, want %v", flags, wantFlags)
	}
	for i := range wantFlags {
		if flags[i] != wantFlags[i] {
			t.Errorf("flags[%d] = %q, want %q", i, flags[i], wantFlags[i])
		}
	}
}

func TestFirstEnvReturnsFallbackWhenNoneSet(t *testing.T) {
	got := firstEnv([]string{"HCM_NEXT_PROVENANCE_TEST_UNSET_VAR_1", "HCM_NEXT_PROVENANCE_TEST_UNSET_VAR_2"}, unknownSourceValue)
	if got != unknownSourceValue {
		t.Errorf("firstEnv = %q, want %q", got, unknownSourceValue)
	}
}

func TestFirstEnvReturnsFirstNonEmptyValue(t *testing.T) {
	t.Setenv("HCM_NEXT_PROVENANCE_TEST_VAR_A", "")
	t.Setenv("HCM_NEXT_PROVENANCE_TEST_VAR_B", "value-b")
	got := firstEnv([]string{"HCM_NEXT_PROVENANCE_TEST_VAR_A", "HCM_NEXT_PROVENANCE_TEST_VAR_B"}, unknownSourceValue)
	if got != "value-b" {
		t.Errorf("firstEnv = %q, want value-b", got)
	}
}

func TestOptionsDefaults(t *testing.T) {
	var o Options
	if o.pattern() != "./cmd/hcmnext" {
		t.Errorf("pattern() = %q", o.pattern())
	}
	if o.subjectName() != "hcmnext" {
		t.Errorf("subjectName() = %q", o.subjectName())
	}
	if o.builderID() != BuilderID {
		t.Errorf("builderID() = %q", o.builderID())
	}
	if o.sbomPath() != DefaultSBOMPath {
		t.Errorf("sbomPath() = %q", o.sbomPath())
	}
	if o.repository() != RootModulePath {
		t.Errorf("repository() = %q", o.repository())
	}
	if len(o.commitEnvVars()) == 0 {
		t.Error("commitEnvVars() default must be non-empty")
	}
	if len(o.refEnvVars()) == 0 {
		t.Error("refEnvVars() default must be non-empty")
	}
	if o.now().IsZero() {
		t.Error("now() default must not be zero")
	}
}

func TestOptionsOverrides(t *testing.T) {
	fixed := time.Unix(100, 0)
	o := Options{
		Pattern:       "./cmd/other",
		SubjectName:   "other",
		BuilderID:     "custom-builder",
		SBOMPath:      "custom-sbom.json",
		Repository:    "example.com/other",
		CommitEnvVars: []string{"X_COMMIT"},
		RefEnvVars:    []string{"X_REF"},
		Now:           func() time.Time { return fixed },
	}
	if o.pattern() != "./cmd/other" {
		t.Errorf("pattern() = %q", o.pattern())
	}
	if o.subjectName() != "other" {
		t.Errorf("subjectName() = %q", o.subjectName())
	}
	if o.builderID() != "custom-builder" {
		t.Errorf("builderID() = %q", o.builderID())
	}
	if o.sbomPath() != "custom-sbom.json" {
		t.Errorf("sbomPath() = %q", o.sbomPath())
	}
	if o.repository() != "example.com/other" {
		t.Errorf("repository() = %q", o.repository())
	}
	if len(o.commitEnvVars()) != 1 || o.commitEnvVars()[0] != "X_COMMIT" {
		t.Errorf("commitEnvVars() = %v", o.commitEnvVars())
	}
	if len(o.refEnvVars()) != 1 || o.refEnvVars()[0] != "X_REF" {
		t.Errorf("refEnvVars() = %v", o.refEnvVars())
	}
	if !o.now().Equal(fixed) {
		t.Errorf("now() = %v, want %v", o.now(), fixed)
	}
}
