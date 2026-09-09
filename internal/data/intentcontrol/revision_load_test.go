package intentcontrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// TestTodo_CONFLICT_003_RevisionLoad proves that the parent-row read is bound
// to all three identity coordinates and returns the immutable payload exactly.
func TestTodo_CONFLICT_003_RevisionLoad(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant := insertTenant(t, db, "revision-load")
	other := insertTenant(t, db, "revision-load-other")
	id := insertIntent(t, db, tenant, "revision-load")
	insertRevision(t, db, tenant, id, 3)

	t.Run("exact row survives fresh app connection", func(t *testing.T) {
		conn := appConn(t, db)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			t.Fatal(err)
		}
		got, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenant, id, 3)
		if err != nil {
			t.Fatal(err)
		}
		if got.TenantID != tenant || got.IntentID != id || got.Revision != 3 {
			t.Fatalf("identity = %s/%s/%d", got.TenantID, got.IntentID, got.Revision)
		}
		var payload map[string]any
		if err := json.Unmarshal(got.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["proposal"] != true {
			t.Fatalf("payload = %q", got.Payload)
		}
	})

	t.Run("wrong revision and tenant are not found", func(t *testing.T) {
		conn := appConn(t, db)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenant, id, 4); !errors.Is(err, intentcontrol.ErrNotFound) {
			t.Fatalf("wrong revision error = %v", err)
		}
		if err := tenancy.WithTenant(ctx, tx, other); err != nil {
			t.Fatal(err)
		}
		if _, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenant, id, 3); !errors.Is(err, intentcontrol.ErrNotFound) {
			t.Fatalf("wrong tenant error = %v", err)
		}
		if _, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, other, id, 3); !errors.Is(err, intentcontrol.ErrNotFound) {
			t.Fatalf("cross-tenant identity error = %v", err)
		}
		if _, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenant, uuid.New(), 3); !errors.Is(err, intentcontrol.ErrNotFound) {
			t.Fatalf("wrong intent error = %v", err)
		}
	})
}

func TestRevisionLoadRejectsInvalidCoordinatesBeforeQuery(t *testing.T) {
	store := intentcontrol.RevisionStore{}
	for name, tc := range map[string]struct {
		tenant, intentID uuid.UUID
		revision         uint64
	}{
		"nil tenant":      {uuid.Nil, uuid.New(), 1},
		"nil intent":      {uuid.New(), uuid.Nil, 1},
		"zero revision":   {uuid.New(), uuid.New(), 0},
		"bigint overflow": {uuid.New(), uuid.New(), uint64(1) << 63},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Load(context.Background(), nil, tc.tenant, tc.intentID, tc.revision); !errors.Is(err, intentcontrol.ErrInvalidRow) {
				t.Fatalf("Load invalid coordinates = %v, want ErrInvalidRow", err)
			}
		})
	}
}
