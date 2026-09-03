package health_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/health"
)

// repoRoot locates definitions/storage/storage-disposition.yaml relative to
// this test file (go test's working directory is the package directory),
// rather than hardcoding an absolute path or assuming the module root is the
// process's current directory.
func repoRoot() string {
	// internal/data/health -> repo root is three levels up.
	return filepath.Join("..", "..", "..")
}

// TestLoadDispositionRegistry_FromFile proves this package's own YAML loader
// parses STORE-001's real, checked-in registry and recovers the facts Probe
// depends on for the three tables it evaluates on every dimension.
func TestLoadDispositionRegistry_FromFile(t *testing.T) {
	path := filepath.Join(repoRoot(), health.DefaultRegistryPath)

	reg, err := health.LoadDispositionRegistry(path)
	if err != nil {
		t.Fatalf("LoadDispositionRegistry(%s): %v", path, err)
	}
	if reg.Version != 1 {
		t.Fatalf("registry version = %d, want 1", reg.Version)
	}

	for _, tc := range []struct {
		table               string
		dataRole            string
		tenantScopingColumn string
		appendOnly          bool
		retentionClass      string
		rebuildSource       string
	}{
		{"ledger_event", "LEDGER", "tenant_id", true, "PERMANENT", ""},
		{"projection_checkpoint", "PROJECTION", "tenant_id", false, "REBUILDABLE", "ledger_event"},
		{"outbox", "OUTBOX", "tenant_id", false, "REBUILDABLE", "ledger_event"},
		{"stream_head", "LEDGER", "tenant_id", false, "OPERATIONAL", "ledger_event"},
	} {
		sd, ok := reg.Table(tc.table)
		if !ok {
			t.Fatalf("registry has no entry for %q", tc.table)
		}
		if sd.DataRole != tc.dataRole {
			t.Errorf("%s: data role = %q, want %q", tc.table, sd.DataRole, tc.dataRole)
		}
		if sd.TenantScopingColumn != tc.tenantScopingColumn {
			t.Errorf("%s: tenant scoping column = %q, want %q", tc.table, sd.TenantScopingColumn, tc.tenantScopingColumn)
		}
		if sd.AppendOnly != tc.appendOnly {
			t.Errorf("%s: append only = %v, want %v", tc.table, sd.AppendOnly, tc.appendOnly)
		}
		if sd.RetentionClass != tc.retentionClass {
			t.Errorf("%s: retention class = %q, want %q", tc.table, sd.RetentionClass, tc.retentionClass)
		}
		if sd.RebuildSource != tc.rebuildSource {
			t.Errorf("%s: rebuild source = %q, want %q", tc.table, sd.RebuildSource, tc.rebuildSource)
		}
	}

	if _, ok := reg.Table("no_such_table"); ok {
		t.Fatal("registry reported an entry for a table it does not define")
	}
}

// TestLoadOrDefaultDispositionRegistry_PrefersFile proves that when the
// published registry is present, it is what gets used, with ok=true.
func TestLoadOrDefaultDispositionRegistry_PrefersFile(t *testing.T) {
	path := filepath.Join(repoRoot(), health.DefaultRegistryPath)

	reg, ok, err := health.LoadOrDefaultDispositionRegistry(path)
	if err != nil {
		t.Fatalf("LoadOrDefaultDispositionRegistry(%s): %v", path, err)
	}
	if !ok {
		t.Fatal("ok = false, want true: the published registry exists at this path")
	}
	if _, found := reg.Table("outbox"); !found {
		t.Fatal("loaded registry has no outbox entry")
	}
}

// TestLoadOrDefaultDispositionRegistry_FallsBackWhenAbsent proves the
// documented fallback: a checkout where STORE-001's file has not landed yet
// still gets a usable registry covering the tables Probe depends on, with
// ok=false so a caller can tell the difference and log it.
func TestLoadOrDefaultDispositionRegistry_FallsBackWhenAbsent(t *testing.T) {
	reg, ok, err := health.LoadOrDefaultDispositionRegistry(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("LoadOrDefaultDispositionRegistry: %v", err)
	}
	if ok {
		t.Fatal("ok = true, want false: the registry file does not exist")
	}
	want := health.DefaultDispositionRegistry()
	if len(reg.Tables) != len(want.Tables) {
		t.Fatalf("fallback registry has %d tables, want %d", len(reg.Tables), len(want.Tables))
	}
	for _, name := range []string{"ledger_event", "projection_checkpoint", "outbox"} {
		if _, found := reg.Table(name); !found {
			t.Errorf("fallback registry has no entry for %q", name)
		}
	}
}

// TestLoadOrDefaultDispositionRegistry_PropagatesParseError proves a
// present-but-malformed registry is reported as an error, not silently
// treated as absent.
func TestLoadOrDefaultDispositionRegistry_PropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "storage-disposition.yaml")
	if err := os.WriteFile(badPath, []byte("tables: [this is not a table list"), 0o644); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}

	if _, _, err := health.LoadOrDefaultDispositionRegistry(badPath); err == nil {
		t.Fatal("expected a parse error for a malformed registry file, got nil")
	}
}
