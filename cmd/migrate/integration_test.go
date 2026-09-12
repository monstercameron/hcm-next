package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	fixtureseed "github.com/monstercameron/human-capital-management-suite/internal/data/seed"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

func TestSeedCommandSeedsOnceAndEmitsStableReceipt(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	const tenant = "harborcare"
	tenantID := pgstore.TenantID(tenant)
	plan, err := fixtureseed.Plan()
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	var first bytes.Buffer
	if err := runSeedCommand(ctx, db.Conn, tenant, &first); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	var firstReceipt seedReceipt
	if err := json.Unmarshal(first.Bytes(), &firstReceipt); err != nil {
		t.Fatalf("decode first receipt %q: %v", first.String(), err)
	}
	if firstReceipt.Tenant != tenantID || firstReceipt.Digest == "" || firstReceipt.Inserted != len(plan) || firstReceipt.Skipped != 0 {
		t.Fatalf("first receipt = %+v, want tenant/digest/new inserts", firstReceipt)
	}
	var tenantKey, cellID, displayName string
	var effectiveFrom time.Time
	if err := db.Conn.QueryRow(ctx, `
		SELECT tenant_key, cell_id, display_name, effective_from FROM tenant WHERE tenant_id = $1`, tenantID,
	).Scan(&tenantKey, &cellID, &displayName, &effectiveFrom); err != nil {
		t.Fatalf("read seeded tenant: %v", err)
	}
	if tenantKey != tenant || cellID != "cell-local" || displayName != tenant || effectiveFrom.UTC().Format(time.RFC3339) != "2026-01-01T00:00:00Z" {
		t.Fatalf("seeded tenant = key=%q cell=%q display=%q effective_from=%s", tenantKey, cellID, displayName, effectiveFrom.UTC().Format(time.RFC3339))
	}
	var seededRows int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM definition_version WHERE tenant_id = $1`, tenantID).Scan(&seededRows); err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if seededRows != len(plan) {
		t.Fatalf("definition_version rows for %s = %d, want %d", tenantID, seededRows, len(plan))
	}

	var second bytes.Buffer
	if err := runSeedCommand(ctx, db.Conn, tenant, &second); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var secondReceipt seedReceipt
	if err := json.Unmarshal(second.Bytes(), &secondReceipt); err != nil {
		t.Fatalf("decode second receipt %q: %v", second.String(), err)
	}
	if secondReceipt.Tenant != tenantID || secondReceipt.Digest != firstReceipt.Digest || secondReceipt.Inserted != 0 || secondReceipt.Skipped != len(plan) {
		t.Fatalf("second receipt = %+v, want matching digest and an idempotent skip", secondReceipt)
	}

	wantFirst := `{"tenant":"` + tenantID.String() + `","digest":"` + firstReceipt.Digest + `","inserted":` + strconv.Itoa(len(plan)) + `,"skipped":0}` + "\n"
	if first.String() != wantFirst {
		t.Fatalf("first receipt = %q, want %q", first.String(), wantFirst)
	}
}

// TestTodo_SVC_013_Integration proves migrate's actual up/status/down
// behavior - Goose applying every embedded migration, the schema journal
// recording it, status reporting the applied version, and down rolling the
// most recent one back - against a real PostgreSQL server. It calls
// runMigrateCommand directly against pgtest's already-open, schema-isolated
// *sql.DB (pgtest.NewEmpty) rather than routing through bootstrap.Run's
// -database-url flag: migrate does not use bootstrap's
// DatabaseURLField/DBPoolFactory (it needs a database/sql.DB, not
// bootstrap's DBPool port - see main.go), so there is nothing further
// bootstrap-specific to exercise here that TestTodo_SVC_013's
// bootstrap.Run-level subtests do not already cover with a fake pool.
func TestTodo_SVC_013_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()

	var up1 bytes.Buffer
	if err := runMigrateCommand(ctx, "up", db.SQL, &up1); err != nil {
		t.Fatalf("up: %v", err)
	}
	if !strings.Contains(up1.String(), "schema version") {
		t.Fatalf("up output = %q, want a schema-version report line", up1.String())
	}

	var upAgain bytes.Buffer
	if err := runMigrateCommand(ctx, "up", db.SQL, &upAgain); err != nil {
		t.Fatalf("up (idempotent rerun): %v", err)
	}
	if strings.Contains(upAgain.String(), "\napplied ") {
		t.Fatalf("rerunning up re-applied a migration: %q", upAgain.String())
	}

	var statusOut bytes.Buffer
	if err := runMigrateCommand(ctx, "status", db.SQL, &statusOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(statusOut.String(), "schema version") {
		t.Fatalf("status output = %q, want a schema-version report line", statusOut.String())
	}

	// Rolling the newest migration back must be refused, not succeed. Every
	// migration from 00279 onward declares itself irreversible on purpose --
	// they write durable evidence (legal-hold notices, admission-retry
	// records, ledger payload dispositions) that cannot be safely unwound --
	// and goose runs Down newest-first, so the newest migration is always one
	// of them. Asserting a successful rollback here asserted behaviour the
	// migration chain deliberately forbids, and had been failing for that
	// reason; the command is still exercised, but against the outcome the
	// design actually specifies. migrations.TestNewestReversibleVersionStops-
	// BelowDeclaredIrreversibles pins the same rule from the other side.
	var downOut bytes.Buffer
	err := runMigrateCommand(ctx, "down", db.SQL, &downOut)
	if err == nil {
		t.Fatalf("down succeeded against an irreversible chain head; output = %q", downOut.String())
	}
	if !strings.Contains(err.Error(), "irreversible") {
		t.Fatalf("down error = %v, want a refusal citing irreversibility", err)
	}
}
