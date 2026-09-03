package fakeincumbent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/fakeincumbent"
)

func newIncumbent(t *testing.T) *fakeincumbent.Incumbent {
	t.Helper()
	inc, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	return inc
}

// drain walks an object to completion and returns every record in order.
func drain(t *testing.T, inc *fakeincumbent.Incumbent, object connectivity.ObjectKind) []connectivity.Record {
	t.Helper()
	ctx := context.Background()
	snapshot, err := inc.Snapshot(ctx, object)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	cursor := connectivity.StartCursor(snapshot)
	var out []connectivity.Record
	for range 64 {
		page, err := inc.Read(ctx, connectivity.ReadRequest{
			Object: object, Mode: connectivity.ReadFull, Cursor: cursor,
			Limit: inc.Bounds().MaxPageSize,
		})
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if err := page.Validate(); err != nil {
			t.Fatalf("page: %v", err)
		}
		out = append(out, page.Records...)
		if page.Complete {
			return out
		}
		cursor = page.NextCursor
	}
	t.Fatal("traversal did not complete within 64 pages")
	return nil
}

func TestSeedDataIsDeterministicAndOrdered(t *testing.T) {
	t.Parallel()
	for _, object := range connectivity.ObjectKinds() {
		first := drain(t, newIncumbent(t), object)
		second := drain(t, newIncumbent(t), object)
		if len(first) == 0 {
			t.Fatalf("%s seeded no records", object)
		}
		if len(first) != len(second) {
			t.Fatalf("%s yielded %d then %d records", object, len(first), len(second))
		}
		for i := range first {
			if first[i].ExternalID != second[i].ExternalID {
				t.Fatalf("%s record %d differs between instances: %s vs %s",
					object, i, first[i].ExternalID, second[i].ExternalID)
			}
			if i > 0 && first[i-1].SortKey >= first[i].SortKey && first[i-1].ExternalID >= first[i].ExternalID {
				t.Fatalf("%s is not strictly ordered at index %d", object, i)
			}
		}
	}
}

func TestSnapshotIsStableAndCarriesTheSchemaVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inc := newIncumbent(t)

	first, err := inc.Snapshot(ctx, connectivity.ObjectWorker)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	second, err := inc.Snapshot(ctx, connectivity.ObjectWorker)
	if err != nil {
		t.Fatalf("snapshot again: %v", err)
	}
	if first != second {
		t.Fatalf("snapshot id changed with no data change: %s then %s", first, second)
	}
	version, err := inc.SchemaVersion(ctx, connectivity.ObjectWorker)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != fakeincumbent.SchemaWorkerV1 {
		t.Fatalf("worker schema version is %s", version)
	}
}

func TestInjectedFaultsClassifyCorrectly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name   string
		faults fakeincumbent.Faults
		want   error
	}{
		{"credential", fakeincumbent.Faults{CredentialInvalid: true}, connectivity.ErrCredential},
		{"permission", fakeincumbent.Faults{
			PermissionDenied: []connectivity.ObjectKind{connectivity.ObjectWorker},
		}, connectivity.ErrPermission},
		{"transient", fakeincumbent.Faults{TransientOnReads: []int{1}}, connectivity.ErrTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inc := newIncumbent(t)
			snapshot, err := inc.Snapshot(ctx, connectivity.ObjectWorker)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			inc.InjectFaults(tc.faults)
			_, err = inc.Read(ctx, connectivity.ReadRequest{
				Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull,
				Cursor: connectivity.StartCursor(snapshot), Limit: 1,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("read returned %v, want %v", err, tc.want)
			}
			class, ok := connectivity.ClassOf(err)
			if !ok {
				t.Fatalf("error %v carries no class", err)
			}
			if class.Retryable() != (tc.name == "transient") {
				t.Fatalf("class %s reports retryable=%t", class, class.Retryable())
			}
			log := inc.Calls()
			last := log[len(log)-1]
			if last.Class != class {
				t.Fatalf("call log recorded class %s, error carried %s", last.Class, class)
			}
		})
	}
}

func TestSchemaDriftChangesTheSnapshotAndIsReportedAsSchemaDrift(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inc := newIncumbent(t)
	inc.InjectFaults(fakeincumbent.Faults{DriftAfterReads: 1})

	snapshot, err := inc.Snapshot(ctx, connectivity.ObjectWorker)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	page, err := inc.Read(ctx, connectivity.ReadRequest{
		Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull,
		Cursor: connectivity.StartCursor(snapshot), Limit: 1,
	})
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if page.SchemaVersion != fakeincumbent.SchemaWorkerV1 {
		t.Fatalf("first page read under %s", page.SchemaVersion)
	}
	if _, err := inc.Read(ctx, connectivity.ReadRequest{
		Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull,
		Cursor: page.NextCursor, Limit: 1,
	}); !errors.Is(err, connectivity.ErrSchema) {
		t.Fatalf("read after drift returned %v, want ErrSchema", err)
	}
}

func TestReadsNeverMutateTheStore(t *testing.T) {
	t.Parallel()
	inc := newIncumbent(t)
	before, err := inc.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	for _, object := range connectivity.ObjectKinds() {
		drain(t, inc, object)
	}
	after, err := inc.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if before != after {
		t.Fatalf("the store changed during a read-only traversal:\nbefore %s\nafter  %s", before, after)
	}
	if n := inc.MutatingCalls(); n != 0 {
		t.Fatalf("the traversal performed %d mutating calls", n)
	}
}

func TestBoundsAreEnforcedRatherThanClamped(t *testing.T) {
	t.Parallel()
	inc := newIncumbent(t)
	_, err := inc.Read(context.Background(), connectivity.ReadRequest{
		Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull,
		Limit: inc.Bounds().MaxPageSize + 1,
	})
	if !errors.Is(err, connectivity.ErrBounds) {
		t.Fatalf("an over-large page request returned %v, want ErrBounds", err)
	}
}
