package wakeup

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestPostgresNotificationLossAndStartupRaceCannotLoseDurableWork(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wakeup-primary")
	reader := db.NewConn(t)
	firstScanStarted := make(chan struct{})
	releaseFirstScan := make(chan struct{})
	var once sync.Once
	var scansMu sync.Mutex
	var scans int
	scan := func(ctx context.Context, gotTenant uuid.UUID) error {
		once.Do(func() {
			close(firstScanStarted)
			<-releaseFirstScan
		})
		var count int
		if err := reader.QueryRow(ctx, `
			SELECT count(*) FROM workflow_ready_work
			WHERE tenant_id = $1 AND ready_state = 'READY' AND eligible_at <= now()`, gotTenant).Scan(&count); err != nil {
			return err
		}
		scansMu.Lock()
		scans++
		scansMu.Unlock()
		if count == 0 {
			return fmt.Errorf("durable scan found no ready work")
		}
		return nil
	}

	listenerResult := make(chan struct {
		listener *Listener
		err      error
	}, 1)
	go func() {
		listener, err := NewListener(context.Background(), ListenerConfig{
			URL:              db.URL,
			RuntimeParams:    map[string]string{"search_path": db.Schema},
			TenantIDs:        []uuid.UUID{tenant},
			RecoveryInterval: 40 * time.Millisecond,
			Scan:             scan,
		})
		listenerResult <- struct {
			listener *Listener
			err      error
		}{listener, err}
	}()
	<-firstScanStarted
	// This commit lands after LISTEN has been issued and while the required
	// post-subscription scan is paused. No NOTIFY is sent: the scan must find it.
	insertReadyWork(t, db, tenant, uuid.New())
	close(releaseFirstScan)
	result := <-listenerResult
	if result.err != nil {
		t.Fatalf("listener startup: %v", result.err)
	}
	listener := result.listener
	t.Cleanup(func() { _ = listener.Close(context.Background()) })

	// A second committed row also suppresses its notification. The bounded
	// recovery scan, not an ephemeral hint, must find it.
	insertReadyWork(t, db, tenant, uuid.New())
	wake, err := listener.Wait(context.Background())
	if err != nil {
		t.Fatalf("periodic durable catch-up: %v", err)
	}
	if wake.Reason != ReasonRecovery {
		t.Fatalf("wake reason = %q, want %q", wake.Reason, ReasonRecovery)
	}
	scansMu.Lock()
	gotScans := scans
	scansMu.Unlock()
	if gotScans < 2 {
		t.Fatalf("durable scan count = %d, want startup and recovery scans", gotScans)
	}
}

func TestTodo_DB_EDGE_001_Property(t *testing.T) {
	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	channel := ChannelForTenant(tenant)
	if channel == "" || channel != ChannelForTenant(tenant) {
		t.Fatalf("channel is not deterministic: %q", channel)
	}
	if channel == tenant.String() || len(channel) > 63 {
		t.Fatalf("channel %q is not namespaced and bounded", channel)
	}
	if ChannelForTenant(uuid.Nil) != "" {
		t.Fatal("nil tenant produced a channel")
	}
	if _, err := NewListener(context.Background(), ListenerConfig{URL: "postgres://unused", Scan: func(context.Context, uuid.UUID) error { return nil }}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid listener interval error = %v, want ErrInvalid", err)
	}
}

func TestTodo_DB_EDGE_001_Race(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wakeup-race")
	insertReadyWork(t, db, tenant, uuid.New())
	reader := db.NewConn(t)
	var readsMu sync.Mutex
	reads := 0
	scan := func(ctx context.Context, gotTenant uuid.UUID) error {
		var count int
		if err := reader.QueryRow(ctx, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND ready_state = 'READY'`, gotTenant).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("ready work count = %d, want 1", count)
		}
		readsMu.Lock()
		reads++
		readsMu.Unlock()
		return nil
	}
	listener, err := NewListener(context.Background(), ListenerConfig{
		URL: db.URL, RuntimeParams: map[string]string{"search_path": db.Schema},
		TenantIDs: []uuid.UUID{tenant}, RecoveryInterval: time.Second, Scan: scan,
	})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	notifier := NewNotifier(db.Conn)
	for range 3 {
		if err := notifier.NotifyCommitted(context.Background(), tenant); err != nil {
			t.Fatalf("duplicate hint: %v", err)
		}
		wake, err := listener.Wait(context.Background())
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if wake.Reason != ReasonHint || wake.TenantID != tenant {
			t.Fatalf("wake = %+v, want tenant hint", wake)
		}
	}
	readsMu.Lock()
	gotReads := reads
	readsMu.Unlock()
	if gotReads != 4 { // initial durable read plus one read per duplicate hint
		t.Fatalf("durable reads = %d, want 4", gotReads)
	}
}

func TestTodo_DB_EDGE_001_Integration(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wakeup-integration")
	reader := db.NewConn(t)
	var reads int
	scan := func(ctx context.Context, gotTenant uuid.UUID) error {
		var count int
		if err := reader.QueryRow(ctx, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND ready_state = 'READY'`, gotTenant).Scan(&count); err != nil {
			return err
		}
		reads++
		if reads > 1 && count != 1 {
			return fmt.Errorf("durable count = %d, want 1", count)
		}
		return nil
	}
	listener, err := NewListener(context.Background(), ListenerConfig{
		URL: db.URL, RuntimeParams: map[string]string{"search_path": db.Schema},
		TenantIDs: []uuid.UUID{tenant}, RecoveryInterval: time.Second, Scan: scan,
	})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	rowID := uuid.New()
	insertReadyWork(t, db, tenant, rowID)
	if err := NewNotifier(db.Conn).NotifyCommitted(context.Background(), tenant); err != nil {
		t.Fatalf("notify after commit: %v", err)
	}
	wake, err := listener.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if wake.Reason != ReasonHint || wake.TenantID != tenant || reads != 2 {
		t.Fatalf("wake=%+v reads=%d, want one post-commit hint scan after startup scan", wake, reads)
	}
}

func TestTodo_DB_EDGE_001_Fault(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wakeup-fault")
	reader := db.NewConn(t)
	reads := 0
	scan := func(ctx context.Context, gotTenant uuid.UUID) error {
		var count int
		if err := reader.QueryRow(ctx, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND ready_state = 'READY'`, gotTenant).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("spurious hint found unexpected work: %d", count)
		}
		reads++
		return nil
	}
	listener, err := NewListener(context.Background(), ListenerConfig{
		URL: db.URL, RuntimeParams: map[string]string{"search_path": db.Schema},
		TenantIDs: []uuid.UUID{tenant}, RecoveryInterval: time.Second, Scan: scan,
	})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	// A forged payload names a pretend row. It must be ignored; the only action
	// is one durable read for the tenant channel.
	if _, err := db.Conn.Exec(context.Background(), `SELECT pg_notify($1, $2)`, ChannelForTenant(tenant), "forged-durable-row"); err != nil {
		t.Fatalf("forged notification: %v", err)
	}
	wake, err := listener.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if wake.Reason != ReasonHint || wake.TenantID != tenant || reads != 2 {
		t.Fatalf("wake=%+v reads=%d, want one read beyond startup", wake, reads)
	}
}

func TestTodo_DB_EDGE_001_Recovery(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wakeup-recovery")
	reader := db.NewConn(t)
	reads := 0
	scan := func(ctx context.Context, gotTenant uuid.UUID) error {
		var count int
		if err := reader.QueryRow(ctx, `SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND ready_state = 'READY'`, gotTenant).Scan(&count); err != nil {
			return err
		}
		reads++
		if reads > 1 && count != 1 {
			return fmt.Errorf("durable recovery count = %d, want 1", count)
		}
		return nil
	}
	listener, err := NewListener(context.Background(), ListenerConfig{
		URL: db.URL, RuntimeParams: map[string]string{"search_path": db.Schema},
		TenantIDs: []uuid.UUID{tenant}, RecoveryInterval: time.Second, Scan: scan,
	})
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	if err := listener.conn.Close(context.Background()); err != nil {
		t.Fatalf("drop listener connection: %v", err)
	}
	insertReadyWork(t, db, tenant, uuid.New())
	wake, err := listener.Wait(context.Background())
	if err != nil {
		t.Fatalf("reconnect wait: %v", err)
	}
	if wake.Reason != ReasonRecovery || reads != 2 {
		t.Fatalf("wake=%+v reads=%d, want reconnect catch-up", wake, reads)
	}
	if err := NewNotifier(db.Conn).Notify(context.Background(), tenant); err != nil {
		t.Fatalf("post-reconnect notify: %v", err)
	}
	wake, err = listener.Wait(context.Background())
	if err != nil || wake.Reason != ReasonHint || reads != 3 {
		t.Fatalf("post-reconnect wake=%+v err=%v reads=%d, want re-LISTEN hint", wake, err, reads)
	}
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func insertReadyWork(t *testing.T, db *pgtest.DB, tenant, readyWorkID uuid.UUID) {
	t.Helper()
	instanceID := uuid.New()
	now := time.Now().UTC().Add(-time.Minute)
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wakeup-test', 1, $3, 'SIMULATE', 'CREATED', 'input:wakeup', $4, $5)`,
		tenant, instanceID, repeatHex("a"), "corr:"+readyWorkID.String(), now)
	db.Exec(t, `
		INSERT INTO workflow_ready_work (
			tenant_id, ready_work_id, instance_id, node_id, attempt,
			ready_state, priority, eligible_at, enqueued_at)
		VALUES ($1, $2, $3, 'wakeup.node', 1, 'READY', 100, $4, $4)`,
		tenant, readyWorkID, instanceID, now)
}

func repeatHex(digit string) string {
	out := ""
	for range 64 {
		out += digit
	}
	return out
}

var _ dbport.Conn = (*pgxadapter.Conn)(nil)
