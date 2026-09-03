// Command worker is a composition root. It wires packages; it owns no
// semantics.
//
// It runs the transactional outbox consumer (internal/data/outbox) across
// every active tenant: each sweep claims due messages (PENDING, or IN_FLIGHT
// past their lease) and dispatches them. A message failing dispatch returns
// to PENDING for retry; the target database's own restart safety is
// Consumer.Poll's lease, not anything this process remembers, so killing and
// restarting worker loses nothing and never double-applies a delivered
// message (DATA-008).
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/migrate's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

func main() {
	info := buildinfo.Current()
	fmt.Fprintf(os.Stdout, "worker %s revision=%s modified=%t go=%s\n", info.Module, info.Revision, info.Modified, info.GoVersion)

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	databaseURL := fs.String("database-url", os.Getenv(EnvDatabaseURL), "PostgreSQL connection URL ("+EnvDatabaseURL+" if unset)")
	pollInterval := fs.Duration("poll-interval", 2*time.Second, "how long to sleep between sweeps that found no due work")
	lease := fs.Duration("lease", outbox.DefaultLease, "how long a claimed message stays IN_FLIGHT before another sweep may reclaim it")
	batchSize := fs.Int("batch-size", outbox.DefaultBatchSize, "maximum messages one sweep claims per tenant")
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

	consumer := outbox.NewConsumer(pool, outbox.WithLease(*lease), outbox.WithBatchSize(*batchSize))
	log.Printf("worker: dispatching outbox messages (poll-interval=%s lease=%s batch-size=%d)", *pollInterval, *lease, *batchSize)

	for {
		select {
		case <-ctx.Done():
			log.Print("worker: shutting down")
			return nil
		default:
		}

		didWork, err := sweep(ctx, pool, consumer)
		if err != nil {
			log.Printf("worker: sweep: %v", err)
		}
		if didWork {
			continue
		}
		select {
		case <-ctx.Done():
			log.Print("worker: shutting down")
			return nil
		case <-time.After(*pollInterval):
		}
	}
}

// sweep dispatches one batch of due messages for every active tenant, and
// reports whether any tenant had work.
func sweep(ctx context.Context, pool *pgxpool.Pool, consumer *outbox.Consumer) (bool, error) {
	tenants, err := activeTenants(ctx, pool)
	if err != nil {
		return false, fmt.Errorf("list tenants: %w", err)
	}

	didWork := false
	for _, tenant := range tenants {
		batch, err := consumer.Poll(ctx, tenant)
		if err != nil {
			log.Printf("worker: poll tenant %s: %v", tenant, err)
			continue
		}
		for _, msg := range batch {
			didWork = true
			if err := dispatch(ctx, msg); err != nil {
				log.Printf("worker: dispatch %s: %v", msg.OutboxID, err)
				if ackErr := consumer.Fail(ctx, tenant, msg.OutboxID, err); ackErr != nil {
					log.Printf("worker: fail %s: %v", msg.OutboxID, ackErr)
				}
				continue
			}
			if err := consumer.Ack(ctx, tenant, msg.OutboxID); err != nil {
				log.Printf("worker: ack %s: %v", msg.OutboxID, err)
			}
		}
	}
	return didWork, nil
}

// dispatch delivers one outbox message. P1A has no external distribution
// target wired yet (no funded consumer reads the outbox outside this
// process); this composition root logs the delivery so the at-least-once,
// restart-safe mechanics are exercised end to end in the real binary. A real
// destination (a queue, a webhook) plugs in here without changing
// internal/data/outbox.
func dispatch(_ context.Context, msg outbox.Record) error {
	log.Printf("worker: dispatched effect=%s schema=%s bytes=%d", msg.EffectIdentity, msg.SchemaRef, len(msg.Payload))
	return nil
}

func activeTenants(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, `SELECT tenant_id FROM tenant WHERE status = 'ACTIVE'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
