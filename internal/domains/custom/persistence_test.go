package custom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func persistenceDefinition() CustomObjectDefinition {
	return CustomObjectDefinition{
		Kind: "Asset", Namespace: "inventory", Version: 1,
		Fields: map[string]FieldDefinition{
			"serial": {
				Type: "string",
				Classification: FieldClassification{
					AuthZDomain: "worker.core", Classification: "INTERNAL",
					ResidencyRef: "NO_CONSTRAINT", RetentionClass: "PERMANENT",
				},
			},
		},
	}
}

func persistenceRecord(t *testing.T) CustomRecordRevision {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	effective, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(start)
	if err != nil {
		t.Fatal(err)
	}
	return CustomRecordRevision{
		ObjectID: "asset-1", ObjectKind: "Asset", Namespace: "inventory",
		DefinitionVersion: 1, Effective: effective, Known: known,
		FieldValues: map[string]TypedValue{
			"serial": {FieldName: "serial", Type: "string", Value: "SN-1"},
		},
	}
}

func TestCustomMemoryStoreSatisfiesStore(t *testing.T) {
	store := NewMemoryStore()
	var port Store = store
	ctx := context.Background()
	if err := port.SaveObjectDefinition(ctx, "tenant-a", persistenceDefinition(), 0); err != nil {
		t.Fatalf("SaveObjectDefinition: %v", err)
	}
	if err := port.SaveObjectDefinition(ctx, "tenant-a", persistenceDefinition(), 0); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate definition = %v, want ErrStoreDuplicate", err)
	}
	next := persistenceDefinition()
	next.Version = 3
	if err := port.SaveObjectDefinition(ctx, "tenant-a", next, 1); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("gapped definition = %v, want ErrStoreStaleCAS", err)
	}

	record := persistenceRecord(t)
	if err := port.SaveRecordRevision(ctx, "tenant-a", record); err != nil {
		t.Fatalf("SaveRecordRevision: %v", err)
	}
	rows, err := port.ListRecordRevisions(ctx, "tenant-a", record.ObjectID)
	if err != nil {
		t.Fatalf("ListRecordRevisions: %v", err)
	}
	if len(rows) != 1 || rows[0].FieldValues["serial"].Value != "SN-1" {
		t.Fatalf("records = %#v, want one preserved record", rows)
	}
}

func TestValidatePersistableRecordRejectsDeclaredTypeMismatch(t *testing.T) {
	record := persistenceRecord(t)
	record.FieldValues["serial"] = TypedValue{FieldName: "serial", Type: "integer", Value: 1}
	if err := ValidatePersistableRecord(persistenceDefinition(), record); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("type mismatch = %v, want ErrStoreInvalid", err)
	}
}
