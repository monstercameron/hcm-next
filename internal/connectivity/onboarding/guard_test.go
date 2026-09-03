package onboarding_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/hcm-next/internal/connectivity/onboarding"
)

// TestTodo_ONBOARD_004 proves the whole-run resource budget stops extraction
// at its limit and leaves a resumable checkpoint, and that a malformed or
// oversized record is isolated into the quarantine log rather than failing
// the page - and, symmetrically, the batch - it arrived in.
func TestTodo_ONBOARD_004(t *testing.T) {
	t.Parallel()

	t.Run("a page budget of one stops extraction after exactly one page", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		guard := onboarding.NewBudgetGuard(h.Incumbent, onboarding.Budget{
			MaxPages: 1, MaxRecords: 1 << 20, MaxBytes: 1 << 20, MaxWallTime: time.Hour,
		}, h.Clock)

		page1, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 2,
		})
		if err != nil {
			t.Fatalf("first read: %v", err)
		}
		if page1.Complete {
			t.Fatal("fixture completed in one page; the test needs at least two to prove the budget stops early")
		}

		_, err = guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: page1.NextCursor, Limit: 2,
		})
		if !errors.Is(err, onboarding.ErrBudgetExceeded) {
			t.Fatalf("second read returned %v, want ErrBudgetExceeded", err)
		}
		pages, records, _, _ := guard.Spent()
		if pages != 1 {
			t.Fatalf("spent %d pages, want 1 (the refused read must not count)", pages)
		}
		if records != uint64(len(page1.Records)) {
			t.Fatalf("spent %d records, want %d", records, len(page1.Records))
		}
	})

	t.Run("a record budget stops extraction once it is met, not before", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		total := h.Incumbent.RecordCount(connectivity.ObjectWorker)
		guard := onboarding.NewBudgetGuard(h.Incumbent, onboarding.Budget{
			MaxPages: 1 << 20, MaxRecords: uint64(total), MaxBytes: 1 << 20, MaxWallTime: time.Hour,
		}, h.Clock)

		cursor := connectivity.Cursor{}
		seen := 0
		for {
			page, err := guard.Read(context.Background(), connectivity.ReadRequest{
				Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: cursor, Limit: 1,
			})
			if err != nil {
				t.Fatalf("read at %d records seen: %v", seen, err)
			}
			seen += len(page.Records)
			if page.Complete {
				break
			}
			cursor = page.NextCursor
		}
		if seen != total {
			t.Fatalf("saw %d records, want exactly %d before the budget stopped further reads", seen, total)
		}
		_, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: cursor, Limit: 1,
		})
		if !errors.Is(err, onboarding.ErrBudgetExceeded) {
			t.Fatalf("read past the record budget returned %v, want ErrBudgetExceeded", err)
		}
	})

	t.Run("a wall-time budget stops extraction once the injected clock passes it", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		tick := 0
		clock := func() time.Time {
			t := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(tick) * time.Minute)
			tick++
			return t
		}
		guard := onboarding.NewBudgetGuard(h.Incumbent, onboarding.Budget{
			MaxPages: 1 << 20, MaxRecords: 1 << 20, MaxBytes: 1 << 20, MaxWallTime: time.Minute,
		}, clock)

		if _, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1,
		}); err != nil {
			t.Fatalf("first read: %v", err)
		}
		// The clock has advanced one minute since the first read set the
		// guard's start time, meeting the one-minute budget.
		_, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1,
		})
		if !errors.Is(err, onboarding.ErrBudgetExceeded) {
			t.Fatalf("read past the wall-time budget returned %v, want ErrBudgetExceeded", err)
		}
	})

	t.Run("budget exhaustion is never reported as an external failure", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		guard := onboarding.NewBudgetGuard(h.Incumbent, onboarding.Budget{
			MaxPages: 0, MaxRecords: 1, MaxBytes: 1, MaxWallTime: time.Hour,
		}, h.Clock)
		_, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1,
		})
		if !errors.Is(err, onboarding.ErrBudgetExceeded) {
			t.Fatalf("error = %v, want ErrBudgetExceeded", err)
		}
	})

	t.Run("a malformed record is quarantined and its page still returns every well-formed record", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		poisoning := &poisoningConnector{Connector: h.Incumbent, poisonExternalID: "POS-HRBP-204"}
		guard := onboarding.NewBudgetGuard(poisoning, onboarding.Budget{
			MaxPages: 1 << 20, MaxRecords: 1 << 20, MaxBytes: 1 << 20, MaxWallTime: time.Hour,
		}, h.Clock)

		page, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectPosition, Mode: connectivity.ReadFull, Limit: 2,
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for _, rec := range page.Records {
			if rec.ExternalID == "POS-HRBP-204" {
				t.Fatal("the poisoned record was returned in the page instead of being quarantined")
			}
		}
		quarantine := guard.Quarantine()
		if len(quarantine) != 1 {
			t.Fatalf("quarantine has %d entries, want 1", len(quarantine))
		}
		if quarantine[0].ExternalID != "POS-HRBP-204" || quarantine[0].Reason == "" {
			t.Fatalf("quarantine entry incomplete: %+v", quarantine[0])
		}
	})

	t.Run("an oversized record is quarantined for exceeding the connector's per-record bound", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		oversizing := &oversizingConnector{Connector: h.Incumbent, targetExternalID: "POS-HRBP-204"}
		guard := onboarding.NewBudgetGuard(oversizing, onboarding.Budget{
			MaxPages: 1 << 20, MaxRecords: 1 << 20, MaxBytes: 1 << 20, MaxWallTime: time.Hour,
		}, h.Clock)

		page, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectPosition, Mode: connectivity.ReadFull, Limit: 2,
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for _, rec := range page.Records {
			if rec.ExternalID == "POS-HRBP-204" {
				t.Fatal("the oversized record was returned instead of being quarantined")
			}
		}
		if len(guard.Quarantine()) != 1 {
			t.Fatalf("quarantine has %d entries, want 1", len(guard.Quarantine()))
		}
	})

	t.Run("a field allow list and a budget guard compose without interference", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		filtered := onboarding.FilterFields(h.Incumbent, map[connectivity.ObjectKind][]string{
			connectivity.ObjectWorker: {"worker_number"},
		})
		guard := onboarding.NewBudgetGuard(filtered, onboarding.Budget{
			MaxPages: 1 << 20, MaxRecords: 1 << 20, MaxBytes: 1 << 20, MaxWallTime: time.Hour,
		}, h.Clock)
		page, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 2,
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for _, rec := range page.Records {
			if len(rec.Fields) != 1 || rec.Fields["worker_number"] == "" {
				t.Fatalf("record %s fields = %v, want only worker_number", rec.ExternalID, rec.Fields)
			}
		}
	})
}

// poisoningConnector wraps a connectivity.Connector, blanking out one
// record's external id after the wrapped connector already validated it, so
// [onboarding.BudgetGuard] sees exactly the malformed shape a real poison row
// would arrive as.
type poisoningConnector struct {
	connectivity.Connector
	poisonExternalID string
}

func (p *poisoningConnector) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	page, err := p.Connector.Read(ctx, req)
	if err != nil {
		return page, err
	}
	for i, rec := range page.Records {
		if rec.ExternalID == p.poisonExternalID {
			rec.SourceVersion = ""
			page.Records[i] = rec
		}
	}
	return page, nil
}

// oversizingConnector wraps a connectivity.Connector, inflating one record
// with an oversized field so it exceeds the connector's declared
// MaxRecordBytes bound.
type oversizingConnector struct {
	connectivity.Connector
	targetExternalID string
}

func (o *oversizingConnector) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	page, err := o.Connector.Read(ctx, req)
	if err != nil {
		return page, err
	}
	for i, rec := range page.Records {
		if rec.ExternalID == o.targetExternalID {
			fields := map[string]string{}
			for k, v := range rec.Fields {
				fields[k] = v
			}
			fields["oversized"] = string(make([]byte, o.Connector.Bounds().MaxRecordBytes+1))
			rec.Fields = fields
			page.Records[i] = rec
		}
	}
	return page, nil
}

// FuzzTodo_ONBOARD_004 proves the budget guard's admit/quarantine/stop
// decision is a total, panic-free function of arbitrary record shapes and
// arbitrary budgets: every record is either admitted counted-once,
// quarantined with a reason, or the whole read is refused for exhausting the
// budget - never more than one of those, and never a crash.
func FuzzTodo_ONBOARD_004(f *testing.F) {
	f.Add("", "", "", int64(0), uint64(1), uint64(1), uint64(1))
	f.Add("ext-1", "sort-1", "v1", int64(1735689600), uint64(0), uint64(0), uint64(0))
	f.Add("ext-2", "sort-2", "v2", int64(-1), uint64(5), uint64(1<<20), uint64(1<<20))

	f.Fuzz(func(t *testing.T, externalID, sortKey, sourceVersion string, observedUnix int64, maxPages, maxRecords, maxBytes uint64) {
		fake := &singleRecordConnector{
			descriptor: fakeincumbent.DefaultDescriptor(),
			bounds:     connectivity.Bounds{MaxPageSize: 10, MaxPagesPerRun: 10, MaxRecordsPerRun: 100, MaxRecordBytes: 4096},
			record: connectivity.Record{
				ExternalID:    externalID,
				SortKey:       sortKey,
				SourceVersion: sourceVersion,
				ObservedAt:    time.Unix(observedUnix, 0).UTC(),
				Fields:        map[string]string{"a": externalID, "b": sortKey},
			},
		}
		if observedUnix == 0 {
			fake.record.ObservedAt = time.Time{}
		}

		guard := onboarding.NewBudgetGuard(fake, onboarding.Budget{
			MaxPages: maxPages, MaxRecords: maxRecords, MaxBytes: maxBytes, MaxWallTime: time.Hour,
		}, func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) })

		page, err := guard.Read(context.Background(), connectivity.ReadRequest{
			Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1,
		})

		admitted := err == nil && len(page.Records) == 1
		quarantined := err == nil && len(guard.Quarantine()) == 1
		budgetStopped := errors.Is(err, onboarding.ErrBudgetExceeded)

		switch {
		case err != nil && !budgetStopped:
			t.Fatalf("unexpected error class: %v", err)
		case budgetStopped && (admitted || quarantined):
			t.Fatalf("budget-stopped read still admitted or quarantined a record")
		case err == nil && admitted == quarantined:
			t.Fatalf("a served read admitted=%t quarantined=%t, want exactly one", admitted, quarantined)
		}
	})
}

// singleRecordConnector is a minimal connectivity.Connector that always
// returns exactly one record (or zero, for a fuzz case with an already-empty
// object) as a complete page, so the fuzz target exercises BudgetGuard alone
// rather than fakeincumbent's own pagination.
type singleRecordConnector struct {
	descriptor connectivity.Descriptor
	bounds     connectivity.Bounds
	record     connectivity.Record
}

func (c *singleRecordConnector) Descriptor() connectivity.Descriptor { return c.descriptor }
func (c *singleRecordConnector) Bounds() connectivity.Bounds         { return c.bounds }
func (c *singleRecordConnector) Capabilities() []connectivity.Capability {
	return connectivity.ReadCapabilities(connectivity.ObjectWorker)
}
func (c *singleRecordConnector) SchemaVersion(context.Context, connectivity.ObjectKind) (string, error) {
	return "fuzz.schema/1", nil
}
func (c *singleRecordConnector) Snapshot(context.Context, connectivity.ObjectKind) (string, error) {
	return "fuzz-snapshot", nil
}
func (c *singleRecordConnector) Read(_ context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	return connectivity.Page{
		Object:        req.Object,
		SchemaVersion: "fuzz.schema/1",
		SnapshotID:    "fuzz-snapshot",
		Records:       []connectivity.Record{c.record},
		Complete:      true,
		RetrievedAt:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}, nil
}

var _ connectivity.Connector = (*singleRecordConnector)(nil)
