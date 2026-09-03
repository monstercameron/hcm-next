// Command projector is a composition root. It wires packages; it owns no
// semantics.
//
// It runs the projection reconciler (internal/data/projection): each sweep
// finds every checkpoint behind its stream's current head and replays the
// missing ledger events to catch it up (DATA-010). This is the out-of-band
// complement to internal/data/outbox.Commit's synchronous, same-transaction
// advance - a projection that ever falls behind (a missed synchronous
// commit, or a projection registered after events already existed) catches
// up here instead of staying stuck. Restart safety needs nothing special:
// ReconcileOne recomputes "how far behind" from the database on every call,
// so killing and restarting projector mid-sweep just repeats whatever the
// last sweep had not finished.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with -database-url.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/migrate's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

func main() {
	info := buildinfo.Current()
	fmt.Fprintf(os.Stdout, "projector %s revision=%s modified=%t go=%s\n", info.Module, info.Revision, info.Modified, info.GoVersion)

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "projector: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("projector", flag.ContinueOnError)
	databaseURL := fs.String("database-url", os.Getenv(EnvDatabaseURL), "PostgreSQL connection URL ("+EnvDatabaseURL+" if unset)")
	pollInterval := fs.Duration("poll-interval", 2*time.Second, "how long to sleep between sweeps that found nothing behind")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *databaseURL == "" {
		return fmt.Errorf("%s is not set; pass -database-url or set the environment variable", EnvDatabaseURL)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	reconciler := projection.NewReconciler(pool, datalogger.NewReader())
	log.Printf("projector: reconciling projections (poll-interval=%s)", *pollInterval)

	for {
		select {
		case <-ctx.Done():
			log.Print("projector: shutting down")
			return nil
		default:
		}

		didWork, err := sweep(ctx, pool, reconciler)
		if err != nil {
			log.Printf("projector: sweep: %v", err)
		}
		if didWork {
			continue
		}
		select {
		case <-ctx.Done():
			log.Print("projector: shutting down")
			return nil
		case <-time.After(*pollInterval):
		}
	}
}

// sweep catches up every projection currently behind its stream head, and
// reports whether any work was found.
func sweep(ctx context.Context, pool *pgxpool.Pool, reconciler *projection.Reconciler) (bool, error) {
	due, err := projection.ReconcileDue(ctx, pool)
	if err != nil {
		return false, fmt.Errorf("list due: %w", err)
	}

	didWork := false
	for _, target := range due {
		applied, err := reconciler.ReconcileOne(ctx, target)
		if err != nil {
			log.Printf("projector: reconcile %s/%s: %v", target.ProjectionName, target.StreamKey, err)
			continue
		}
		if applied > 0 {
			didWork = true
			log.Printf("projector: caught up %s/%s by %d event(s)", target.ProjectionName, target.StreamKey, applied)
		}
	}
	return didWork, nil
}
