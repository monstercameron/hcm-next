package storeboundaries_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/storeboundaries"
)

// --- shared fixture helpers --------------------------------------------------

func mustPackage(t *testing.T, importPath string, imports []string, sources map[string]string) storeboundaries.PackageSource {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for name, src := range sources {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files[name] = f
	}
	return storeboundaries.PackageSource{ImportPath: importPath, Imports: imports, Files: files, Fset: fset}
}

func strPtr(s string) *string { return &s }

// fixtureRegistry declares one tenant-scoped table ("widget") and one
// control-plane, non-tenant table ("catalog"), enough for every RED/GREEN
// scenario this suite exercises without depending on the real, evolving
// STORE-001 registry content (that dependency belongs to TestTodo_STORE_002
// and TestTodo_STORE_002_Integration alone).
func fixtureRegistry() *storagedisposition.Registry {
	return &storagedisposition.Registry{
		Tables: []storagedisposition.TableEntry{
			{Table: "widget", TenantScopingColumn: strPtr("tenant_id"), EncryptionClass: storagedisposition.EncryptionPlatformManaged},
			{Table: "catalog", TenantScopingColumn: nil, EncryptionClass: storagedisposition.EncryptionPlatformManaged},
		},
	}
}

// --- TestTodo_STORE_002 (PRIMARY) -------------------------------------------

// TestTodo_STORE_002 is the STORE-002 primary test. It runs every rule from
// the TEST clause over the real, checked-in tree and asserts the exact,
// reviewed set of raw findings: TestTodo_STORE_002_Integration then proves
// the same scan comes back clean once the reviewed allowlist in
// allowlist.yaml is applied. A new raw finding beyond the six understood
// ones below -- in an already-listed package or a new one -- fails this
// test, which is the point: this suite is the enforcement mechanism, so a
// silent widening of any of these six is exactly what it exists to catch.
func TestTodo_STORE_002(t *testing.T) {
	root := repopath.RootDir()
	report, err := storeboundaries.Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// TEST clause (1): tenant scoping. Every one of these six is reviewed
	// and explained in allowlist.yaml; see that file for why each is not a
	// real cross-tenant boundary break.
	wantTenant := map[string]bool{
		"kit.go: INSERT Kit.Load touches tenant-scoped table(s) ledger_event with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package github.com/monstercameron/hcm-next/internal/data/partition":                                                                                                                                                                                                          true,
		"kit.go: OTHER Kit.Build touches tenant-scoped table(s) ledger_event with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package github.com/monstercameron/hcm-next/internal/data/partition":                                                                                                                                                                                                          true,
		"kit.go: SELECT Kit.fetchJSON touches tenant-scoped table(s) ledger_event with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package github.com/monstercameron/hcm-next/internal/data/partition":                                                                                                                                                                                                     true,
		"scheduling.go: SELECT TimerStore.list touches tenant-scoped table(s) workflow_timer with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package github.com/monstercameron/hcm-next/internal/data/runtimestate":                                                                                                                                                                                       true,
		"probe.go: SELECT Probe.probeGeneric touches tenant-scoped table(s) ledger_event,outbox,projection_checkpoint,stream_head,tenant with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package github.com/monstercameron/hcm-next/internal/data/health":                                                                                                                                                 true,
		"store.go: UPDATE put touches tenant-scoped table(s) assignment,budget_reservation,compensation_band,compensation_component,compensation_package,employment,identity_claim,job,job_position,legal_entity,organization_unit,person,position_occupancy,worker,workforce_budget with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package github.com/monstercameron/hcm-next/internal/data/aggregates": true,
	}
	got := map[string]bool{}
	for _, f := range report.TenantScope {
		got[f.String()] = true
	}
	if len(got) != len(wantTenant) {
		t.Errorf("tenant-scope findings = %d, want %d", len(got), len(wantTenant))
	}
	for msg := range wantTenant {
		if !got[msg] {
			t.Errorf("expected (and allowlisted) finding missing: %s", msg)
		}
	}
	for msg := range got {
		if !wantTenant[msg] {
			t.Errorf("NEW, un-reviewed tenant-scope finding (add it to allowlist.yaml with an owner/rationale, or fix the adapter): %s", msg)
		}
	}

	// TEST clause (2): no adapter opens its own pool. internal/data/pgxadapter
	// is the only package under the scanned roots allowed to import pgxpool
	// directly (confirmed separately: internal/data/pgtest imports pgx/v5
	// and pgx/v5/stdlib for its embedded-Postgres bootstrap, never pgxpool).
	if len(report.PoolImport) != 0 {
		for _, f := range report.PoolImport {
			t.Errorf("pool-import violation: %s", f.String())
		}
	}

	// TEST clause (3): encryption class. internal/data and internal/kernel
	// export no interface with an Encrypt*/Decrypt* method today (grepped
	// directly: internal/data/store.Scope.EncryptionClass only validates the
	// registry's two enum string values, it never encrypts anything), and
	// the registry classifies zero tables FIELD_LEVEL. Those two facts are
	// mutually consistent -- there is nothing yet to enforce -- which is
	// exactly the registry-consistency half this rule can actually check
	// today; see EncryptionGap's doc comment for what closes this gap.
	if len(report.Encryption.PortEvidence) != 0 {
		t.Errorf("found unexpected encryption port evidence %v; TEST clause (3) can now enforce a real per-write rule instead of only registry consistency -- update EvaluateTenantScope/EncryptionGap accordingly", report.Encryption.PortEvidence)
	}
	if len(report.Encryption.FieldLevelTables) != 0 {
		t.Errorf("registry now declares FIELD_LEVEL table(s) %v with no encryption port found (PortEvidence is empty): this is a real STORE-002 gap, not a documented one -- add the port or block the write", report.Encryption.FieldLevelTables)
	}
	if !report.Encryption.Consistent() {
		t.Error("EncryptionGap.Consistent() = false, want true (see the two checks above for which half broke)")
	}
}

// --- TestTodo_STORE_002_Integration ------------------------------------------

// TestTodo_STORE_002_Integration proves the reviewed allowlist in
// allowlist.yaml, applied to the same real-tree scan TestTodo_STORE_002
// checks raw, leaves zero outstanding violations, and that the allowlist
// itself is well-formed (every exception has an owner, a rationale and a
// follow-up plan, per CheckWithPolicy's requirement -- an incomplete entry
// waives nothing).
func TestTodo_STORE_002_Integration(t *testing.T) {
	root := repopath.RootDir()
	report, err := storeboundaries.Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	policy, err := storeboundaries.LoadPolicy(filepath.Join(root, "tools", "policy", "storeboundaries", "allowlist.yaml"))
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if len(policy.Exceptions) != 4 {
		t.Fatalf("allowlist.yaml has %d exceptions, want 4 (one per reviewed package)", len(policy.Exceptions))
	}
	for _, e := range policy.Exceptions {
		if e.Owner == "" || e.Rationale == "" || (e.ReplacementPlan == "" && e.FollowupTodo == "") || e.Expiry == "" {
			t.Errorf("incomplete exception for %s/%s: every entry needs owner, rationale, expiry and a replacement_plan or followup_todo", e.Kind, e.Package)
		}
	}

	remaining := storeboundaries.CheckWithPolicy(report.Violations(), policy)
	if len(remaining) != 0 {
		for _, v := range remaining {
			t.Errorf("STORE-002 violation not covered by allowlist.yaml: %s", v)
		}
	}
}

// --- TestTodo_STORE_002_Fault -------------------------------------------------

// TestTodo_STORE_002_Fault proves the RED clause: a statement with a
// missing, forged-by-omission, or otherwise absent tenant context is
// caught, and the two documented escape hatches (an explicit tenant
// predicate in the statement text, and the package-level
// tenancy-import/dbport.Tx signal) each independently clear a statement
// that would otherwise be flagged.
func TestTodo_STORE_002_Fault(t *testing.T) {
	reg := fixtureRegistry()

	t.Run("missing tenant predicate is a violation", func(t *testing.T) {
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

import "fmt"

func Delete(db Conn, id string) error {
	_, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE row_id = $1", "widget"), id)
	return err
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
		}
		if findings[0].Verb != storeboundaries.VerbDelete || len(findings[0].Tables) != 1 || findings[0].Tables[0] != "widget" {
			t.Errorf("finding = %+v, want a DELETE on [widget]", findings[0])
		}
	})

	t.Run("dynamic table resolved via Tier B is still caught", func(t *testing.T) {
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

import "fmt"

func remove(db Conn, table, id string) error {
	_, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE row_id = $1", table), id)
	return err
}

func RemoveWidget(db Conn, id string) error {
	return remove(db, "widget", id)
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
		}
		if len(findings[0].Tables) != 1 || findings[0].Tables[0] != "widget" {
			t.Errorf("finding.Tables = %v, want [widget] (resolved from the sibling call site's literal argument)", findings[0].Tables)
		}
	})

	t.Run("explicit tenant predicate clears it", func(t *testing.T) {
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

import "fmt"

func Delete(db Conn, tenantID, id string) error {
	_, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE tenant_id = $1 AND row_id = $2", "widget"), tenantID, id)
	return err
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(findings), findings)
		}
	})

	t.Run("package importing tenancy clears it despite no in-statement evidence", func(t *testing.T) {
		pkg := mustPackage(t, "example.com/adapter", []string{"github.com/monstercameron/hcm-next/internal/data/tenancy"}, map[string]string{
			"adapter.go": `package adapter

import "fmt"

func Delete(db Conn, id string) error {
	_, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE row_id = $1", "widget"), id)
	return err
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 0 {
			t.Errorf("got %d findings, want 0 (package imports internal/data/tenancy): %+v", len(findings), findings)
		}
	})

	t.Run("literal dbport.Tx parameter clears it despite no in-statement evidence", func(t *testing.T) {
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

import "fmt"

func Delete(tx dbport.Tx, id string) error {
	_, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE row_id = $1", "widget"), id)
	return err
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 0 {
			t.Errorf("got %d findings, want 0 (parameter type is literally dbport.Tx): %+v", len(findings), findings)
		}
	})

	t.Run("non-tenant table is never a finding regardless of text", func(t *testing.T) {
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

func List(db Conn) {
	db.Query("SELECT * FROM catalog")
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 0 {
			t.Errorf("got %d findings, want 0 (catalog is not tenant-scoped in the fixture registry): %+v", len(findings), findings)
		}
	})
}

// --- TestTodo_STORE_002_Security ----------------------------------------------

// TestTodo_STORE_002_Security proves the two boundary-shaped rules: no
// adapter opens its own pgxpool pool other than internal/data/pgxadapter
// (TEST clause (2)), and a SELECT cannot buy tenant-scoping credit merely
// by mentioning the tenant_id column without filtering on it -- the
// column-selection/column-filter distinction that would otherwise let a
// statement quietly read cross-tenant rows while superficially looking
// tenant-aware.
func TestTodo_STORE_002_Security(t *testing.T) {
	t.Run("non-pgxadapter package importing pgxpool is a violation", func(t *testing.T) {
		pkg := storeboundaries.PackageSource{
			ImportPath: "github.com/monstercameron/hcm-next/internal/data/somepkg",
			Imports:    []string{"github.com/jackc/pgx/v5/pgxpool"},
		}
		f := storeboundaries.EvaluatePoolImport(pkg)
		if f == nil {
			t.Fatal("want a pool-import finding, got nil")
		}
		if f.Package != pkg.ImportPath {
			t.Errorf("finding.Package = %q, want %q", f.Package, pkg.ImportPath)
		}
	})

	t.Run("pgxadapter itself importing pgxpool is not a violation", func(t *testing.T) {
		pkg := storeboundaries.PackageSource{
			ImportPath: "github.com/monstercameron/hcm-next/internal/data/pgxadapter",
			Imports:    []string{"github.com/jackc/pgx/v5/pgxpool"},
		}
		if f := storeboundaries.EvaluatePoolImport(pkg); f != nil {
			t.Errorf("want no finding for pgxadapter itself, got %+v", f)
		}
	})

	t.Run("a package neither importing pgxpool nor being pgxadapter is clean", func(t *testing.T) {
		pkg := storeboundaries.PackageSource{
			ImportPath: "github.com/monstercameron/hcm-next/internal/data/somepkg",
			Imports:    []string{"github.com/jackc/pgx/v5"},
		}
		if f := storeboundaries.EvaluatePoolImport(pkg); f != nil {
			t.Errorf("want no finding, got %+v", f)
		}
	})

	t.Run("selecting the tenant_id column is not a filter and is still a violation", func(t *testing.T) {
		reg := fixtureRegistry()
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

func ListAll(db Conn) {
	db.Query("SELECT tenant_id, name FROM widget ORDER BY name")
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 1 {
			t.Fatalf("got %d findings, want 1 (reading the tenant_id column back is not a tenant filter): %+v", len(findings), findings)
		}
	})

	t.Run("naming tenant_id in an INSERT column list is credited", func(t *testing.T) {
		reg := fixtureRegistry()
		pkg := mustPackage(t, "example.com/adapter", nil, map[string]string{
			"adapter.go": `package adapter

import "fmt"

func Insert(db Conn, tenantID, id string) error {
	_, err := db.Exec(fmt.Sprintf("INSERT INTO %s (tenant_id, row_id) VALUES ($1, $2)", "widget"), tenantID, id)
	return err
}
`,
		})
		findings := storeboundaries.EvaluateTenantScope(pkg, reg)
		if len(findings) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(findings), findings)
		}
	})
}

// TestLoadPolicy_RejectsIncompleteExceptions proves CheckWithPolicy's own
// documented safety property directly: an exception missing any required
// field waives nothing, so allowlist.yaml cannot silently grow a
// standing, unreviewed exemption through a partially-filled entry.
func TestLoadPolicy_RejectsIncompleteExceptions(t *testing.T) {
	v := storeboundaries.Violation{Kind: storeboundaries.KindTenantScope, Package: "example.com/p", Message: "m"}
	base := storeboundaries.Exception{
		Kind: storeboundaries.KindTenantScope, Package: "example.com/p",
		Owner: "team", Rationale: "why", FollowupTodo: "later", Expiry: "2099-01-01",
	}
	if got := storeboundaries.CheckWithPolicy([]storeboundaries.Violation{v}, storeboundaries.Policy{PolicyDate: "2026-01-01", Exceptions: []storeboundaries.Exception{base}}); len(got) != 0 {
		t.Fatalf("complete, active exception should waive the violation, got %v", got)
	}

	missingOwner := base
	missingOwner.Owner = ""
	if got := storeboundaries.CheckWithPolicy([]storeboundaries.Violation{v}, storeboundaries.Policy{PolicyDate: "2026-01-01", Exceptions: []storeboundaries.Exception{missingOwner}}); len(got) != 1 {
		t.Errorf("missing owner should waive nothing, got %v", got)
	}

	expired := base
	expired.Expiry = "2020-01-01"
	if got := storeboundaries.CheckWithPolicy([]storeboundaries.Violation{v}, storeboundaries.Policy{PolicyDate: "2026-01-01", Exceptions: []storeboundaries.Exception{expired}}); len(got) != 1 {
		t.Errorf("expired exception should waive nothing, got %v", got)
	}

	wrongPackage := base
	wrongPackage.Package = "example.com/other"
	if got := storeboundaries.CheckWithPolicy([]storeboundaries.Violation{v}, storeboundaries.Policy{PolicyDate: "2026-01-01", Exceptions: []storeboundaries.Exception{wrongPackage}}); len(got) != 1 {
		t.Errorf("exception for a different package should waive nothing, got %v", got)
	}
}
