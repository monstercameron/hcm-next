package archrules_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/archrules"
	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

func loadArchConfig(t *testing.T) *archrules.Config {
	t.Helper()
	root := repopath.RootDir()
	c, err := archrules.Load(filepath.Join(root, "definitions", "architecture", "architecture-rules.yaml"))
	if err != nil {
		t.Fatalf("loading architecture-rules config: %v", err)
	}
	return c
}

// TestKernelImportsOnlyAllowedDependencies is the ARCH-GO-004 primary test:
// internal/kernel/* may import only the standard library plus
// architecture-rules.yaml's explicit allow-list (apd, uuid, x/text); any
// other third-party import -- Protobuf/gRPC included -- is a violation, as
// is any intra-module import of a higher layer or a port/adapter (already
// covered by depedge's kernel-must-not-import-upward rule; this test
// focuses on the external-dependency allowlist ARCH-GO-004 adds on top).
func TestKernelImportsOnlyAllowedDependencies(t *testing.T) {
	cfg := loadArchConfig(t)
	mod := cfg.Module
	k := cfg.Kernel

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"kernel/values importing apd is allowed", mod + "/internal/kernel/values", "github.com/cockroachdb/apd/v3", false},
		{"kernel/values importing uuid is allowed", mod + "/internal/kernel/values", "github.com/google/uuid", false},
		{"kernel/canonical importing x/text is allowed", mod + "/internal/kernel/canonical", "golang.org/x/text/unicode/norm", false},
		{"kernel importing stdlib is always allowed", mod + "/internal/kernel/digest", "crypto/sha256", false},
		{"kernel importing protobuf is forbidden", mod + "/internal/kernel/canonical", "google.golang.org/protobuf/proto", true},
		{"kernel importing grpc is forbidden", mod + "/internal/kernel/values", "google.golang.org/grpc", true},
		{"kernel importing pgx is forbidden", mod + "/internal/kernel/values", "github.com/jackc/pgx/v5", true},
		{"kernel importing an unrelated x/ package is forbidden (only x/text is allowed)", mod + "/internal/kernel/values", "golang.org/x/net/http2", true},
		{"a non-kernel package is not this check's concern", mod + "/internal/domains/people", "google.golang.org/protobuf/proto", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			importerRel, ok := archrules.TrimModule(mod, tc.importer)
			if !ok {
				t.Fatalf("importer %q not part of module %q", tc.importer, mod)
			}
			v := archrules.CheckExternalImportAllowlist(k, mod, importerRel, tc.imported)
			if tc.wantV && v == nil {
				t.Errorf("CheckExternalImportAllowlist(%q, %q) = nil, want a violation", tc.importer, tc.imported)
			}
			if !tc.wantV && v != nil {
				t.Errorf("CheckExternalImportAllowlist(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		})
	}
}

// TestTodo_ARCH_GO_004_Property: for any import path with a "." in its
// first path segment (i.e. plausibly third-party, not stdlib) that does
// not match one of the allow-listed prefixes, a kernel importer is always
// flagged -- independent of which specific bogus module name is used.
func TestTodo_ARCH_GO_004_Property(t *testing.T) {
	cfg := loadArchConfig(t)
	mod := cfg.Module
	importer, _ := archrules.TrimModule(mod, mod+"/internal/kernel/values")

	bogusModules := []string{
		"example.com/whatever/v1",
		"github.com/some/other-thing",
		"gitlab.com/foo/bar/v9",
	}
	for _, m := range bogusModules {
		if v := archrules.CheckExternalImportAllowlist(cfg.Kernel, mod, importer, m); v == nil {
			t.Errorf("CheckExternalImportAllowlist(kernel, %q) = nil, want a violation", m)
		}
	}
}

// TestTodo_ARCH_GO_004_Golden pins the exact allow-list architecture-rules.yaml
// declares for the kernel today.
func TestTodo_ARCH_GO_004_Golden(t *testing.T) {
	cfg := loadArchConfig(t)
	want := []string{"github.com/cockroachdb/apd/v3", "github.com/google/uuid", "golang.org/x/text"}
	got := cfg.Kernel.AllowedExternalImportPrefixes
	if len(got) != len(want) {
		t.Fatalf("kernel.allowed_external_import_prefixes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kernel.allowed_external_import_prefixes = %v, want %v", got, want)
		}
	}
}

// TestTodo_ARCH_GO_004_Integration runs the allowlist against the real
// import graph and reports every real kernel package's non-allow-listed
// external import found in HEAD.
func TestTodo_ARCH_GO_004_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok || !archrules.UnderRoot(rel, cfg.Kernel.Root) {
			continue
		}
		for _, imp := range pkg.Imports {
			if v := archrules.CheckExternalImportAllowlist(cfg.Kernel, cfg.Module, rel, imp); v != nil {
				total++
				t.Errorf("ARCH-GO-004 violation: %s imports %s, which is not stdlib or one of %v", v.Importer, v.Imported, cfg.Kernel.AllowedExternalImportPrefixes)
			}
		}
	}
	t.Logf("scanned kernel packages, %d ARCH-GO-004 violations", total)
}

// TestTodo_ARCH_GO_004_Security checks that internal/kernel's exported API
// never names an HCM business-process term (a concrete domain like
// Payroll/Promotion/Compensation): kernel primitives must stay generic
// identity/temporal/decimal/digest/evidence/lifecycle vocabulary, never a
// leaked process name, which is the ARCH-GO-004 RED clause's "rejects HCM
// process names in exported kernel APIs" requirement.
func TestTodo_ARCH_GO_004_Security(t *testing.T) {
	forbiddenTerms := []string{"Payroll", "Promotion", "Compensation", "Benefits", "Recruiting"}

	src := `package fakekernel

// PayrollAmount would leak a business-process term into the kernel's
// exported API; RED fixture.
type PayrollAmount struct{}
`
	names, err := archrules.ExportedTopLevelNamesFromSource("fixture.go", src)
	if err != nil {
		t.Fatalf("ExportedTopLevelNamesFromSource: %v", err)
	}
	found := false
	for name := range names {
		for _, term := range forbiddenTerms {
			if containsFold(name, term) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("fixture should have surfaced a forbidden process term for the checker to catch")
	}

	// GREEN: the real internal/kernel exported surface has no such term.
	root := repopath.RootDir()
	kernelDirs := []string{
		filepath.Join(root, "internal", "kernel", "values"),
		filepath.Join(root, "internal", "kernel", "canonical"),
		filepath.Join(root, "internal", "kernel", "digest"),
	}
	for _, dir := range kernelDirs {
		names, err := archrules.ExportedTopLevelNames(dir)
		if err != nil {
			t.Fatalf("ExportedTopLevelNames(%s): %v", dir, err)
		}
		for name := range names {
			for _, term := range forbiddenTerms {
				if containsFold(name, term) {
					t.Errorf("kernel package %s exports %q, which contains the HCM process term %q", dir, name, term)
				}
			}
		}
	}
}

func containsFold(s, substr string) bool {
	// Simple ASCII case-insensitive substring check; kernel identifiers are
	// ASCII Go exported names.
	sl, subl := len(s), len(substr)
	if subl == 0 || subl > sl {
		return false
	}
	lower := func(b byte) byte {
		if b >= 'A' && b <= 'Z' {
			return b + ('a' - 'A')
		}
		return b
	}
	for i := 0; i+subl <= sl; i++ {
		match := true
		for j := 0; j < subl; j++ {
			if lower(s[i+j]) != lower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestTodo_ARCH_GO_004_Conformance checks a canonical vector per allow-listed
// prefix (each is accepted) plus one representative forbidden module
// outside the list (rejected), end to end.
func TestTodo_ARCH_GO_004_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	importer, _ := archrules.TrimModule(cfg.Module, cfg.Module+"/internal/kernel/values")

	for _, prefix := range cfg.Kernel.AllowedExternalImportPrefixes {
		if v := archrules.CheckExternalImportAllowlist(cfg.Kernel, cfg.Module, importer, prefix); v != nil {
			t.Errorf("allow-listed prefix %q was flagged: %+v", prefix, v)
		}
	}
	if v := archrules.CheckExternalImportAllowlist(cfg.Kernel, cfg.Module, importer, "google.golang.org/protobuf/proto"); v == nil {
		t.Errorf("protobuf was not flagged, want a violation")
	}
}

// TestTodo_ARCH_GO_004_Mutation starts from a clean allow-listed import and
// mutates the imported path one character at a time into each known-bad
// case, confirming the checker (not a lucky exact-string match) is doing
// prefix comparison correctly in both directions.
func TestTodo_ARCH_GO_004_Mutation(t *testing.T) {
	cfg := loadArchConfig(t)
	importer, _ := archrules.TrimModule(cfg.Module, cfg.Module+"/internal/kernel/values")

	if v := archrules.CheckExternalImportAllowlist(cfg.Kernel, cfg.Module, importer, "github.com/cockroachdb/apd/v3"); v != nil {
		t.Fatalf("baseline unexpectedly flagged: %+v", v)
	}

	mutants := []string{
		"github.com/cockroachdb/apd",     // missing /v3: different module path entirely
		"github.com/cockroachdb/apd/v3x", // prefix collision without a "/" boundary
		"github.com/cockroachdbxapd/v3",  // similar but not the real module
	}
	for _, m := range mutants {
		t.Run(m, func(t *testing.T) {
			if v := archrules.CheckExternalImportAllowlist(cfg.Kernel, cfg.Module, importer, m); v == nil {
				t.Errorf("mutant %q was not caught", m)
			}
		})
	}
}

// TestTodo_ARCH_GO_004_Race runs the allowlist check concurrently.
func TestTodo_ARCH_GO_004_Race(t *testing.T) {
	cfg := loadArchConfig(t)
	importer, _ := archrules.TrimModule(cfg.Module, cfg.Module+"/internal/kernel/values")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			imp := "github.com/cockroachdb/apd/v3"
			if i%2 == 0 {
				imp = "google.golang.org/protobuf/proto"
			}
			_ = archrules.CheckExternalImportAllowlist(cfg.Kernel, cfg.Module, importer, imp)
		}(i)
	}
	wg.Wait()
}
