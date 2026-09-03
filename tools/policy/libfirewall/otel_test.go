package libfirewall_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/libfirewall"
)

// TestOTelBackendQualification is the LIB-007 primary test. OpenTelemetry
// is "not imported by production code before OBS-002" per
// dependency-roles.yaml's go.opentelemetry.io/ family_rule, whose
// allowed_import_roots is deliberately empty until OBS-002 adds an exact
// row. This check reads that live allowed-roots value from the manifest
// rather than hardcoding "always forbidden", so the day OBS-002 lands a row
// naming internal/operations/telemetry (per that row's own comment), this
// test starts enforcing the new boundary without needing to change.
func TestOTelBackendQualification(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"a domain package importing OTel trace", roles.Module + "/internal/domains/people", cfg.OTelModulePrefix + "otel/trace", true},
		{"an engine importing OTel metric", roles.Module + "/internal/engines/payband", cfg.OTelModulePrefix + "otel/metric", true},
		{"workflow importing OTel", roles.Module + "/internal/workflow/runtime", cfg.OTelModulePrefix + "otel", true},
		{"capability importing OTel", roles.Module + "/internal/capability", cfg.OTelModulePrefix + "otel/sdk/trace", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := libfirewall.CheckImport(roles, tc.importer, tc.imported)
			if tc.wantV && v == nil {
				t.Errorf("CheckImport(%q, %q) = nil, want a violation (OTel is not yet importable anywhere; OBS-002 has not landed)", tc.importer, tc.imported)
			}
			if !tc.wantV && v != nil {
				t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		})
	}
}

// TestTodo_LIB_007_Golden pins the current (pre-OBS-002) allowed-roots
// state for the OpenTelemetry family: empty, meaning zero direct
// production imports anywhere. A change to dependency-roles.yaml's
// go.opentelemetry.io/ family_rule allowed_import_roots (i.e. OBS-002
// landing) is a visible diff here, not a silent behavior change.
func TestTodo_LIB_007_Golden(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	class := roles.Classify("go.opentelemetry.io/otel")
	if !class.Found {
		t.Fatalf("dependency-roles.yaml no longer classifies go.opentelemetry.io/*")
	}
	if len(class.Row.AllowedImportRoots) != 0 {
		t.Logf("go.opentelemetry.io/ allowed_import_roots is now %v (OBS-002 appears to have landed); TestOTelBackendQualification enforces whatever this manifest currently says", class.Row.AllowedImportRoots)
	}
}

// TestTodo_LIB_007_Integration runs the check against the real import graph
// and reports every real direct OpenTelemetry import found in HEAD.
func TestTodo_LIB_007_Integration(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if len(imp) < len(cfg.OTelModulePrefix) || imp[:len(cfg.OTelModulePrefix)] != cfg.OTelModulePrefix {
				continue
			}
			if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil {
				total++
				t.Errorf("LIB-007 OpenTelemetry direct-import violation: %s imports %s (production code may not import OTel before OBS-002)", v.Importer, v.ImportedPath)
			}
		}
	}
	t.Logf("scanned %d packages, %d direct OpenTelemetry imports found", len(pkgs), total)
}

// TestTodo_LIB_007_Race runs CheckImport concurrently for OTel edges.
func TestTodo_LIB_007_Race(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_ = libfirewall.CheckImport(roles, roles.Module+"/internal/domains/people", "go.opentelemetry.io/otel")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

// TestTodo_LIB_007_Conformance checks a representative OTel submodule
// (trace, metric, sdk) all resolve to the same family classification and
// are all currently forbidden everywhere.
func TestTodo_LIB_007_Conformance(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	submodules := []string{
		"go.opentelemetry.io/otel",
		"go.opentelemetry.io/otel/trace",
		"go.opentelemetry.io/otel/metric",
		"go.opentelemetry.io/otel/sdk/trace",
		"go.opentelemetry.io/otel/exporters/otlp/otlptrace",
	}
	for _, sub := range submodules {
		if v := libfirewall.CheckImport(roles, roles.Module+"/internal/domains/people", sub); v == nil {
			t.Errorf("%s was not flagged as forbidden", sub)
		}
	}
}
