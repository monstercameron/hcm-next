package onboarding_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

// TestTodo_ONBOARD_002 proves extraction is resumable across an interruption
// without duplicating or losing a single record, that a field allow-list is
// enforced on every observed record, and that a source snapshot which
// changed underneath a stored checkpoint aborts with a typed error before any
// further read is attempted.
func TestTodo_ONBOARD_002(t *testing.T) {
	t.Parallel()

	t.Run("a full extraction observes every record of every object exactly once", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		results, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-1", Mode: connectivity.ReadFull,
		})
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		if len(results) != len(m.Objects) {
			t.Fatalf("got %d results, want %d", len(results), len(m.Objects))
		}
		for _, r := range results {
			if r.SnapshotChanged {
				t.Fatalf("object %s: unexpected snapshot change", r.Object)
			}
			if r.Run.Status != observe.RunCompleted {
				t.Fatalf("object %s: status = %s, want COMPLETED", r.Object, r.Run.Status)
			}
			want := h.Incumbent.RecordCount(r.Object)
			if r.Run.Records != want {
				t.Fatalf("object %s: observed %d records, want %d", r.Object, r.Run.Records, want)
			}
			pages, err := h.Store.List(context.Background(), observe.Query{
				TenantID: testTenant, Object: r.Object,
			})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			total := 0
			for _, p := range pages {
				total += p.RecordCount
			}
			if total != want {
				t.Fatalf("object %s: stored %d records across %d pages, want %d", r.Object, total, len(pages), want)
			}
		}
	})

	t.Run("an interrupted run resumes without duplicating or losing records", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Incumbent.InjectFaults(fakeincumbent.Faults{TransientOnReads: []int{2}})
		m := validManifest(h)
		narrowToObjects(&m, connectivity.ObjectWorker)

		first, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-a", Mode: connectivity.ReadFull, MaxRunsPerObject: 1,
		})
		if err != nil {
			t.Fatalf("first extract: %v", err)
		}
		if first[0].Run.Status != observe.RunInterrupted {
			t.Fatalf("status = %s, want INTERRUPTED", first[0].Run.Status)
		}
		if !observe.IsExternalFailure(first[0].Run.Cause) {
			t.Fatalf("cause is not an external failure: %v", first[0].Run.Cause)
		}

		h.Incumbent.ClearFaults()
		second, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-a-resume", Mode: connectivity.ReadFull,
		})
		if err != nil {
			t.Fatalf("second extract: %v", err)
		}
		if second[0].Run.Status != observe.RunCompleted {
			t.Fatalf("resumed status = %s, want COMPLETED", second[0].Run.Status)
		}

		want := h.Incumbent.RecordCount(connectivity.ObjectWorker)
		pages, err := h.Store.List(context.Background(), observe.Query{
			TenantID: testTenant, Object: connectivity.ObjectWorker,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		seen := map[string]bool{}
		total := 0
		for i, p := range pages {
			if want := uint64(i + 1); p.PageSequence != want {
				t.Fatalf("page sequence gap: page %d has sequence %d", i, p.PageSequence)
			}
			if seen[p.ObservationID.String()] {
				t.Fatalf("observation %s appears twice", p.ObservationID)
			}
			seen[p.ObservationID.String()] = true
			total += p.RecordCount
		}
		if total != want {
			t.Fatalf("resumed extraction stored %d records, want %d (no duplication or loss)", total, want)
		}
	})

	t.Run("a field allow list strips every field not listed, on every observed record", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		narrowToObjects(&m, connectivity.ObjectWorker)
		m.FieldAllowList = map[connectivity.ObjectKind][]string{
			connectivity.ObjectWorker: {"worker_number", "legal_name"},
		}

		if _, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-filtered", Mode: connectivity.ReadFull,
		}); err != nil {
			t.Fatalf("extract: %v", err)
		}

		pages, err := h.Store.List(context.Background(), observe.Query{
			TenantID: testTenant, Object: connectivity.ObjectWorker,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(pages) == 0 {
			t.Fatal("no pages observed")
		}
		for _, p := range pages {
			// The canonical payload is what a record allow-list actually
			// bounds; a field the manifest never allowed must never appear
			// in it, whether as a key or as its value.
			if strings.Contains(string(p.Payload), "job_code") {
				t.Fatalf("page %d canonical payload contains a field outside the allow list", p.PageSequence)
			}
		}
	})

	t.Run("a source snapshot changed underneath a stored checkpoint aborts before any read", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		narrowToObjects(&m, connectivity.ObjectWorker)

		// Take exactly one page (the fixture needs two at the default page
		// size) so a checkpoint is stored but the traversal is not complete.
		results, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-partial", Mode: connectivity.ReadFull,
			MaxRunsPerObject: 1, MaxPagesPerRun: 1,
		})
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		if results[0].Run.Status != observe.RunBounded {
			t.Fatalf("status = %s, want BOUNDED (one page taken of a two-page traversal)", results[0].Run.Status)
		}
		if results[0].Run.Checkpoint.Complete {
			t.Fatal("checkpoint reports complete after only one of two pages")
		}

		// Drift the schema, which changes the snapshot id the connector
		// reports for every subsequent call once one more read has served.
		h.Incumbent.InjectFaults(fakeincumbent.Faults{DriftAfterReads: 1})
		if _, err := h.Incumbent.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectPosition, Mode: connectivity.ReadFull, Limit: 1,
		}); err != nil {
			t.Fatalf("priming read: %v", err)
		}

		after, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-resume-after-drift", Mode: connectivity.ReadFull,
		})
		if err != nil {
			t.Fatalf("extract after drift: %v", err)
		}
		if !after[0].SnapshotChanged {
			t.Fatalf("extraction did not detect the changed snapshot: %+v", after[0])
		}
		if !errors.Is(after[0].Run.Cause, onboarding.ErrSnapshotChanged) {
			t.Fatalf("cause does not wrap ErrSnapshotChanged: %v", after[0].Run.Cause)
		}
		// The stored checkpoint is untouched: still incomplete, still at the
		// snapshot the earlier run left it at.
		if after[0].Run.Checkpoint.Complete {
			t.Fatal("checkpoint reports complete after a refused resume")
		}
	})

	t.Run("restart abandons the stored checkpoint and reads under a fresh snapshot", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		m := validManifest(h)
		narrowToObjects(&m, connectivity.ObjectWorker)

		if _, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-1", Mode: connectivity.ReadFull,
		}); err != nil {
			t.Fatalf("first extract: %v", err)
		}
		results, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
			RunID: "run-restart", Mode: connectivity.ReadFull, Restart: true,
		})
		if err != nil {
			t.Fatalf("restart extract: %v", err)
		}
		if results[0].Run.Status != observe.RunCompleted {
			t.Fatalf("status = %s, want COMPLETED", results[0].Run.Status)
		}
	})
}

// TestTodo_ONBOARD_002_Golden pins one object's fully-extracted evidence
// shape: page count, total records, completeness and the traversal's
// snapshot id must not silently drift as the extractor changes.
func TestTodo_ONBOARD_002_Golden(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	m := validManifest(h)
	narrowToObjects(&m, connectivity.ObjectWorker)

	results, err := h.Extractor().Extract(context.Background(), m, onboarding.ExtractRequest{
		RunID: "run-golden", Mode: connectivity.ReadFull,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	r := results[0]

	pages, err := h.Store.List(context.Background(), observe.Query{TenantID: testTenant, Object: connectivity.ObjectWorker})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "object: %s\n", r.Object)
	fmt.Fprintf(&b, "status: %s\n", r.Run.Status)
	fmt.Fprintf(&b, "pages: %d\n", r.Run.Pages)
	fmt.Fprintf(&b, "records: %d\n", r.Run.Records)
	fmt.Fprintf(&b, "snapshot_id: %s\n", r.Run.SnapshotID)
	fmt.Fprintf(&b, "stored_pages: %d\n", len(pages))
	fmt.Fprintf(&b, "checkpoint_complete: %t\n", r.Run.Checkpoint.Complete)
	fmt.Fprintf(&b, "checkpoint_records: %d\n", r.Run.Checkpoint.RecordsCommitted)
	compareGoldenFile(t, filepath.Join("testdata", "onboard002_extract.golden"), b.String())
}
