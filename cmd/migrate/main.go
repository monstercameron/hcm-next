// Command migrate is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration and hands bootstrap one one-shot Workload (migrate.go) that
// applies the embedded SQL-first migration tree with Goose over pgx and
// journals every step per DB-006: artifact digest, tool version, checksum,
// owner, start and finish, and the resulting status.
//
//	migrate up      apply every pending migration
//	migrate down    roll back the most recently applied migration
//	migrate status  report the schema version, digest and per-migration state
//	migrate seed    load the deterministic Promotion fixture for -tenant
//	migrate demo-people load HarborCare's demo workforce and processed photos
//
// migrate is not a long-running server: process-roles.yaml marks it
// "operator-invoked" with no readiness probe and "not applicable" drain, so
// this composition root serves no health endpoint and Build returns a
// single Workload that runs once and returns rather than looping. Unlike
// every other role's settings, the up|down|status subcommand is a plain
// positional argument (not a -flag), read directly from os.Args before
// bootstrap.Run ever parses Spec.ConfigFields - the same way bootstrap.Run
// itself pre-empts Spec.HealthAddr resolution in cmd/worker and
// cmd/projector.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with
// -database-url.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// EnvDatabaseURL names the server this command migrates, matching
// cmd/worker's and cmd/projector's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

const fieldTenant = "tenant"

const (
	fieldPhotoSource = "photo-source"
	fieldAssetDir    = "asset-dir"
	fieldOriginalDir = "original-dir"
)

// migrationTimeout bounds one migrate invocation, matching the original
// command's own budget. Unlike the original (which ran against a bare
// context.Background(), deaf to OS signals), this timeout is derived from
// bootstrap's own workload context, so a SIGINT/SIGTERM during a long
// migration now cancels it too rather than only a plain deadline.
const migrationTimeout = 30 * time.Minute

func main() {
	command, rest := splitCommand(os.Args[1:])
	os.Exit(bootstrap.Run(context.Background(), spec(command, rest)))
}

// splitCommand peels the leading positional subcommand off args, matching
// migrate's "migrate up|down|status [flags...]" invocation. An empty args
// yields an empty command, which validateCommand below reports as a usage
// error.
func splitCommand(args []string) (command string, rest []string) {
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}

// migrateConfigFields declares every flag/env-backed value this role
// accepts: only the database URL - migrate has no health endpoint, poll
// interval or other tunable.
func migrateConfigFields() []bootstrap.Field {
	return []bootstrap.Field{
		{
			Name:   "database-url",
			Env:    EnvDatabaseURL,
			Usage:  "PostgreSQL connection URL (" + EnvDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:  fieldTenant,
			Usage: "tenant slug to seed (required by seed and demo-people)",
		},
		{
			Name:  fieldPhotoSource,
			Usage: "directory containing generated hc-NNN.png source photos (required by demo-people)",
		},
		{
			Name:    fieldAssetDir,
			Usage:   "workspace asset directory for retained originals and display proxies",
			Default: "internal/humanwork/workspace/assets",
		},
		{
			Name:    fieldOriginalDir,
			Usage:   "non-public directory for byte-exact retained profile-photo originals",
			Default: "demo-assets/profile-originals",
		},
	}
}

// spec builds the full migrate Spec for one invocation's subcommand and
// remaining flag arguments.
func spec(command string, rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role:         bootstrap.RoleMigrate,
		Args:         rest,
		ConfigFields: migrateConfigFields(),
		Validate:     validateConfig(command),
		// No DatabaseURLField/DBPoolFactory: Goose and schema.Journal both
		// need a database/sql.DB (via pgx's stdlib adapter), not
		// bootstrap's narrower DBPool port, so this role opens its own
		// connection inside the Workload (openMigrateDB in migrate.go)
		// instead. The seed workload opens its own pgx adapter connection for
		// the same reason: it needs the transaction-scoped dbport adapter, not
		// bootstrap's narrower DBPool port.
		// No HealthAddr: migrate is operator-invoked and short-lived
		// (process-roles.yaml: "not applicable" liveness/readiness,
		// drain_policy "not applicable"), so it serves no health endpoint.
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			url := deps.Values.String("database-url")
			wl := bootstrap.Workload{
				Name: "migrate-" + command,
				Run: func(ctx context.Context) error {
					ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
					defer cancel()

					if command == "seed" || command == "demo-people" {
						conn, err := openSeedDB(ctx, url)
						if err != nil {
							return err
						}
						defer func() { _ = conn.Close(ctx) }()

						if command == "demo-people" {
							return runDemoPeopleCommand(ctx, conn, deps.Values.String(fieldTenant), deps.Values.String(fieldPhotoSource), deps.Values.String(fieldAssetDir), deps.Values.String(fieldOriginalDir), os.Stdout)
						}
						return runSeedCommand(ctx, conn, deps.Values.String(fieldTenant), os.Stdout)
					}

					db, err := openMigrateDB(ctx, url)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()

					return runMigrateCommand(ctx, command, db, os.Stdout)
				},
			}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{wl}}, nil
		},
	}
}

// validateConfig fails config resolution (before the Workload ever opens a
// connection) on an unrecognized subcommand or a missing database URL,
// matching the original command's own usage checks.
func validateConfig(command string) func(*bootstrap.Values) error {
	return func(v *bootstrap.Values) error {
		switch command {
		case "up", "down", "status":
		case "seed":
			if v.String(fieldTenant) == "" {
				return fmt.Errorf("-%s is required for the seed subcommand", fieldTenant)
			}
		case "demo-people":
			if v.String(fieldTenant) == "" {
				return fmt.Errorf("-%s is required for the demo-people subcommand", fieldTenant)
			}
			if v.String(fieldPhotoSource) == "" {
				return fmt.Errorf("-%s is required for the demo-people subcommand", fieldPhotoSource)
			}
		case "":
			return fmt.Errorf("usage: migrate up|down|status|seed|demo-people")
		default:
			return fmt.Errorf("unknown command %q; usage: migrate up|down|status|seed|demo-people", command)
		}
		if v.String("database-url") == "" {
			return fmt.Errorf("%s is not set; pass -database-url or set the environment variable", EnvDatabaseURL)
		}
		return nil
	}
}
