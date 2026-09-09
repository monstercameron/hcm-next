package provenance_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/provenance"
)

// TestTodo_DATA_014 proves provenance publishing: a record can never be
// published without at least one evidence id, publishing the same source
// twice is a no-op rather than a second edge, every publish enqueues one
// "provenance.published" outbox message, and Lineage reports COMPLETE,
// PARTIAL or UNKNOWN honestly rather than assuming completeness.
func TestTodo_DATA_014(t *testing.T) {
	t.Parallel()

	t.Run("provenance is never published without its evidence id", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		receipt := f.appendEvent(t, 0)
		req := f.publishRequest(receipt)
		req.EvidenceIDs = nil

		_, err := f.publishErr(t, f.db.Conn, req)
		var missing provenance.ErrMissingEvidence
		if !errors.As(err, &missing) {
			t.Fatalf("publish with no evidence ids = %v, want ErrMissingEvidence", err)
		}

		var count int
		if err := f.db.Conn.QueryRow(context.Background(),
			`SELECT count(*) FROM provenance_record WHERE tenant_id = $1`, f.tenant).Scan(&count); err != nil {
			t.Fatalf("count provenance_record: %v", err)
		}
		if count != 0 {
			t.Fatalf("provenance_record has %d rows after a refused publish, want 0", count)
		}
	})

	t.Run("provenance is never published without a digest", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		receipt := f.appendEvent(t, 0)
		req := f.publishRequest(receipt)
		req.Digests = nil

		_, err := f.publishErr(t, f.db.Conn, req)
		var missing provenance.ErrMissingDigest
		if !errors.As(err, &missing) {
			t.Fatalf("publish with no digest = %v, want ErrMissingDigest", err)
		}
	})

	t.Run("publish records an edge and enqueues provenance.published", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		receipt := f.appendEvent(t, 0)
		req := f.publishRequest(receipt)

		rec := f.publish(t, req)
		if !rec.Published {
			t.Fatalf("first publish reported Published=false")
		}
		if rec.IntentRef != f.intentRef || rec.SourceKind != provenance.SourceLedgerEvent {
			t.Fatalf("record = %+v, want intent %q kind LEDGER_EVENT", rec, f.intentRef)
		}

		var status string
		var effectIdentity string
		err := f.db.Conn.QueryRow(context.Background(),
			`SELECT status, effect_identity FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`,
			f.tenant, provenance.EffectIdentityPrefix+rec.RecordID.String()).Scan(&status, &effectIdentity)
		if err != nil {
			t.Fatalf("read outbox row: %v", err)
		}
		if status != "PENDING" {
			t.Fatalf("outbox status = %q, want PENDING", status)
		}
	})

	t.Run("duplicate publish is a no-op", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		receipt := f.appendEvent(t, 0)
		req := f.publishRequest(receipt)

		first := f.publish(t, req)
		second := f.publish(t, req)
		if !first.Published {
			t.Fatalf("first publish reported Published=false")
		}
		if second.Published {
			t.Fatalf("second publish for the same source reported Published=true, want a no-op")
		}
		if second.RecordID != first.RecordID {
			t.Fatalf("duplicate publish minted a different record id: %s vs %s", second.RecordID, first.RecordID)
		}

		var recordCount, outboxCount int
		if err := f.db.Conn.QueryRow(context.Background(),
			`SELECT count(*) FROM provenance_record WHERE tenant_id = $1 AND source_ref = $2`,
			f.tenant, req.SourceRef).Scan(&recordCount); err != nil {
			t.Fatalf("count provenance_record: %v", err)
		}
		if err := f.db.Conn.QueryRow(context.Background(),
			`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`,
			f.tenant, provenance.EffectIdentityPrefix+first.RecordID.String()).Scan(&outboxCount); err != nil {
			t.Fatalf("count outbox: %v", err)
		}
		if recordCount != 1 || outboxCount != 1 {
			t.Fatalf("duplicate publish left %d provenance_record rows and %d outbox rows, want exactly 1 each", recordCount, outboxCount)
		}
	})

	t.Run("Lineage reports UNKNOWN for a stream with no events", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		result, err := provenance.Lineage(context.Background(), f.db.Conn, f.reader, f.tenant, f.streamKey, f.intentRef)
		if err != nil {
			t.Fatalf("lineage: %v", err)
		}
		if result.Status != provenance.StatusUnknown || result.LedgerEventCount != 0 {
			t.Fatalf("lineage on an empty stream = %+v, want Status=UNKNOWN LedgerEventCount=0", result)
		}
	})

	t.Run("Lineage reports PARTIAL then COMPLETE as publishing catches up", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		r1 := f.appendEvent(t, 0)
		r2 := f.appendEvent(t, r1.Sequence)

		f.publish(t, f.publishRequest(r1))

		partial, err := provenance.Lineage(context.Background(), f.db.Conn, f.reader, f.tenant, f.streamKey, f.intentRef)
		if err != nil {
			t.Fatalf("lineage: %v", err)
		}
		if partial.Status != provenance.StatusPartial || partial.LedgerEventCount != 2 || partial.PublishedLedgerEventCount != 1 {
			t.Fatalf("lineage after publishing one of two events = %+v, want PARTIAL 1/2", partial)
		}

		f.publish(t, f.publishRequest(r2))

		complete, err := provenance.Lineage(context.Background(), f.db.Conn, f.reader, f.tenant, f.streamKey, f.intentRef)
		if err != nil {
			t.Fatalf("lineage: %v", err)
		}
		if complete.Status != provenance.StatusComplete || complete.PublishedLedgerEventCount != 2 {
			t.Fatalf("lineage after publishing both events = %+v, want COMPLETE 2/2", complete)
		}
		if len(complete.Edges) != 2 {
			t.Fatalf("lineage returned %d edges, want 2", len(complete.Edges))
		}
	})
}

// TestTodo_DATA_014_Golden fixes one exact publish call and checks the
// returned record and the stored row's evidence/digest columns against a
// literal expected value.
func TestTodo_DATA_014_Golden(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	receipt := f.appendEvent(t, 0)
	req := f.publishRequest(receipt)
	req.ConnectorRef = "connector:incumbent-hris"
	req.EvidenceIDs = []string{"evidence:a", "evidence:b"}

	rec := f.publish(t, req)
	if rec.SourceAuthority != "hcmnext:intent" || rec.PrincipalRef != "principal:worker-1" || rec.ConnectorRef != "connector:incumbent-hris" {
		t.Fatalf("record = %+v, want authority/principal/connector from the request", rec)
	}
	if len(rec.EvidenceIDs) != 2 || rec.EvidenceIDs[0] != "evidence:a" || rec.EvidenceIDs[1] != "evidence:b" {
		t.Fatalf("record evidence ids = %v, want [evidence:a evidence:b]", rec.EvidenceIDs)
	}
	if len(rec.Digests) != 1 || rec.Digests[0].Kind != "LEDGER_EVENT" || rec.Digests[0].Digest != receipt.Digest {
		t.Fatalf("record digests = %+v, want one LEDGER_EVENT digest %q", rec.Digests, receipt.Digest)
	}

	var evidenceIDs []string
	var connectorRef string
	err := f.db.Conn.QueryRow(context.Background(),
		`SELECT evidence_ids, connector_ref FROM provenance_record WHERE tenant_id = $1 AND record_id = $2`,
		f.tenant, rec.RecordID).Scan(&evidenceIDs, &connectorRef)
	if err != nil {
		t.Fatalf("read stored row: %v", err)
	}
	if len(evidenceIDs) != 2 || connectorRef != "connector:incumbent-hris" {
		t.Fatalf("stored row = (%v, %q), want ([evidence:a evidence:b], connector:incumbent-hris)", evidenceIDs, connectorRef)
	}
}

// TestTodo_DATA_014_Race proves that concurrent publishers of the same
// source produce exactly one edge: one wins Published=true, every other
// caller gets the same record back with Published=false, and no error is
// returned to anyone.
func TestTodo_DATA_014_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	receipt := f.appendEvent(t, 0)
	req := f.publishRequest(receipt)

	const racers = 6
	conns := make([]*pgxadapter.Conn, racers)
	for i := range conns {
		conns[i] = f.db.NewConn(t)
	}

	type outcome struct {
		rec provenance.Record
		err error
	}
	results := make([]outcome, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec, err := f.publishErr(t, conns[i], req)
			results[i] = outcome{rec: rec, err: err}
		}()
	}
	close(start)
	wg.Wait()

	winners := 0
	var recordID uuid.UUID
	for i, out := range results {
		if out.err != nil {
			t.Fatalf("racer %d returned an error: %v", i, out.err)
		}
		if out.rec.Published {
			winners++
		}
		if recordID == uuid.Nil {
			recordID = out.rec.RecordID
		} else if out.rec.RecordID != recordID {
			t.Fatalf("racer %d recorded a different record id: %s vs %s", i, out.rec.RecordID, recordID)
		}
	}
	if winners != 1 {
		t.Fatalf("%d racers reported Published=true for the same source, want exactly 1", winners)
	}

	var recordCount, outboxCount int
	if err := f.db.Conn.QueryRow(context.Background(),
		`SELECT count(*) FROM provenance_record WHERE tenant_id = $1 AND source_ref = $2`,
		f.tenant, req.SourceRef).Scan(&recordCount); err != nil {
		t.Fatalf("count provenance_record: %v", err)
	}
	if err := f.db.Conn.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`,
		f.tenant, provenance.EffectIdentityPrefix+recordID.String()).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if recordCount != 1 || outboxCount != 1 {
		t.Fatalf("race left %d provenance_record rows and %d outbox rows, want exactly 1 each", recordCount, outboxCount)
	}
}
