package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

type fakeRows struct{}

func (f *fakeRows) Next() bool        { return false }
func (f *fakeRows) Scan(...any) error { return nil }
func (f *fakeRows) Err() error        { return nil }
func (f *fakeRows) Close()            {}

type fakeDB struct{}

func (f *fakeDB) Exec(_ context.Context, _ string, _ ...any) (int64, error) { return 0, nil }
func (f *fakeDB) Query(_ context.Context, _ string, _ ...any) (dbport.Rows, error) {
	return &fakeRows{}, nil
}
func (f *fakeDB) QueryRow(_ context.Context, _ string, _ ...any) dbport.Row {
	return rowErr{err: dbport.ErrNoRows}
}
func (f *fakeDB) Begin(_ context.Context) (dbport.Tx, error) { return &fakeTx{}, nil }

type rowErr struct{ err error }

func (r rowErr) Scan(...any) error { return r.err }

type fakeTx struct{ fakeDB }

func (f *fakeTx) Commit(_ context.Context) error   { return nil }
func (f *fakeTx) Rollback(_ context.Context) error { return nil }

func TestTenantIDDeterministic(t *testing.T) {
	a := TenantID("acme-corp")
	b := TenantID("acme-corp")
	if a != b {
		t.Fatal("not deterministic")
	}
	c := TenantID("other-tenant")
	if a == c {
		t.Fatal("different tenant same id")
	}
	if a.Version() != 5 {
		t.Fatalf("version %d", a.Version())
	}
}

func TestStreamKey(t *testing.T) {
	if got := StreamKey("abc-123"); got != "intent:abc-123" {
		t.Fatalf("got %q", got)
	}
}

func TestCorrelationUUID(t *testing.T) {
	empty1 := correlationUUID("")
	empty2 := correlationUUID("")
	if empty1 == empty2 {
		t.Fatal("empty should generate random distinct ids")
	}
	valid := uuid.New().String()
	parsed := correlationUUID(valid)
	if parsed.String() != valid {
		t.Fatalf("got %s want %s", parsed, valid)
	}
	derived1 := correlationUUID("my-correlation")
	derived2 := correlationUUID("my-correlation")
	if derived1 != derived2 {
		t.Fatal("derived not deterministic")
	}
	derived3 := correlationUUID("other")
	if derived1 == derived3 {
		t.Fatal("different correlation same uuid")
	}
}

func TestNewWithOptions(t *testing.T) {
	db := &fakeDB{}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil store")
	}
	if s.cellID != "cell-local" {
		t.Fatalf("cell %q", s.cellID)
	}
	s2, err := New(db, WithCellID("cell-99"))
	if err != nil {
		t.Fatal(err)
	}
	if s2.cellID != "cell-99" {
		t.Fatalf("cell %q", s2.cellID)
	}
}

func TestWithClock(t *testing.T) {
	db := &fakeDB{}
	fixed := time.Now()
	s, err := New(db, WithClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatal(err)
	}
	if s.now == nil {
		t.Fatal("clock not set")
	}
	if !s.now().Equal(fixed) {
		t.Fatal("clock mismatch")
	}
}

func TestBootstrapEmptyTenant(t *testing.T) {
	s, err := New(&fakeDB{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty tenant")
	}
}

func TestConstants(t *testing.T) {
	if ProjectionName != "intent_instance" {
		t.Fatalf("projection %q", ProjectionName)
	}
	if StreamKind != "TRANSACTION" {
		t.Fatalf("kind %q", StreamKind)
	}
}

func TestActiveTenants(t *testing.T) {
	ids, err := ActiveTenants(context.Background(), &fakeDB{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("got %d", len(ids))
	}
}
