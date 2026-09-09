package observe_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

// firstPage reads page one of an object straight from the connector, so the
// observation tests can exercise Record without a full run.
func firstPage(t *testing.T, object connectivity.ObjectKind) (*fakeincumbent.Incumbent, connectivity.Page, connectivity.Cursor) {
	t.Helper()
	ctx := context.Background()
	inc, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	snapshot, err := inc.Snapshot(ctx, object)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	start := connectivity.StartCursor(snapshot)
	page, err := inc.Read(ctx, connectivity.ReadRequest{
		Object: object, Mode: connectivity.ReadFull, Cursor: start, Limit: inc.Bounds().MaxPageSize,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return inc, page, start
}

func recordOptions(inc *fakeincumbent.Incumbent, start connectivity.Cursor) observe.RecordOptions {
	return observe.RecordOptions{
		TenantID:        testTenant,
		Descriptor:      inc.Descriptor(),
		PageSequence:    1,
		StartCursor:     start,
		FreshnessBudget: freshnessBudget,
	}
}

// TestTodo_INTG_009 is the INTG-009 primary test.
//
// RED: a raw provider response becomes a domain fact, or an observation lacks
// source, schema, mapping, authority, freshness, watermark or digest.
// GREEN: the observation is immutable, classified, source-attributed, and
// returns fresh/stale/partial/unknown/unavailable honestly.
func TestTodo_INTG_009(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an observation carries its full attribution", func(t *testing.T) {
		t.Parallel()
		inc, page, start := firstPage(t, connectivity.ObjectWorker)
		obs, err := observe.Record(page, recordOptions(inc, start))
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := obs.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if err := obs.Verify(); err != nil {
			t.Fatalf("verify: %v", err)
		}

		descriptor := inc.Descriptor()
		checks := map[string][2]string{
			"tenant":            {obs.TenantID, testTenant},
			"connection":        {obs.ConnectionID, descriptor.ConnectionID},
			"connector":         {obs.ConnectorID, descriptor.ConnectorID},
			"connector version": {obs.ConnectorVersion, descriptor.Version.String()},
			"source":            {obs.SourceRef, descriptor.SourceRef},
			"authority":         {obs.AuthorityRef, descriptor.AuthorityRef},
			"schema version":    {obs.SchemaVersion, page.SchemaVersion},
			"snapshot":          {obs.SnapshotID, page.SnapshotID},
			"classification":    {string(obs.Classification), string(observe.ClassificationExternalObservation)},
		}
		for name, pair := range checks {
			if pair[0] != pair[1] {
				t.Fatalf("observation %s is %q, want %q", name, pair[0], pair[1])
			}
		}
		if obs.RetrievedAt.IsZero() || obs.Watermark.IsZero() {
			t.Fatalf("observation times are incomplete: retrieved=%v watermark=%v",
				obs.RetrievedAt, obs.Watermark)
		}
		if obs.RetrievedAt.Equal(obs.Watermark) {
			t.Fatal("retrieval time and source watermark collapsed into one value")
		}
		if !strings.HasPrefix(obs.ContentDigest, "sha256:") {
			t.Fatalf("content digest %q does not name its algorithm", obs.ContentDigest)
		}
		if obs.RawArtifactRef != nil {
			t.Fatal("a raw provider payload leaked into the observation by default")
		}
	})

	t.Run("an observation missing any attribution cannot be stored", func(t *testing.T) {
		t.Parallel()
		inc, page, start := firstPage(t, connectivity.ObjectWorker)
		base, err := observe.Record(page, recordOptions(inc, start))
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		cases := map[string]func(*observe.Observation){
			"no tenant":            func(o *observe.Observation) { o.TenantID = "" },
			"no connection":        func(o *observe.Observation) { o.ConnectionID = "" },
			"no connector":         func(o *observe.Observation) { o.ConnectorID = "" },
			"no connector version": func(o *observe.Observation) { o.ConnectorVersion = "" },
			"no source":            func(o *observe.Observation) { o.SourceRef = "" },
			"no authority":         func(o *observe.Observation) { o.AuthorityRef = "" },
			"no schema version":    func(o *observe.Observation) { o.SchemaVersion = "" },
			"no snapshot":          func(o *observe.Observation) { o.SnapshotID = "" },
			"no page sequence":     func(o *observe.Observation) { o.PageSequence = 0 },
			"no start cursor":      func(o *observe.Observation) { o.StartCursor = "" },
			"no retrieval time":    func(o *observe.Observation) { o.RetrievedAt = time.Time{} },
			"no freshness":         func(o *observe.Observation) { o.Freshness = "" },
			"no digest":            func(o *observe.Observation) { o.ContentDigest = "" },
			"no payload":           func(o *observe.Observation) { o.Payload = nil },
			"promoted to a domain fact": func(o *observe.Observation) {
				o.Classification = "DOMAIN_FACT"
			},
		}
		store := observe.NewMemoryStore()
		for name, mutate := range cases {
			broken := base
			mutate(&broken)
			if err := broken.Validate(); !errors.Is(err, observe.ErrIncomplete) {
				t.Fatalf("%s: Validate returned %v, want ErrIncomplete", name, err)
			}
			if _, err := store.Append(ctx, broken); err == nil {
				t.Fatalf("%s: an incomplete observation was stored", name)
			}
		}
	})

	t.Run("appending is idempotent and content is immutable", func(t *testing.T) {
		t.Parallel()
		inc, page, start := firstPage(t, connectivity.ObjectWorker)
		obs, err := observe.Record(page, recordOptions(inc, start))
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		store := observe.NewMemoryStore()

		first, err := store.Append(ctx, obs)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if first.Existing {
			t.Fatal("the first append reported the observation as pre-existing")
		}
		second, err := store.Append(ctx, obs)
		if err != nil {
			t.Fatalf("re-append: %v", err)
		}
		if !second.Existing {
			t.Fatal("re-appending identical evidence was treated as new")
		}

		// Different content under the same identity is refused.
		forged := obs
		forged.RecordCount++
		if _, err := store.Append(ctx, forged); err == nil {
			t.Fatal("forged evidence was stored under an existing observation id")
		}
		stored, err := store.Get(ctx, testTenant, obs.ObservationID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if stored.RecordCount != obs.RecordCount {
			t.Fatalf("the refused write still changed the record count to %d", stored.RecordCount)
		}
	})

	t.Run("observation identity is derived, not allocated", func(t *testing.T) {
		t.Parallel()
		identity := observe.PageIdentity{
			TenantID:     testTenant,
			ConnectionID: "conn-a",
			Object:       connectivity.ObjectWorker,
			SnapshotID:   "snap-1",
			PageSequence: 2,
		}
		first := identity.ObservationID()
		if again := identity.ObservationID(); first != again {
			t.Fatalf("observation identity is not stable: %s then %s", first, again)
		}
		other := identity
		other.PageSequence = 3
		if identity.ObservationID() == other.ObservationID() {
			t.Fatal("two pages of one snapshot share an observation id")
		}
		other = identity
		other.TenantID = "5e3f1c2b-0000-4000-8000-000000000002"
		if identity.ObservationID() == other.ObservationID() {
			t.Fatal("two tenants share an observation id")
		}
	})

	t.Run("freshness is reported honestly", func(t *testing.T) {
		t.Parallel()
		base := connectivity.Page{
			RetrievedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
			Watermark:   time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC),
		}
		cases := []struct {
			name   string
			page   connectivity.Page
			budget time.Duration
			want   observe.Freshness
		}{
			{"within budget", base, 2 * time.Hour, observe.FreshnessFresh},
			{"beyond budget", base, 30 * time.Minute, observe.FreshnessStale},
			{"no watermark", connectivity.Page{RetrievedAt: base.RetrievedAt}, time.Hour, observe.FreshnessUnknown},
			{"no budget", base, 0, observe.FreshnessUnknown},
			{"partial page", connectivity.Page{
				RetrievedAt: base.RetrievedAt, Watermark: base.Watermark, Partial: true,
			}, 2 * time.Hour, observe.FreshnessPartial},
		}
		for _, tc := range cases {
			if got := observe.Classify(tc.page, tc.budget); got != tc.want {
				t.Fatalf("%s: freshness is %s, want %s", tc.name, got, tc.want)
			}
		}
	})

	t.Run("replay verifies stored evidence end to end", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		if _, err := observe.RunToCompletion(ctx, h.Runner, request(connectivity.ObjectWorker), 32); err != nil {
			t.Fatalf("run: %v", err)
		}
		replayed, err := observe.Replay(ctx, h.Store, observe.Query{
			TenantID: testTenant, Object: connectivity.ObjectWorker,
		})
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if len(replayed) == 0 {
			t.Fatal("replay returned no evidence")
		}
		for i, obs := range replayed {
			if obs.PageSequence != uint64(i+1) {
				t.Fatalf("replay page %d has sequence %d", i, obs.PageSequence)
			}
			if err := obs.Verify(); err != nil {
				t.Fatalf("replayed page %d does not verify: %v", i+1, err)
			}
		}
	})

	t.Run("a retained raw payload is a reference, never the evidence", func(t *testing.T) {
		t.Parallel()
		inc, page, start := firstPage(t, connectivity.ObjectWorker)
		opts := recordOptions(inc, start)
		ref := "artifact://raw/workday/worker/page-1"
		opts.RawArtifactRef = &ref

		obs, err := observe.Record(page, opts)
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		if obs.RawArtifactRef == nil || *obs.RawArtifactRef != ref {
			t.Fatalf("raw artifact ref is %v", obs.RawArtifactRef)
		}
		// Dropping the raw payload under a retention policy must not disturb
		// the evidence: the digest covers the normalized page only.
		withoutRaw := recordOptions(inc, start)
		plain, err := observe.Record(page, withoutRaw)
		if err != nil {
			t.Fatalf("record without raw ref: %v", err)
		}
		if plain.ContentDigest != obs.ContentDigest {
			t.Fatal("the raw artifact reference contributed to the content digest")
		}
		if err := obs.Verify(); err != nil {
			t.Fatalf("verify with a raw ref: %v", err)
		}
	})
}

// TestTodo_INTG_009_Golden pins the observation payload and digest of the first
// worker page. A change to the canonical page encoding breaks this test, which
// is what stops evidence recorded today from failing verification tomorrow.
func TestTodo_INTG_009_Golden(t *testing.T) {
	t.Parallel()

	inc, page, start := firstPage(t, connectivity.ObjectWorker)
	obs, err := observe.Record(page, recordOptions(inc, start))
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "object: %s\n", obs.Object)
	fmt.Fprintf(&b, "schema_version: %s\n", obs.SchemaVersion)
	fmt.Fprintf(&b, "snapshot_id: %s\n", obs.SnapshotID)
	fmt.Fprintf(&b, "page_sequence: %d\n", obs.PageSequence)
	fmt.Fprintf(&b, "record_count: %d\n", obs.RecordCount)
	fmt.Fprintf(&b, "complete: %t\n", obs.Complete)
	fmt.Fprintf(&b, "classification: %s\n", obs.Classification)
	fmt.Fprintf(&b, "freshness: %s\n", obs.Freshness)
	fmt.Fprintf(&b, "observation_id: %s\n", obs.ObservationID)
	fmt.Fprintf(&b, "content_digest: %s\n", obs.ContentDigest)
	fmt.Fprintf(&b, "canonical_length: %d\n", len(obs.Payload))
	compareGoldenFile(t, filepath.Join("testdata", "intg009_observation.golden"), b.String())

	// The same page recorded again digests identically even though the
	// retrieval clock moved on.
	inc2, page2, start2 := firstPage(t, connectivity.ObjectWorker)
	again, err := observe.Record(page2, recordOptions(inc2, start2))
	if err != nil {
		t.Fatalf("re-record: %v", err)
	}
	if again.ContentDigest != obs.ContentDigest {
		t.Fatalf("re-reading the same page produced digest %s, first read produced %s",
			again.ContentDigest, obs.ContentDigest)
	}
	if again.ObservationID != obs.ObservationID {
		t.Fatalf("re-reading the same page produced observation %s, first read produced %s",
			again.ObservationID, obs.ObservationID)
	}
}

func compareGoldenFile(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(got, "\r\n", "\n") {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// TestTodo_INTG_009_Mutation flips one thing at a time in stored evidence and
// requires each flip to be caught. An evidence store that cannot detect
// tampering is a filing cabinet, not evidence.
func TestTodo_INTG_009_Mutation(t *testing.T) {
	t.Parallel()

	inc, page, start := firstPage(t, connectivity.ObjectWorker)
	obs, err := observe.Record(page, recordOptions(inc, start))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := obs.Verify(); err != nil {
		t.Fatalf("baseline verify: %v", err)
	}

	t.Run("a flipped payload byte fails verification", func(t *testing.T) {
		t.Parallel()
		for _, offset := range []int{0, len(obs.Payload) / 2, len(obs.Payload) - 1} {
			tampered := obs
			tampered.Payload = append([]byte(nil), obs.Payload...)
			tampered.Payload[offset] ^= 0x01
			if err := tampered.Verify(); !errors.Is(err, observe.ErrDigestMismatch) {
				t.Fatalf("flipping byte %d was not detected: %v", offset, err)
			}
		}
	})

	t.Run("a truncated payload fails verification", func(t *testing.T) {
		t.Parallel()
		tampered := obs
		tampered.Payload = obs.Payload[:len(obs.Payload)-1]
		if err := tampered.Verify(); !errors.Is(err, observe.ErrDigestMismatch) {
			t.Fatalf("truncation was not detected: %v", err)
		}
	})

	t.Run("a substituted digest fails verification", func(t *testing.T) {
		t.Parallel()
		tampered := obs
		tampered.ContentDigest = "sha256:" + strings.Repeat("0", 64)
		if err := tampered.Verify(); !errors.Is(err, observe.ErrDigestMismatch) {
			t.Fatalf("a substituted digest was not detected: %v", err)
		}
	})

	t.Run("every material page field changes the digest", func(t *testing.T) {
		t.Parallel()
		mutations := map[string]func(*connectivity.Page, *observe.RecordOptions){
			"schema version": func(p *connectivity.Page, _ *observe.RecordOptions) {
				p.SchemaVersion += "x"
			},
			"snapshot": func(p *connectivity.Page, _ *observe.RecordOptions) {
				p.SnapshotID += "x"
				p.NextCursor.SnapshotID = p.SnapshotID
			},
			"completeness": func(p *connectivity.Page, _ *observe.RecordOptions) {
				p.Complete = !p.Complete
			},
			"partiality": func(p *connectivity.Page, _ *observe.RecordOptions) { p.Partial = !p.Partial },
			"record external id": func(p *connectivity.Page, _ *observe.RecordOptions) {
				p.Records[0].ExternalID += "x"
			},
			"record source version": func(p *connectivity.Page, _ *observe.RecordOptions) {
				p.Records[0].SourceVersion += "x"
			},
			"record field value": func(p *connectivity.Page, _ *observe.RecordOptions) {
				fields := map[string]string{}
				for k, v := range p.Records[0].Fields {
					fields[k] = v
				}
				fields["job_code"] += "x"
				p.Records[0].Fields = fields
			},
			"record count": func(p *connectivity.Page, _ *observe.RecordOptions) {
				p.Records = p.Records[:len(p.Records)-1]
			},
			"page sequence": func(_ *connectivity.Page, o *observe.RecordOptions) { o.PageSequence++ },
			"tenant":        func(_ *connectivity.Page, o *observe.RecordOptions) { o.TenantID += "x" },
			"start cursor": func(_ *connectivity.Page, o *observe.RecordOptions) {
				o.StartCursor.LastExternalID += "x"
			},
		}
		for name, mutate := range mutations {
			mutatedInc, mutatedPage, mutatedStart := firstPage(t, connectivity.ObjectWorker)
			opts := recordOptions(mutatedInc, mutatedStart)
			mutate(&mutatedPage, &opts)
			mutated, err := observe.Record(mutatedPage, opts)
			if err != nil {
				t.Fatalf("%s: record: %v", name, err)
			}
			if mutated.ContentDigest == obs.ContentDigest {
				t.Fatalf("changing the %s did not change the content digest", name)
			}
		}
	})

	t.Run("a replay over tampered evidence fails", func(t *testing.T) {
		t.Parallel()
		store := &tamperingStore{MemoryStore: observe.NewMemoryStore()}
		h := newHarness(t)
		h.Runner.Observations = store
		if _, err := observe.RunToCompletion(context.Background(), h.Runner,
			request(connectivity.ObjectWorker), 32); err != nil {
			t.Fatalf("run: %v", err)
		}
		store.tamper = true
		_, err := observe.Replay(context.Background(), store, observe.Query{
			TenantID: testTenant, Object: connectivity.ObjectWorker,
		})
		if !errors.Is(err, observe.ErrDigestMismatch) {
			t.Fatalf("replay over tampered evidence returned %v, want ErrDigestMismatch", err)
		}
	})
}

// tamperingStore corrupts what it hands back once tamper is set, so that
// replay verification is tested against real corruption rather than a mock.
type tamperingStore struct {
	*observe.MemoryStore
	tamper bool
}

func (s *tamperingStore) List(ctx context.Context, q observe.Query) ([]observe.Observation, error) {
	out, err := s.MemoryStore.List(ctx, q)
	if err != nil || !s.tamper || len(out) == 0 {
		return out, err
	}
	out[0].Payload = append([]byte(nil), out[0].Payload...)
	out[0].Payload[0] ^= 0xff
	return out, nil
}

// TestTodo_INTG_009_Fault proves an observation reports an honest verdict when
// the source misbehaves, and that a failed read stores nothing rather than
// storing a hopeful blank.
func TestTodo_INTG_009_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a stale source produces stale observations", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		// The fixtures carry historical watermarks, so a one-minute budget
		// makes every page legitimately stale.
		h.Runner.FreshnessBudget = time.Minute

		result, err := observe.RunToCompletion(ctx, h.Runner, request(connectivity.ObjectWorker), 32)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result.Freshness != observe.FreshnessStale {
			t.Fatalf("run freshness is %s, want STALE", result.Freshness)
		}
		for _, obs := range observedPages(t, h.Store, connectivity.ObjectWorker) {
			if obs.Freshness != observe.FreshnessStale {
				t.Fatalf("page %d reports freshness %s", obs.PageSequence, obs.Freshness)
			}
		}
	})

	t.Run("a connector with no freshness budget reports unknown, not fresh", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Runner.FreshnessBudget = 0
		result, err := observe.RunToCompletion(ctx, h.Runner, request(connectivity.ObjectWorker), 32)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result.Freshness != observe.FreshnessUnknown {
			t.Fatalf("run freshness is %s, want UNKNOWN", result.Freshness)
		}
	})

	t.Run("a failed read stores no evidence", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.Incumbent.InjectFaults(fakeincumbent.Faults{TransientOnReads: []int{1}})
		result, err := h.Runner.Run(ctx, request(connectivity.ObjectWorker))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if result.Status != observe.RunInterrupted {
			t.Fatalf("status is %s", result.Status)
		}
		if pages := observedPages(t, h.Store, connectivity.ObjectWorker); len(pages) != 0 {
			t.Fatalf("a failed read stored %d observations", len(pages))
		}
		if _, err := observe.Replay(ctx, h.Store, observe.Query{
			TenantID: testTenant, Object: connectivity.ObjectWorker,
		}); !errors.Is(err, observe.ErrNotFound) {
			t.Fatalf("replay over an empty store returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a sequence gap is caught by replay", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		if _, err := observe.RunToCompletion(ctx, h.Runner,
			request(connectivity.ObjectPosition), 32); err != nil {
			t.Fatalf("run: %v", err)
		}
		_, err := observe.Replay(ctx, h.Store, observe.Query{
			TenantID: testTenant, Object: connectivity.ObjectPosition, FromPage: 2,
		})
		if !errors.Is(err, observe.ErrSequenceGap) {
			t.Fatalf("replay starting at page 2 returned %v, want ErrSequenceGap", err)
		}
	})
}

// FuzzTodo_INTG_009 fuzzes observation recording. No page and no options may
// produce an observation that fails its own validation or verification, and no
// input may panic.
func FuzzTodo_INTG_009(f *testing.F) {
	f.Add("WORKER", "schema/1", "snap-1", uint64(1), 2, true, int64(3600))
	f.Add("", "", "", uint64(0), 0, false, int64(0))
	f.Add("POSITION", "\x00", "\xff", uint64(1<<32), 5, true, int64(-1))
	f.Add("COMPENSATION", "s", "n", uint64(1), 1, false, int64(1))

	descriptor := fakeincumbent.DefaultDescriptor()

	f.Fuzz(func(t *testing.T, object, schema, snapshot string, seq uint64, records int, complete bool, budgetSec int64) {
		if records < 0 || records > 32 {
			t.Skip()
		}
		retrieved := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		page := connectivity.Page{
			Object:        connectivity.ObjectKind(object),
			SchemaVersion: schema,
			SnapshotID:    snapshot,
			Complete:      complete,
			RetrievedAt:   retrieved,
			Watermark:     retrieved.Add(-time.Hour),
			NextCursor:    connectivity.Cursor{SnapshotID: snapshot},
		}
		for i := range records {
			page.Records = append(page.Records, connectivity.Record{
				ExternalID:    fmt.Sprintf("id-%04d", i),
				SortKey:       fmt.Sprintf("key-%04d", i),
				SourceVersion: "v1",
				ObservedAt:    retrieved.Add(-time.Duration(i) * time.Minute),
				Fields:        map[string]string{"a": "1", "b": "2"},
			})
		}

		obs, err := observe.Record(page, observe.RecordOptions{
			TenantID:        testTenant,
			Descriptor:      descriptor,
			PageSequence:    seq,
			StartCursor:     connectivity.StartCursor(snapshot),
			FreshnessBudget: time.Duration(budgetSec) * time.Second,
		})
		if err != nil {
			// Every rejection must be a classified one, never a bare error.
			var connErr *connectivity.Error
			var obsErr *observe.Error
			if !errors.As(err, &connErr) && !errors.As(err, &obsErr) {
				t.Fatalf("Record failed with an unclassified error: %v", err)
			}
			return
		}
		if err := obs.Validate(); err != nil {
			t.Fatalf("Record produced an observation that fails Validate: %v", err)
		}
		if err := obs.Verify(); err != nil {
			t.Fatalf("Record produced an observation that fails Verify: %v", err)
		}
		if obs.Classification != observe.ClassificationExternalObservation {
			t.Fatalf("Record produced classification %q", obs.Classification)
		}
		if !obs.Freshness.Valid() {
			t.Fatalf("Record produced freshness %q", obs.Freshness)
		}
		algorithm, hex, err := observe.SplitDigest(obs.ContentDigest)
		if err != nil {
			t.Fatalf("split digest %q: %v", obs.ContentDigest, err)
		}
		if observe.JoinDigest(algorithm, hex) != obs.ContentDigest {
			t.Fatalf("digest did not round-trip: %s", obs.ContentDigest)
		}
		if len(hex) != 64 {
			t.Fatalf("digest hex is %d characters", len(hex))
		}
	})
}
