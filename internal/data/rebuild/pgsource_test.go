package rebuild_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rebuild"
)

// TestPGSourceSchemaRegistered proves the live source's schema check reads
// the tenant's own registrations: a registered schema reports true, an
// unregistered one false, and one registered for another tenant is still not
// registered for this tenant.
func TestPGSourceSchemaRegistered(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	other := insertTenant(t, f.db)
	f.registerSchema(t, tenant) // `other` registers nothing at all.

	src := rebuild.NewPGSource(ledger.NewReader())
	ctx := context.Background()

	got, err := src.SchemaRegistered(ctx, f.db.Conn, tenant, schemaRef)
	if err != nil {
		t.Fatalf("schema registered for the tenant: %v", err)
	}
	if !got {
		t.Fatal("a registered schema reported as unregistered")
	}

	got, err = src.SchemaRegistered(ctx, f.db.Conn, tenant, "hcmnext.unknown.v1.Nothing@1")
	if err != nil {
		t.Fatalf("schema not registered for the tenant: %v", err)
	}
	if got {
		t.Fatal("an unregistered schema reported as registered")
	}

	got, err = src.SchemaRegistered(ctx, f.db.Conn, other, schemaRef)
	if err != nil {
		t.Fatalf("schema on a tenant that registered nothing: %v", err)
	}
	if got {
		t.Fatal("one tenant's registration leaked into another tenant's view")
	}
}

// TestPGSourceCorrectionTarget proves the live source resolves a correction's
// target the way the rebuild needs: no reference at all, a reference to an
// assertion that is not on the ledger, and a reference the ledger confirms -
// including the corrected assertion's class.
func TestPGSourceCorrectionTarget(t *testing.T) {
	f := newFixture(t)
	tenant := insertTenant(t, f.db)
	stream := "rebuild:" + uuid.NewString()
	ensureStream(t, f.db, tenant, stream, "worker_state")
	f.registerSchema(t, tenant)

	f.appendEvent(t, tenant, stream, 0, ledger.TransactionFact, nil)
	f.appendEvent(t, tenant, stream, 1, ledger.Correction, &ledger.EventRef{StreamKey: stream, Sequence: 1})
	f.appendEvent(t, tenant, stream, 2, ledger.Correction, &ledger.EventRef{StreamKey: stream, Sequence: 7})

	src := rebuild.NewPGSource(ledger.NewReader())
	reader := ledger.NewReader()
	ctx := context.Background()

	t.Run("plain event carries no reference", func(t *testing.T) {
		ev, err := reader.ReadEvent(ctx, f.db.Conn, tenant, stream, 1)
		if err != nil {
			t.Fatalf("read event 1: %v", err)
		}
		ref, err := src.CorrectionTarget(ctx, f.db.Conn, ev)
		if err != nil {
			t.Fatalf("correction target: %v", err)
		}
		if ref.HasReference {
			t.Fatalf("a plain event reported a correction reference: %+v", ref)
		}
	})

	t.Run("reference to an assertion not on the ledger", func(t *testing.T) {
		ev, err := reader.ReadEvent(ctx, f.db.Conn, tenant, stream, 3)
		if err != nil {
			t.Fatalf("read event 3: %v", err)
		}
		ref, err := src.CorrectionTarget(ctx, f.db.Conn, ev)
		if err != nil {
			t.Fatalf("correction target: %v", err)
		}
		if !ref.HasReference || ref.Resolved {
			t.Fatalf("ref = %+v, want HasReference=true Resolved=false", ref)
		}
		if ref.Stream != stream || ref.Sequence != 7 {
			t.Fatalf("ref points at %s@%d, want %s@7", ref.Stream, ref.Sequence, stream)
		}
	})

	t.Run("reference the ledger confirms", func(t *testing.T) {
		ev, err := reader.ReadEvent(ctx, f.db.Conn, tenant, stream, 2)
		if err != nil {
			t.Fatalf("read event 2: %v", err)
		}
		ref, err := src.CorrectionTarget(ctx, f.db.Conn, ev)
		if err != nil {
			t.Fatalf("correction target: %v", err)
		}
		if !ref.HasReference || !ref.Resolved {
			t.Fatalf("ref = %+v, want HasReference=true Resolved=true", ref)
		}
		if ref.Stream != stream || ref.Sequence != 1 || ref.Class != string(ledger.TransactionFact) {
			t.Fatalf("ref = %+v, want %s@1 with class TRANSACTION_FACT", ref, stream)
		}
	})

	t.Run("event missing from the ledger is an error", func(t *testing.T) {
		ref, err := src.CorrectionTarget(ctx, f.db.Conn, ledger.EventRecord{
			Tenant: tenant, StreamKey: stream, Sequence: 99, EventID: uuid.New(),
		})
		if err == nil {
			t.Fatalf("absent event resolved to %+v, want an error", ref)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error = %q, want the absence named", err)
		}
	})
}
