package evidence_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
)

// TestExportStatesOnlyTheWindowAndDerivesEverythingElse proves the rule
// export.go opens with: a caller states a tenant, a window and a schema
// release, and nothing else. A caller that could also state which streams
// were covered or where their chains stood could hand an auditor a package
// describing a ledger that never existed.
func TestExportStatesOnlyTheWindowAndDerivesEverythingElse(t *testing.T) {
	f := newFixture(t)
	f.seedCoveredWindow(t)
	exporter := evidence.NewExporter()
	ctx := context.Background()

	t.Run("a request without a tenant is refused", func(t *testing.T) {
		_, err := exporter.Read(ctx, f.db.Conn, evidence.Request{From: windowFrom, To: windowTo, Schema: f.release})
		if _, ok := asContentInvalid(err); !ok {
			t.Fatalf("Read without a tenant = %v, want ErrContentInvalid", err)
		}
	})

	t.Run("an empty or inverted window is refused", func(t *testing.T) {
		for name, req := range map[string]evidence.Request{
			"empty":    {Tenant: f.tenant, From: windowTo, To: windowTo, Schema: f.release},
			"inverted": {Tenant: f.tenant, From: windowTo, To: windowFrom, Schema: f.release},
			"unset":    {Tenant: f.tenant, Schema: f.release},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := exporter.Read(ctx, f.db.Conn, req); err == nil {
					t.Fatalf("Read over a %s window produced content", name)
				}
			})
		}
	})

	t.Run("the window is half-open on both sides", func(t *testing.T) {
		// The first event is recorded exactly at windowFrom and is inside;
		// nothing recorded at windowTo would be.
		content, err := exporter.Read(ctx, f.db.Conn, f.request())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got := len(content.Streams[0].Events); got != 2 {
			t.Fatalf("stream one covers %d events, want 2", got)
		}
		if !content.Streams[0].Events[0].RecordedAt.Equal(windowFrom) {
			t.Fatalf("the first covered event is at %s, want the window start %s",
				content.Streams[0].Events[0].RecordedAt, windowFrom)
		}

		// A window that ends before the second event drops it and takes the
		// head back with it.
		narrow := f.request()
		narrow.To = recordedSecond
		narrowed, err := exporter.Read(ctx, f.db.Conn, narrow)
		if err != nil {
			t.Fatalf("read narrowed window: %v", err)
		}
		if got := len(narrowed.Streams[0].Events); got != 1 {
			t.Fatalf("the narrowed window covers %d events on stream one, want 1", got)
		}
		if got := narrowed.Streams[0].Head.Sequence; got != 1 {
			t.Fatalf("the narrowed head is at sequence %d, want 1", got)
		}
		if got := len(narrowed.Streams[0].Links); got != 1 {
			t.Fatalf("the narrowed chain carries %d links, want 1", got)
		}
	})

	t.Run("the head is taken at the window's end, not now", func(t *testing.T) {
		// The stream moves on and is checkpointed again; the same request
		// still describes the same ledger it described before.
		before := f.mustExport(t, f.request())
		f.appendLinked(t, f.tenant, streamOne, 2, recordedThird)
		f.mustCheckpoint(t, checkpointTwo)

		after := f.mustExport(t, f.request())
		if before.Manifest.Digest != after.Manifest.Digest {
			t.Fatalf("the package changed after the stream moved on: %s then %s",
				before.Manifest.Digest, after.Manifest.Digest)
		}
		content, err := exporter.Read(ctx, f.db.Conn, f.request())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got := content.Streams[0].Head.Sequence; got != 2 {
			t.Fatalf("the head is at sequence %d after a later append, want 2", got)
		}
		if got := len(content.Epochs); got != 1 {
			t.Fatalf("%d epochs intersect the window, want 1; a later epoch's window only touches the boundary", got)
		}
	})

	t.Run("the whole window's epochs are carried once it spans both", func(t *testing.T) {
		wide := f.request()
		wide.To = checkpointTwo
		content, err := exporter.Read(ctx, f.db.Conn, wide)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got := len(content.Epochs); got != 2 {
			t.Fatalf("%d epochs intersect the wider window, want 2", got)
		}
		if content.Epochs[0].EpochNumber != 1 || content.Epochs[1].EpochNumber != 2 {
			t.Fatalf("epochs are not in number order: %d, %d",
				content.Epochs[0].EpochNumber, content.Epochs[1].EpochNumber)
		}
	})
}

// TestExportIsReadOnlyAndRefusesRatherThanDegrades proves the two properties
// the REFACTOR line asks for: an export writes nothing, so it can be taken
// against a replica and never competes with an appender for a lock; and it
// produces no package at all rather than a package with a caveat.
func TestExportIsReadOnlyAndRefusesRatherThanDegrades(t *testing.T) {
	f := newFixture(t)
	f.seedCoveredWindow(t)
	ctx := context.Background()

	t.Run("an export changes no row", func(t *testing.T) {
		before := rowCounts(t, f)
		if _, err := evidence.NewExporter().Export(ctx, f.db.Conn, f.request()); err != nil {
			t.Fatalf("export: %v", err)
		}
		if after := rowCounts(t, f); after != before {
			t.Fatalf("an export changed the ledger: %+v then %+v", before, after)
		}
	})

	t.Run("a tenant with nothing in the window produces no package", func(t *testing.T) {
		empty := f.withTenant(t)
		_, err := evidence.NewExporter().Export(ctx, f.db.Conn, evidence.Request{
			Tenant: empty.tenant, From: windowFrom, To: windowTo, Schema: f.release,
		})
		if _, ok := asContentInvalid(err); !ok {
			t.Fatalf("export over an empty window = %v, want ErrContentInvalid", err)
		}
	})

	t.Run("an unchained head produces no package", func(t *testing.T) {
		other := f.withTenant(t)
		other.appendLinked(t, other.tenant, streamOne, 0, recordedFirst)
		other.mustCheckpoint(t, checkpointOne)
		// The unlinked event lands inside a window a signed epoch already
		// covers, so what refuses the export is the chain and nothing else.
		other.appendUnlinked(t, other.tenant, streamOne, 1, recordedSecond)

		_, err := evidence.NewExporter().Export(ctx, f.db.Conn, evidence.Request{
			Tenant: other.tenant, From: windowFrom, To: windowTo, Schema: f.release,
		})
		invalid, ok := asContentInvalid(err)
		if !ok {
			t.Fatalf("export over an unchained head = %v, want ErrContentInvalid", err)
		}
		if len(invalid.Missing) == 0 {
			t.Fatal("the refusal named no problem")
		}
	})

	t.Run("a tenant that does not exist produces no package", func(t *testing.T) {
		_, err := evidence.NewExporter().Export(ctx, f.db.Conn, evidence.Request{
			Tenant: uuid.New(), From: windowFrom, To: windowTo, Schema: f.release,
		})
		if err == nil {
			t.Fatal("an unknown tenant produced a package")
		}
	})
}

// TestExportCarriesChainPrefixAsDigestsNotPayloads proves the disclosure
// rule the package doc states: a chain that starts in the middle proves
// nothing, so the prefix before the window is carried - but as digests and
// links only, never as payloads.
func TestExportCarriesChainPrefixAsDigestsNotPayloads(t *testing.T) {
	f := newFixture(t)
	f.appendLinked(t, f.tenant, streamOne, 0, recordedFirst)
	f.appendLinked(t, f.tenant, streamOne, 1, recordedSecond)
	f.appendLinked(t, f.tenant, streamTwo, 0, recordedFirst)
	f.mustCheckpoint(t, checkpointOne)
	// A later window whose events sit above a prefix recorded before it.
	f.appendLinked(t, f.tenant, streamOne, 2, recordedThird)
	f.mustCheckpoint(t, checkpointTwo)

	req := evidence.Request{Tenant: f.tenant, From: windowTo, To: checkpointTwo, Schema: f.release}
	content, err := evidence.NewExporter().Read(context.Background(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("read later window: %v", err)
	}

	var one evidence.Stream
	for _, s := range content.Streams {
		if s.StreamKey == streamOne {
			one = s
		}
	}
	if one.StreamKey == "" {
		t.Fatal("the later window does not cover stream one")
	}
	if len(one.Events) != 1 || one.Events[0].Sequence != 3 {
		t.Fatalf("the later window covers %d events, want only sequence 3", len(one.Events))
	}
	if len(one.Links) != 3 || len(one.Digests) != 3 {
		t.Fatalf("the chain carries %d links and %d digests, want 3 of each from genesis",
			len(one.Links), len(one.Digests))
	}
	// The prefix is present as proof and absent as content.
	for _, link := range one.Links {
		if !link.RecordedAt.IsZero() {
			t.Errorf("link %d carries the writer's clock, which is not what it proves", link.Sequence)
		}
	}

	pkg, err := evidence.NewExporter().Export(context.Background(), f.db.Conn, req)
	if err != nil {
		t.Fatalf("export later window: %v", err)
	}
	chain, ok := pkg.Part(evidence.StreamChainPath(0))
	if !ok {
		t.Fatal("the package has no chain part for stream one")
	}
	// The prefix events' payloads are "worker:1-1" and "worker:1-2"; only
	// their digests may appear in a chain part.
	for _, payload := range []string{"worker:1-1", "worker:1-2"} {
		if bytes.Contains(chain, []byte(payload)) {
			t.Errorf("the chain part discloses the prefix payload %q", payload)
		}
	}
	mustVerify(t, pkg.Files(), f.liveKeyDirectory())
}

type ledgerCounts struct{ events, links, epochs int }

func rowCounts(t *testing.T, f fixture) ledgerCounts {
	t.Helper()
	var out ledgerCounts
	ctx := context.Background()
	for _, q := range []struct {
		sql  string
		into *int
	}{
		{"SELECT count(*) FROM ledger_event", &out.events},
		{"SELECT count(*) FROM ledger_hash_chain_link", &out.links},
		{"SELECT count(*) FROM ledger_checkpoint_epoch", &out.epochs},
	} {
		if err := f.db.QueryRow(ctx, q.sql).Scan(q.into); err != nil {
			t.Fatalf("count rows (%s): %v", q.sql, err)
		}
	}
	return out
}
