package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
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

	var downOut bytes.Buffer
	if err := runMigrateCommand(ctx, "down", db.SQL, &downOut); err != nil {
		t.Fatalf("down: %v", err)
	}
	if !strings.Contains(downOut.String(), "rolled back") {
		t.Fatalf("down output = %q, want a rolled-back report line", downOut.String())
	}
}
