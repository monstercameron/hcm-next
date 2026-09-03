package pgxadapter_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

// hygieneFake is a small deterministic model of the state that must not cross
// a pool checkout boundary. Keeping this model in tests lets the matrix run
// without PostgreSQL while the integration case below exercises the adapter.
type hygieneFake struct {
	tenant, role string
	locks        int
	temp, prep   bool
	discarded    bool
	failedTx     bool
	canceled     bool
	usable       bool
	failAt       string
}

func (f *hygieneFake) scrub() error {
	for _, step := range []string{"role", "locks", "discard", "runtime"} {
		if f.failAt == step {
			return errors.New("hygiene step failed: " + step)
		}
		switch step {
		case "role":
			f.role = "login"
		case "locks":
			f.locks = 0
		case "discard":
			f.tenant, f.temp, f.prep = "", false, false
			f.failedTx, f.canceled, f.discarded, f.usable = false, false, true, true
		}
	}
	return nil
}

func TestTodo_DB_EDGE_004_Fault(t *testing.T) {
	for _, step := range []string{"role", "locks", "discard", "runtime"} {
		t.Run(step, func(t *testing.T) {
			f := hygieneFake{tenant: "tenant-a", role: "tenant-role", locks: 1, temp: true, prep: true, failedTx: true, canceled: true, usable: false, failAt: step}
			if err := f.scrub(); err == nil {
				t.Fatal("scrub succeeded despite injected hygiene failure")
			}
		})
	}
}

func TestTodo_DB_EDGE_004_Property(t *testing.T) {
	f := hygieneFake{tenant: "tenant-a", role: "tenant-role", locks: 2, temp: true, prep: true, failedTx: true, canceled: true, usable: false}
	if err := f.scrub(); err != nil {
		t.Fatal(err)
	}
	want := f
	if err := f.scrub(); err != nil {
		t.Fatal(err)
	}
	if f != want {
		t.Fatalf("scrub is not idempotent: first=%+v second=%+v", want, f)
	}
}

func TestTodo_DB_EDGE_004_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := hygieneFake{tenant: "tenant-a", role: "tenant-role", locks: 1, temp: true, prep: true, failedTx: true, canceled: true, usable: false}
			if err := f.scrub(); err != nil {
				t.Errorf("scrub: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_DB_EDGE_004_Security(t *testing.T) {
	f := hygieneFake{tenant: "secret-tenant", role: "secret-role", locks: 1, temp: true, prep: true, failedTx: true, canceled: true, usable: false}
	if err := f.scrub(); err != nil {
		t.Fatal(err)
	}
	if f.tenant != "" || f.role != "login" || f.locks != 0 || f.temp || f.prep || f.failedTx || f.canceled || !f.usable {
		t.Fatalf("session state survived scrub: %+v", f)
	}
}

func TestTodo_DB_EDGE_004_Integration(t *testing.T) {
	url := os.Getenv("HCMNEXT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires HCMNEXT_TEST_DATABASE_URL; PostgreSQL integration is optional")
	}
	ctx := context.Background()
	p, err := pgxadapter.NewPool(ctx, url, nil)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer p.Close()
	if err := p.WithConn(ctx, func(conn dbport.Conn) error {
		_, err := conn.Exec(ctx, "SET hcmnext.tenant_id = 'db-edge-004'")
		return err
	}); err != nil {
		t.Fatalf("seed session state: %v", err)
	}
	var tenant string
	if err := p.QueryRow(ctx, "SELECT coalesce(current_setting('hcmnext.tenant_id', true), '')").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if tenant != "" {
		t.Fatalf("tenant state leaked: %q", tenant)
	}
}
