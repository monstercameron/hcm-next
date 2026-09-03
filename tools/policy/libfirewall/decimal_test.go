package libfirewall_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/depmanifest"
	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/libfirewall"
)

const apdImportPath = "github.com/cockroachdb/apd/v3"

func apdOwningRoots(t *testing.T, roles *depmanifest.Manifest) []string {
	t.Helper()
	for _, row := range roles.Modules {
		if row.Path == apdImportPath {
			return row.AllowedImportRoots
		}
	}
	t.Fatalf("dependency-roles.yaml has no row for %s", apdImportPath)
	return nil
}

const leakedDecimalReturnSource = `package fakekernel

import "github.com/cockroachdb/apd/v3"

// Add leaks the raw apd.Decimal to any caller; RED fixture.
func Add(a, b *apd.Decimal) *apd.Decimal { return nil }
`

const leakedDecimalAliasSource = `package fakekernel

import "github.com/cockroachdb/apd/v3"

// Money is a bare alias RED fixture.
type Money = apd.Decimal
`

const ownedMoneySource = `package fakekernel

import "github.com/cockroachdb/apd/v3"

// Money is an owned, opaque wrapper; the apd.Decimal it holds is never
// exported directly. GREEN fixture.
type Money struct {
	value apd.Decimal
}

// Add returns the owned wrapper type, not the raw apd.Decimal. GREEN
// fixture.
func Add(a, b Money) Money { return Money{} }
`

// TestDecimalBackendQualification is the LIB-006 primary test. Like
// TestPGXAdapterQualification, it checks two things: (1) apd/v3 is only
// importable from dependency-roles.yaml's own allowed_import_roots for it
// (internal/kernel); (2) even inside that root, an exported alias, field or
// function signature naming apd.Decimal directly is a leak -- callers must
// receive HCM Next's own Money/Rate/Percentage/Quantity kernel value types,
// never a bare apd.Decimal.
func TestDecimalBackendQualification(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)

	t.Run("import root boundary", func(t *testing.T) {
		cases := []struct {
			name     string
			importer string
			wantV    bool
		}{
			{"internal/kernel/values may import apd", roles.Module + "/internal/kernel/values", false},
			{"a domain package may not import apd", roles.Module + "/internal/domains/compensation", true},
			{"an engine may not import apd", roles.Module + "/internal/engines/payband", true},
			{"workflow may not import apd", roles.Module + "/internal/workflow/step", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				v := libfirewall.CheckImport(roles, tc.importer, apdImportPath)
				if tc.wantV && v == nil {
					t.Errorf("CheckImport(%q, %q) = nil, want a violation", tc.importer, apdImportPath)
				}
				if !tc.wantV && v != nil {
					t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, apdImportPath, v)
				}
			})
		}
	})

	t.Run("leaked concrete type scan", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			source string
		}{
			{"leaked apd.Decimal parameter/return", leakedDecimalReturnSource},
			{"leaked apd.Decimal type alias", leakedDecimalAliasSource},
		} {
			t.Run(tc.name, func(t *testing.T) {
				exposures, err := libfirewall.ScanSource("fixture.go", tc.source, []string{apdImportPath})
				if err != nil {
					t.Fatalf("ScanSource: %v", err)
				}
				if len(libfirewall.FilterLeakTypes(exposures, apdImportPath, []string{"Decimal"})) == 0 {
					t.Errorf("expected a leaked apd.Decimal exposure, found none in %+v", exposures)
				}
			})
		}

		t.Run("owned Money wrapper does not leak", func(t *testing.T) {
			exposures, err := libfirewall.ScanSource("fixture.go", ownedMoneySource, []string{apdImportPath})
			if err != nil {
				t.Fatalf("ScanSource: %v", err)
			}
			if leaked := libfirewall.FilterLeakTypes(exposures, apdImportPath, []string{"Decimal"}); len(leaked) != 0 {
				t.Errorf("owned Money fixture flagged as leaking Decimal: %+v", leaked)
			}
		})
	})

	_ = apdOwningRoots(t, roles) // exercised directly by TestTodo_LIB_006_Integration
}

// TestTodo_LIB_006_Property: an exported func returning exactly *apd.Decimal
// (pointer, as apd's own API shapes it) or apd.Decimal (value) is always
// flagged, regardless of the function's name.
func TestTodo_LIB_006_Property(t *testing.T) {
	forms := []string{"apd.Decimal", "*apd.Decimal", "[]apd.Decimal"}
	names := []string{"Add", "Sub", "Round", "Whatever"}
	for _, name := range names {
		for _, form := range forms {
			src := "package fakekernel\n\nimport \"github.com/cockroachdb/apd/v3\"\n\nfunc " + name + "() " + form + " { return nil }\n"
			exposures, err := libfirewall.ScanSource("fixture.go", src, []string{apdImportPath})
			if err != nil {
				t.Fatalf("ScanSource(%s/%s): %v", name, form, err)
			}
			if len(libfirewall.FilterLeakTypes(exposures, apdImportPath, []string{"Decimal"})) == 0 {
				t.Errorf("func %s() %s was not flagged as a leak", name, form)
			}
		}
	}
}

// TestTodo_LIB_006_Golden pins the exact Exposure shape for one canonical
// leak.
func TestTodo_LIB_006_Golden(t *testing.T) {
	exposures, err := libfirewall.ScanSource("kernel.go", leakedDecimalAliasSource, []string{apdImportPath})
	if err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	leaked := libfirewall.FilterLeakTypes(exposures, apdImportPath, []string{"Decimal"})
	if len(leaked) != 1 {
		t.Fatalf("got %d leaked exposures, want 1: %+v", len(leaked), leaked)
	}
	got := leaked[0]
	if got.File != "kernel.go" || got.Name != "Money" || got.Kind != "alias" || got.Type != "apd.Decimal" {
		t.Errorf("exposure = %+v, want {File:kernel.go Name:Money Kind:alias Type:apd.Decimal}", got)
	}
}

// TestTodo_LIB_006_Race scans the same fixture concurrently, proving no
// shared mutable state across calls.
func TestTodo_LIB_006_Race(t *testing.T) {
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_, err := libfirewall.ScanSource("fixture.go", leakedDecimalReturnSource, []string{apdImportPath})
			done <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Errorf("ScanSource: %v", err)
		}
	}
}

// TestTodo_LIB_006_Conformance runs the qualification against the real
// tree: import boundary plus leak-scan over apd's own allowed roots.
func TestTodo_LIB_006_Conformance(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	root := repopath.RootDir()
	owningRoots := apdOwningRoots(t, roles)

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var boundaryViolations, leakViolations int
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil && v.Module == apdImportPath {
				boundaryViolations++
				t.Errorf("LIB-006 import boundary violation: %s imports %s (allowed: %v)", v.Importer, v.ImportedPath, v.AllowedRoots)
			}
		}
		imports := pkg.Imports
		owns := false
		for _, imp := range imports {
			if imp == apdImportPath {
				owns = true
				break
			}
		}
		if !owns {
			continue
		}
		exposures, err := libfirewall.ScanExposures(pkg.Dir, []string{apdImportPath})
		if err != nil {
			t.Fatalf("scanning %s: %v", pkg.Dir, err)
		}
		for _, exp := range libfirewall.FilterLeakTypes(exposures, apdImportPath, []string{"Decimal"}) {
			leakViolations++
			t.Errorf("LIB-006 leaked apd.Decimal: %s:%d %s %s exposes %s", exp.File, exp.Line, exp.Kind, exp.Name, exp.Type)
		}
	}
	_ = owningRoots
	t.Logf("apd: %d import-boundary violations, %d leaked-type violations", boundaryViolations, leakViolations)
}

// TestTodo_LIB_006_Mutation starts from the clean owned-Money fixture and
// mutates it into a bare-alias leak and a raw-return leak, confirming both
// mutations are caught.
func TestTodo_LIB_006_Mutation(t *testing.T) {
	baseline, err := libfirewall.ScanSource("fixture.go", ownedMoneySource, []string{apdImportPath})
	if err != nil {
		t.Fatalf("ScanSource(baseline): %v", err)
	}
	if leaked := libfirewall.FilterLeakTypes(baseline, apdImportPath, []string{"Decimal"}); len(leaked) != 0 {
		t.Fatalf("baseline fixture unexpectedly leaks: %+v", leaked)
	}

	mutants := map[string]string{
		"alias Money directly to apd.Decimal": leakedDecimalAliasSource,
		"return apd.Decimal instead of Money": leakedDecimalReturnSource,
	}
	for name, src := range mutants {
		t.Run(name, func(t *testing.T) {
			exposures, err := libfirewall.ScanSource("fixture.go", src, []string{apdImportPath})
			if err != nil {
				t.Fatalf("ScanSource: %v", err)
			}
			if len(libfirewall.FilterLeakTypes(exposures, apdImportPath, []string{"Decimal"})) == 0 {
				t.Errorf("mutant %q was not caught", name)
			}
		})
	}
}
