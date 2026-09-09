package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func scopeA() Scope {
	return Scope{TenantID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), CellID: "cell-a", EncryptionClass: EncryptionPlatformManaged}
}
func scopeB() Scope {
	return Scope{TenantID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), CellID: "cell-a", EncryptionClass: EncryptionPlatformManaged}
}
func scopeACellB() Scope {
	return Scope{TenantID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), CellID: "cell-b", EncryptionClass: EncryptionPlatformManaged}
}

func TestTodo_STORE_002(t *testing.T) {
	db := pgtest.New(t)
	ad := NewAdapter(db.Conn, "store_kv_002")
	ctx := t.Context()
	s := scopeA()
	if err := ad.Put(ctx, s, "cell-a:k1", "v1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v, err := ad.Get(ctx, s, "cell-a:k1")
	if err != nil || v != "v1" {
		t.Fatalf("get %v %v", v, err)
	}

	t.Run("missing context crosses", func(t *testing.T) {
		bad := Scope{TenantID: uuid.Nil, CellID: "cell-a", EncryptionClass: EncryptionPlatformManaged}
		if err := ad.Put(ctx, bad, "cell-a:k2", "x"); err == nil {
			t.Fatal("nil tenant must be rejected")
		}
		if _, err := ad.Get(ctx, bad, "cell-a:k1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing tenant must be not found oracle, got %v", err)
		}
		bad2 := Scope{TenantID: s.TenantID, CellID: "", EncryptionClass: EncryptionPlatformManaged}
		if err := ad.Put(ctx, bad2, "cell-a:k2", "x"); err == nil {
			t.Fatal("empty cell must be rejected")
		}
	})

	t.Run("forged key prefix", func(t *testing.T) {
		if err := ad.Put(ctx, s, "cell-b:evil", "x"); err == nil {
			t.Fatal("forged cell prefix must be rejected")
		}
		if _, err := ad.Get(ctx, s, "cell-b:evil"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("forged get must be not found, got %v", err)
		}
	})

	t.Run("tenant isolation", func(t *testing.T) {
		sb := scopeB()
		if _, err := ad.Get(ctx, sb, "cell-a:k1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross tenant must be not found, got %v", err)
		}
		if err := ad.Put(ctx, sb, "cell-a:k1", "other"); err != nil {
			t.Fatalf("other tenant put: %v", err)
		}
		v2, err := ad.Get(ctx, s, "cell-a:k1")
		if err != nil || v2 != "v1" {
			t.Fatalf("original tenant corrupted %v %v", v2, err)
		}
	})

	t.Run("cell isolation", func(t *testing.T) {
		scb := scopeACellB()
		if _, err := ad.Get(ctx, scb, "cell-a:k1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross cell must be not found, got %v", err)
		}
	})

	t.Run("encryption boundary", func(t *testing.T) {
		badEnc := Scope{TenantID: s.TenantID, CellID: s.CellID, EncryptionClass: "INVALID"}
		if err := ad.Put(ctx, badEnc, "cell-a:k3", "x"); err == nil {
			t.Fatal("invalid encryption must be rejected")
		}
		if _, err := ad.Get(ctx, badEnc, "cell-a:k1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("encryption get must be not found, got %v", err)
		}
	})
}

func TestTodo_STORE_002_Integration(t *testing.T) {
	db := pgtest.New(t)
	ad := NewAdapter(db.Conn, "store_kv_002i")
	ctx := t.Context()
	s := scopeA()
	if err := ad.Put(ctx, s, "cell-a:int1", "hello"); err != nil {
		t.Fatalf("put: %v", err)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var v string
	if err := tx.QueryRow(ctx, `select v from store_kv_002i where tenant_id=$1 and cell_id=$2 and k=$3`, s.TenantID.String(), s.CellID, "cell-a:int1").Scan(&v); err != nil {
		t.Fatalf("direct read: %v", err)
	}
	if v != "hello" {
		t.Fatalf("value %q", v)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestTodo_STORE_002_Fault(t *testing.T) {
	db := pgtest.New(t)
	ad := NewAdapter(db.Conn, "store_kv_002f")
	ctx := t.Context()
	s := scopeA()
	if err := ad.Put(ctx, s, "cell-a:f1", "val"); err != nil {
		t.Fatalf("put: %v", err)
	}
	rows, err := ad.List(ctx, s, "cell-a:")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("list empty")
	}
	if _, err := ad.Get(ctx, s, "cell-a:missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing must be not found, got %v", err)
	}
	if err := ad.Delete(ctx, s, "cell-a:f1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ad.Get(ctx, s, "cell-a:f1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete must be not found, got %v", err)
	}
}

func TestTodo_STORE_002_Security(t *testing.T) {
	db := pgtest.New(t)
	ad := NewAdapter(db.Conn, "store_kv_002s")
	ctx := t.Context()
	s := scopeA()
	_ = ad.Put(ctx, s, "cell-a:sec1", "secret")
	forged := Scope{TenantID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), CellID: "cell-a", EncryptionClass: EncryptionPlatformManaged}
	if _, err := ad.Get(ctx, forged, "cell-a:sec1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forged tenant must be not found, got %v", err)
	}
	if _, err := ad.Get(ctx, s, "cell-a:sec1"); err != nil {
		t.Fatalf("owner must succeed, got %v", err)
	}
	maint := Scope{TenantID: uuid.Nil, CellID: "cell-a", EncryptionClass: EncryptionPlatformManaged, IsMaintenance: true}
	if err := ad.Put(ctx, maint, "cell-a:maint", "x"); err == nil {
		t.Fatal("maintenance without tenant must be rejected")
	}
	emptyTenant := Scope{TenantID: uuid.Nil, CellID: "cell-a", EncryptionClass: EncryptionPlatformManaged}
	if _, err := ad.List(ctx, emptyTenant, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty tenant list must be not found, got %v", err)
	}
	if _, err := ad.List(ctx, s, "cell-b:"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forged list prefix must be not found, got %v", err)
	}
	otherCell := scopeACellB()
	if _, err := ad.List(ctx, otherCell, "cell-a:"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-cell list must be not found, got %v", err)
	}
}
