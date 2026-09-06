package sbom

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/module"
)

type recordingLicenseReader struct {
	base  OSFileReader
	paths []string
}

func (r *recordingLicenseReader) ReadFile(name string) ([]byte, error) {
	r.paths = append(r.paths, name)
	return r.base.ReadFile(name)
}

func writeLicenseFixture(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}
}

func cacheFixtureDir(t *testing.T, cache, path, version string) string {
	t.Helper()
	escaped, err := module.EscapePath(path)
	if err != nil {
		t.Fatalf("EscapePath(%s): %v", path, err)
	}
	dir := filepath.Join(cache, escaped+"@"+version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	return dir
}

// TestTodo_SUPPLY_003 proves that license identity comes from module-owned
// evidence through the file port, while an absent declaration remains
// UNKNOWN and is never inferred from a module path.
func TestTodo_SUPPLY_003(t *testing.T) {
	cache := t.TempDir()
	reader := &recordingLicenseReader{}

	declaredPath, declaredVersion := "example.com/declared", "v1.2.3"
	declaredDir := cacheFixtureDir(t, cache, declaredPath, declaredVersion)
	writeLicenseFixture(t, declaredDir, map[string]string{
		"go.mod":  "module example.com/declared\ngo 1.26.3\n// SPDX-License-Identifier: MIT AND Apache-2.0\n",
		"LICENSE": "this text must not override go.mod\n",
	})
	resolver := NewLicenseResolver(reader, cache)
	evidence, err := resolver.Resolve(declaredPath, declaredVersion, "")
	if err != nil {
		t.Fatalf("Resolve declared module: %v", err)
	}
	if evidence != (LicenseEvidence{Expression: "MIT AND Apache-2.0", Source: "go.mod"}) {
		t.Fatalf("declared evidence = %+v", evidence)
	}

	licensePath, licenseVersion := "example.com/license-file", "v0.4.0"
	licenseDir := cacheFixtureDir(t, cache, licensePath, licenseVersion)
	writeLicenseFixture(t, licenseDir, map[string]string{
		"go.mod":  "module example.com/license-file\ngo 1.26.3\n",
		"LICENSE": "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy.\n",
	})
	evidence, err = resolver.Resolve(licensePath, licenseVersion, "")
	if err != nil {
		t.Fatalf("Resolve LICENSE module: %v", err)
	}
	if evidence != (LicenseEvidence{Expression: "MIT", Source: "LICENSE"}) {
		t.Fatalf("LICENSE evidence = %+v", evidence)
	}

	unknownPath, unknownVersion := "example.com/no-license", "v9.0.0"
	cacheFixtureDir(t, cache, unknownPath, unknownVersion)
	evidence, err = resolver.Resolve(unknownPath, unknownVersion, "")
	if err != nil {
		t.Fatalf("Resolve unknown module: %v", err)
	}
	if evidence.Expression != UnknownLicense {
		t.Fatalf("unknown evidence = %+v, want UNKNOWN", evidence)
	}
	if len(reader.paths) == 0 || !strings.Contains(strings.Join(reader.paths, "\n"), "go.mod") {
		t.Fatalf("resolver did not read module files through the port: %v", reader.paths)
	}
}

// TestTodo_SUPPLY_003_Golden pins a fixture module set, including the
// component license field and deterministic exception serialization.
func TestTodo_SUPPLY_003_Golden(t *testing.T) {
	requires := []Require{
		{Path: "example.com/alpha", Version: "v1.0.0"},
		{Path: "example.com/beta", Version: "v2.0.0", Indirect: true},
	}
	licenses := map[string]LicenseEvidence{
		"example.com/golden-root@v0.0.0": {Expression: "MIT", Source: "go.mod"},
		"example.com/alpha@v1.0.0":       {Expression: "Apache-2.0", Source: "LICENSE"},
		"example.com/beta@v2.0.0":        {Expression: UnknownLicense, Source: "LICENSE"},
	}
	exceptions := []LicenseException{{
		Component: "example.com/beta",
		Version:   "v2.0.0",
		Reason:    "upstream has not published a machine-readable declaration",
		Reviewer:  "supply-chain-reviewer",
		Expiry:    "2027-01-01",
	}}
	doc := buildDocumentWithLicenses("example.com/golden-root", requires, nil, nil, Options{
		RootVersion:       "v0.0.0",
		Now:               func() time.Time { return time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC) },
		LicenseExceptions: exceptions,
	}, licenses)

	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	want := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.5",
  "serialNumber": "` + doc.SerialNumber + `",
  "version": 1,
  "metadata": {
    "timestamp": "2026-09-05T00:00:00Z",
    "tools": [
      {
        "vendor": "github.com/monstercameron/hcm-next",
        "name": "hcm-next-sbomgen",
        "version": "dev"
      }
    ],
    "component": {
      "bom-ref": "pkg:golang/example.com/golden-root@v0.0.0",
      "type": "application",
      "name": "example.com/golden-root",
      "version": "v0.0.0",
      "purl": "pkg:golang/example.com/golden-root@v0.0.0",
      "license": "MIT"
    }
  },
  "components": [
    {
      "bom-ref": "pkg:golang/example.com/alpha@v1.0.0",
      "type": "library",
      "name": "example.com/alpha",
      "version": "v1.0.0",
      "purl": "pkg:golang/example.com/alpha@v1.0.0",
      "scope": "required",
      "license": "Apache-2.0"
    },
    {
      "bom-ref": "pkg:golang/example.com/beta@v2.0.0",
      "type": "library",
      "name": "example.com/beta",
      "version": "v2.0.0",
      "purl": "pkg:golang/example.com/beta@v2.0.0",
      "scope": "optional",
      "license": "UNKNOWN"
    }
  ],
  "dependencies": [
    {
      "ref": "pkg:golang/example.com/alpha@v1.0.0"
    },
    {
      "ref": "pkg:golang/example.com/beta@v2.0.0"
    },
    {
      "ref": "pkg:golang/example.com/golden-root@v0.0.0"
    }
  ],
  "licenseExceptions": [
    {
      "component": "example.com/beta",
      "version": "v2.0.0",
      "reason": "upstream has not published a machine-readable declaration",
      "reviewer": "supply-chain-reviewer",
      "expiry": "2027-01-01"
    }
  ]
}`
	if string(got) != want {
		t.Fatalf("golden mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestTodo_SUPPLY_003_Security proves unknown licenses are fail-closed and
// that an exception cannot be reused across revisions or kept past expiry.
func TestTodo_SUPPLY_003_Security(t *testing.T) {
	component := Component{Name: "example.com/unknown", Version: "v1.0.0", License: UnknownLicense}
	base := &Document{Components: []Component{component}}
	asOf := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if got := ValidateLicenseCompletenessAt(base, asOf); len(got.MissingLicenses) != 1 {
		t.Fatalf("unexplained UNKNOWN = %+v, want one missing license", got)
	}

	complete := LicenseException{
		Component: component.Name,
		Version:   component.Version,
		Reason:    "upstream declaration is pending review",
		Reviewer:  "reviewer-1",
		Expiry:    "2026-12-31",
	}
	base.LicenseExceptions = []LicenseException{complete}
	if got := ValidateLicenseCompletenessAt(base, asOf); !got.Empty() {
		t.Fatalf("complete current exception = %+v, want no findings", got)
	}

	bad := *base
	bad.LicenseExceptions = []LicenseException{{
		Component: component.Name,
		Version:   "v2.0.0",
		Reason:    complete.Reason,
		Reviewer:  complete.Reviewer,
		Expiry:    complete.Expiry,
	}}
	got := ValidateLicenseCompletenessAt(&bad, asOf)
	if len(got.MissingLicenses) != 1 {
		t.Fatalf("wrong-revision exception = %+v, want missing license", got)
	}

	bad.LicenseExceptions[0] = complete
	bad.LicenseExceptions[0].Expiry = "2026-09-05"
	got = ValidateLicenseCompletenessAt(&bad, asOf)
	if len(got.InvalidLicenseRecords) != 1 {
		t.Fatalf("expired exception = %+v, want invalid record", got)
	}

	bad.LicenseExceptions[0] = complete
	bad.LicenseExceptions[0].Reviewer = ""
	got = ValidateLicenseCompletenessAt(&bad, asOf)
	if len(got.InvalidLicenseRecords) != 1 || !strings.Contains(got.InvalidLicenseRecords[0], "reviewer") {
		t.Fatalf("incomplete exception = %+v, want reviewer finding", got)
	}
}

// TestTodo_SUPPLY_003_Integration wires the real OS reader, cache resolver,
// generator core, and completeness gate over a fixture module set.
func TestTodo_SUPPLY_003_Integration(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	writeLicenseFixture(t, root, map[string]string{
		"go.mod":  "module github.com/monstercameron/hcm-next\ngo 1.26.3\n// SPDX-License-Identifier: MIT\n",
		"LICENSE": "unrecognized fallback text\n",
	})
	depPath, depVersion := "example.com/fixture-dep", "v1.0.0"
	depDir := cacheFixtureDir(t, cache, depPath, depVersion)
	writeLicenseFixture(t, depDir, map[string]string{
		"go.mod":  "module example.com/fixture-dep\ngo 1.26.3\n",
		"LICENSE": "Apache License\nVersion 2.0, January 2004\n",
	})

	rootEvidence, err := resolveLicenseInDir(OSFileReader{}, root)
	if err != nil {
		t.Fatalf("root evidence: %v", err)
	}
	depEvidence, err := NewLicenseResolver(OSFileReader{}, cache).Resolve(depPath, depVersion, "")
	if err != nil {
		t.Fatalf("dependency evidence: %v", err)
	}
	doc := buildDocumentWithLicenses("github.com/monstercameron/hcm-next", []Require{{Path: depPath, Version: depVersion}}, nil, nil, Options{RootVersion: "v0.0.0"}, map[string]LicenseEvidence{
		"github.com/monstercameron/hcm-next@v0.0.0": rootEvidence,
		depPath + "@" + depVersion:                  depEvidence,
	})
	if doc.Metadata.Component.License != "MIT" || doc.Components[0].License != "Apache-2.0" {
		t.Fatalf("integrated licenses = root %q, dep %q", doc.Metadata.Component.License, doc.Components[0].License)
	}
	if got := ValidateLicenseCompletenessAt(doc, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)); !got.Empty() {
		t.Fatalf("integrated completeness = %+v", got)
	}
}

// TestTodo_SUPPLY_003_Mutation exercises the release-gate mutations that
// would otherwise make an UNKNOWN or an exception appear acceptable.
func TestTodo_SUPPLY_003_Mutation(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*Document)
		want string
	}{
		{"empty-license", func(d *Document) { d.Components[0].License = "" }, "missing"},
		{"unknown-license", func(d *Document) { d.Components[0].License = UnknownLicense }, "missing"},
		{"missing-reason", func(d *Document) { d.LicenseExceptions[0].Reason = "" }, "invalid"},
		{"expired", func(d *Document) { d.LicenseExceptions[0].Expiry = "2026-01-01" }, "invalid"},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			doc := &Document{
				Components: []Component{{Name: "example.com/unknown", Version: "v1.0.0", License: UnknownLicense}},
				LicenseExceptions: []LicenseException{{
					Component: "example.com/unknown", Version: "v1.0.0", Reason: "pending upstream", Reviewer: "reviewer", Expiry: "2027-01-01",
				}},
			}
			mutation.edit(doc)
			if mutation.want == "missing" {
				doc.LicenseExceptions = nil
			}
			got := ValidateLicenseCompletenessAt(doc, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
			if mutation.want == "missing" && len(got.MissingLicenses) == 0 {
				t.Fatalf("mutation findings = %+v, want missing license", got)
			}
			if mutation.want == "invalid" && len(got.InvalidLicenseRecords) == 0 {
				t.Fatalf("mutation findings = %+v, want invalid exception", got)
			}
		})
	}

	if !reflect.DeepEqual(licenseDeclarationValue("// license = \"MIT AND Apache-2.0\""), "MIT AND Apache-2.0") {
		t.Fatal("license declaration parser mutation: quoted expression drifted")
	}
	if _, err := ResolveLicenseEvidence(nil, filepath.Join(t.TempDir(), "missing")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("nil-reader fallback returned unrelated error: %v", err)
		}
	}
}

type faultLicenseReader struct {
	files map[string]string
	errs  map[string]error
}

func (r faultLicenseReader) ReadFile(name string) ([]byte, error) {
	if err, ok := r.errs[name]; ok {
		return nil, err
	}
	if content, ok := r.files[name]; ok {
		return []byte(content), nil
	}
	return nil, fs.ErrNotExist
}

func TestLicenseResolverAndReaderErrorBranches(t *testing.T) {
	if !isMissingFile(fs.ErrNotExist) || !isMissingFile(os.ErrNotExist) || isMissingFile(errors.New("different")) {
		t.Fatal("isMissingFile did not classify filesystem absence correctly")
	}

	resolver := NewLicenseResolver(nil, "")
	if _, ok := resolver.Reader.(OSFileReader); !ok {
		t.Fatalf("nil reader = %T, want OSFileReader", resolver.Reader)
	}
	evidence, err := resolver.Resolve("example.com/no-cache", "v1.0.0", "")
	if err != nil || evidence.Expression != UnknownLicense {
		t.Fatalf("Resolve without a module cache = %+v, %v, want UNKNOWN", evidence, err)
	}

	root := t.TempDir()
	writeLicenseFixture(t, root, map[string]string{"go.mod": "module example.com/root\n// SPDX-License-Identifier: MIT\n"})
	evidence, err = resolver.Resolve("example.com/root", "v1.0.0", root)
	if err != nil || evidence.Expression != "MIT" || evidence.Source != "go.mod" {
		t.Fatalf("Resolve(rootDir) = %+v, %v", evidence, err)
	}
	if alias, err := ResolveLicense(nil, root); err != nil || alias != evidence {
		t.Fatalf("ResolveLicense alias = %+v, %v; want %+v", alias, err, evidence)
	}
	if _, err := NewLicenseResolver(nil, t.TempDir()).Resolve("bad\x00module", "v1.0.0", ""); err == nil || !strings.Contains(err.Error(), "escaping module") {
		t.Fatal("Resolve accepted an invalid module path")
	}

	faults := faultLicenseReader{errs: map[string]error{filepath.Join("fault", "go.mod"): errors.New("go.mod unreadable")}}
	if _, err := resolveLicenseInDir(faults, "fault"); err == nil || !strings.Contains(err.Error(), "go.mod unreadable") {
		t.Fatalf("go.mod reader error = %v, want wrapped reader error", err)
	}
	faults = faultLicenseReader{files: map[string]string{filepath.Join("fault", "go.mod"): "module example.com/fault\n"}, errs: map[string]error{filepath.Join("fault", "LICENSE"): errors.New("license unreadable")}}
	if _, err := resolveLicenseInDir(faults, "fault"); err == nil || !strings.Contains(err.Error(), "license unreadable") {
		t.Fatalf("license reader error = %v, want wrapped reader error", err)
	}
	unknownDir := t.TempDir()
	writeLicenseFixture(t, unknownDir, map[string]string{"LICENSE": "unrecognized license prose"})
	evidence, err = ResolveLicenseEvidence(OSFileReader{}, unknownDir)
	if err != nil || evidence != (LicenseEvidence{Expression: UnknownLicense, Source: "LICENSE"}) {
		t.Fatalf("unrecognized license evidence = %+v, %v", evidence, err)
	}
}

func TestLicenseDeclarationRecognitionAndSPDXGrammar(t *testing.T) {
	declarations := []struct {
		line string
		want string
	}{
		{"// license = \"MIT AND Apache-2.0\"", "MIT AND Apache-2.0"},
		{"# licence: 'BSD-3-Clause'", "BSD-3-Clause"},
		{"licenses = MIT // trailing comment", "MIT"},
		{"not-a-license: MIT", ""},
	}
	for _, tc := range declarations {
		if got := licenseDeclarationValue(tc.line); got != tc.want {
			t.Errorf("licenseDeclarationValue(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
	if got := declaredSPDXExpression("SPDX-License-Identifier: MIT OR Apache-2.0", false); got != "MIT OR Apache-2.0" {
		t.Errorf("declared SPDX marker = %q", got)
	}
	if got := declaredSPDXExpression("license: MIT # comment", false); got != "MIT" {
		t.Errorf("declared license file expression = %q", got)
	}
	if got := declaredSPDXExpression("license: MIT", true); got != "MIT" {
		t.Errorf("declared go.mod expression = %q", got)
	}
	if got := declaredSPDXExpression("SPDX-License-Identifier: MIT AND", false); got != "" {
		t.Errorf("malformed SPDX expression = %q, want empty", got)
	}
	if cleanSPDXExpression("  MIT // note # another") != "MIT" {
		t.Fatal("cleanSPDXExpression did not remove trailing comments")
	}

	valid := []string{"MIT", "MIT AND Apache-2.0", "MIT OR Apache-2.0", "(MIT OR Apache-2.0) WITH GCC-exception-3.1"}
	for _, expression := range valid {
		if !validSPDXExpression(expression) {
			t.Errorf("validSPDXExpression(%q) = false", expression)
		}
	}
	invalid := []string{"", "MIT AND", "OR MIT", "MIT WITH", "(MIT", "MIT)", "MIT@Apache"}
	for _, expression := range invalid {
		if validSPDXExpression(expression) {
			t.Errorf("validSPDXExpression(%q) = true, want false", expression)
		}
	}
	for _, tc := range []struct {
		b    byte
		want bool
	}{{'A', true}, {'9', true}, {'-', true}, {':', true}, {'_', false}, {' ', false}} {
		if got := isSPDXIdentifierByte(tc.b); got != tc.want {
			t.Errorf("isSPDXIdentifierByte(%q) = %v, want %v", tc.b, got, tc.want)
		}
	}
}

func TestRecognizedLicenseTextAndLicenseCounts(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"GNU AFFERO GENERAL PUBLIC LICENSE Version 3", "AGPL-3.0"},
		{"GNU AFFERO GENERAL PUBLIC LICENSE", ""},
		{"SERVER SIDE PUBLIC LICENSE", "SSPL-1.0"},
		{"GNU GENERAL PUBLIC LICENSE Version 3", "GPL-3.0"},
		{"GNU GENERAL PUBLIC LICENSE Version 2", "GPL-2.0"},
		{"Mozilla Public License 2.0", "MPL-2.0"},
		{"PUBLIC DOMAIN", "Public-Domain"},
		{"APACHE LICENSE Version 2.0", "Apache-2.0"},
		{"ISC LICENSE", "ISC"},
		{"PERMISSION TO USE, COPY, MODIFY, AND/OR DISTRIBUTE", "ISC"},
		{"REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS; NEITHER THE NAME", "BSD-3-Clause"},
		{"REDISTRIBUTION AND USE IN SOURCE AND BINARY FORMS", "BSD-2-Clause"},
		{"MIT LICENSE", "MIT"},
		{"unrecognized", ""},
	}
	for _, tc := range cases {
		if got := recognizedLicenseText(tc.text); got != tc.want {
			t.Errorf("recognizedLicenseText(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
	doc := &Document{Components: []Component{{License: "MIT"}, {License: "MIT"}, {License: ""}, {License: UnknownLicense}, {License: "Apache-2.0"}}}
	counts := LicenseCounts(doc)
	if counts["MIT"] != 2 || counts[UnknownLicense] != 2 || counts["Apache-2.0"] != 1 {
		t.Fatalf("LicenseCounts = %v", counts)
	}
	if got := FormatLicenseCounts(doc); got != "Apache-2.0=1, MIT=2, UNKNOWN=2" {
		t.Fatalf("FormatLicenseCounts = %q", got)
	}
	if got := LicenseCounts(nil); len(got) != 0 || FormatLicenseCounts(nil) != "" {
		t.Fatalf("nil license counts = %v / %q", got, FormatLicenseCounts(nil))
	}

	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "cache"))
	if cache, err := defaultModuleCache(); err != nil || cache != os.Getenv("GOMODCACHE") {
		t.Fatalf("defaultModuleCache = %q, %v", cache, err)
	}
}
