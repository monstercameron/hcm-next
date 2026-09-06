// migrate.go supplies this command's adapter for internal/application's
// Migrator port.
//
// It lives in cmd rather than in the application composition root on purpose:
// definitions/architecture/library-firewall.yaml (LIB-008) confines
// github.com/pressly/goose/v3 to the migrations and cmd roots, so the
// application root states the dependency ("bring the schema to the target
// version before the listeners start") and this command supplies the one
// implementation of it.

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/migrations"
)

// migrateUp applies every pending migration with Goose over the embedded tree.
//
// It is deliberately the plain apply. cmd/migrate owns the journaled one
// (DB-006: artifact digest, tool version, per-migration checksum verification,
// owner, start and finish), and duplicating that here would create a second
// migration authority - exactly the "competing migration roots" NEXT-004 names
// as a failure.
func migrateUp(ctx context.Context, url string, logger bootstrap.Logger) error {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse the database URL: %w", err)
	}
	db := stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(
		goose.DialectPostgres, db, migrations.FS,
		goose.WithVerbose(false),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("build the migration provider: %w", err)
	}
	applied, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		return err
	}
	logger.Info("hcmnext.schema_applied", "version", target, "applied_this_start", len(applied))
	return nil
}
