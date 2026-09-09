package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestBootstrapReadHelpers_RejectNilAndReportEmpty(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	if _, err := tenancy.LatestBootstrap(ctx, db.Conn, uuid.Nil); err == nil {
		t.Fatal("LatestBootstrap accepted nil tenant")
	}
	if _, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, uuid.Nil); err == nil {
		t.Fatal("ListBootstrapReceipts accepted nil tenant")
	}
	tenantID := insertTenant(t, db, "bootstrap-empty")
	latest, err := tenancy.LatestBootstrap(ctx, db.Conn, tenantID)
	if err != nil || latest != nil {
		t.Fatalf("LatestBootstrap empty = %+v, %v; want nil, nil", latest, err)
	}
	receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
	if err != nil || len(receipts) != 0 {
		t.Fatalf("ListBootstrapReceipts empty = %+v, %v", receipts, err)
	}
}

func TestBootstrap_RejectsNilTenantBeforeDatabaseWork(t *testing.T) {
	db := pgtest.New(t)
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tenancy.Bootstrap(context.Background(), tx, uuid.Nil, bootstrapManifest("nil", 1))
	if err == nil {
		t.Fatal("Bootstrap accepted nil tenant")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("Bootstrap returned unrelated cancellation error: %v", err)
	}
}
