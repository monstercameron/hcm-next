package health

import (
	"fmt"
	"os"

	"github.com/monstercameron/hcm-next/internal/data/tenancy/storagedisposition"
)

// DefaultRegistryPath is where STORE-001's published physical
// storage-disposition registry lives in this repository, relative to the
// module root.
const DefaultRegistryPath = "definitions/storage/storage-disposition.yaml"

// StoreDisposition mirrors one row of STORE-001's registry -- exactly the
// facts a health check needs about a table, and nothing about how to reach
// it. Fields this package never reads (migration file, notes, encryption
// class, ...) are intentionally not carried here; a caller that needs them
// reads the registry file directly.
type StoreDisposition struct {
	// Table is the exact base-table name in the live PostgreSQL schema.
	Table string
	// OwnerPackage names the Go package that owns writes to Table.
	OwnerPackage string
	// Plane is the architecture plane Table belongs to (platform-plane-model.md).
	Plane string
	// DataRole classifies Table's role within the data plane: LEDGER,
	// PROJECTION, OUTBOX, REGISTRY or CONTROL.
	DataRole string
	// TenantScopingColumn is the column a per-tenant query filters on, or
	// "" when Table is not tenant scoped.
	TenantScopingColumn string
	// AppendOnly reports whether Table's rows are ever mutated after insert.
	AppendOnly bool
	// RetentionClass is PERMANENT, OPERATIONAL or REBUILDABLE.
	RetentionClass string
	// EncryptionClass is PLATFORM_MANAGED or FIELD_LEVEL.
	EncryptionClass string
	// RebuildSource names the append-only table Table can be fully
	// reconstructed from, or "" when Table has no such source (it is
	// PERMANENT, or it is itself the source).
	RebuildSource string
	// Partitions lists Table's own physical partitions, when it has any.
	Partitions []string
}

// DispositionRegistry is the parsed, in-memory form of STORE-001's registry
// (or of DefaultDispositionRegistry, when the published file is absent).
type DispositionRegistry struct {
	Version int
	Tables  []StoreDisposition
}

// Table returns the disposition registered for name, if any.
func (r DispositionRegistry) Table(name string) (StoreDisposition, bool) {
	for _, t := range r.Tables {
		if t.Table == name {
			return t, true
		}
	}
	return StoreDisposition{}, false
}

// LoadDispositionRegistry reads the STORE-001 registry through its sole YAML
// parser, storagedisposition. Health projects that typed registry onto the
// narrower facts a Probe needs rather than becoming a second parser or an
// authority for the physical-storage contract.
func LoadDispositionRegistry(path string) (DispositionRegistry, error) {
	source, err := storagedisposition.Load(path)
	if err != nil {
		return DispositionRegistry{}, fmt.Errorf("health: load disposition registry %s: %w", path, err)
	}

	out := DispositionRegistry{Version: source.Version, Tables: make([]StoreDisposition, 0, len(source.Tables))}
	for _, t := range source.Tables {
		sd := StoreDisposition{
			Table:           t.Table,
			OwnerPackage:    t.OwnerPackage,
			Plane:           t.Plane,
			DataRole:        t.DataRole,
			AppendOnly:      t.AppendOnly,
			RetentionClass:  t.RetentionClass,
			EncryptionClass: t.EncryptionClass,
			Partitions:      append([]string(nil), t.Partitions...),
		}
		if t.TenantScopingColumn != nil {
			sd.TenantScopingColumn = *t.TenantScopingColumn
		}
		if t.RebuildSource != nil {
			sd.RebuildSource = *t.RebuildSource
		}
		out.Tables = append(out.Tables, sd)
	}
	return out, nil
}

// LoadOrDefaultDispositionRegistry loads path -- STORE-001's own published
// registry -- when it is present on disk, and reports ok=true. STORE-001 is
// owned by a separate lane; in a checkout where path does not exist yet,
// this falls back to DefaultDispositionRegistry and reports ok=false, so a
// caller can log the fallback rather than mistake it for the published
// registry. Any other read/parse failure (a present but malformed file) is
// returned as an error rather than silently falling back, since a malformed
// published registry is a real defect, not an absent one.
func LoadOrDefaultDispositionRegistry(path string) (registry DispositionRegistry, ok bool, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return DefaultDispositionRegistry(), false, nil
		}
		return DispositionRegistry{}, false, fmt.Errorf("health: stat disposition registry %s: %w", path, statErr)
	}
	reg, err := LoadDispositionRegistry(path)
	if err != nil {
		return DispositionRegistry{}, false, err
	}
	return reg, true, nil
}

// DefaultDispositionRegistry is this package's own local fixture -- it is
// NOT STORE-001's source of truth -- covering just the tables Probe knows
// how to evaluate on all three dimensions (ledger_event, projection_checkpoint,
// outbox) plus stream_head's registry role, for a checkout where
// definitions/storage/storage-disposition.yaml has not landed yet. See
// LoadOrDefaultDispositionRegistry, which reports ok=false whenever this
// fixture is the one actually in use.
func DefaultDispositionRegistry() DispositionRegistry {
	return DispositionRegistry{
		Version: 0,
		Tables: []StoreDisposition{
			{
				Table: "ledger_event", OwnerPackage: "internal/data/ledger", Plane: "DATA", DataRole: "LEDGER",
				TenantScopingColumn: "tenant_id", AppendOnly: true, RetentionClass: "PERMANENT", EncryptionClass: "PLATFORM_MANAGED",
			},
			{
				Table: "stream_head", OwnerPackage: "internal/data/ledger", Plane: "DATA", DataRole: "LEDGER",
				TenantScopingColumn: "tenant_id", AppendOnly: false, RetentionClass: "OPERATIONAL", EncryptionClass: "PLATFORM_MANAGED",
				RebuildSource: "ledger_event",
			},
			{
				Table: "projection_checkpoint", OwnerPackage: "internal/data/projection", Plane: "DATA", DataRole: "PROJECTION",
				TenantScopingColumn: "tenant_id", AppendOnly: false, RetentionClass: "REBUILDABLE", EncryptionClass: "PLATFORM_MANAGED",
				RebuildSource: "ledger_event",
			},
			{
				Table: "outbox", OwnerPackage: "internal/data/outbox", Plane: "DATA", DataRole: "OUTBOX",
				TenantScopingColumn: "tenant_id", AppendOnly: false, RetentionClass: "REBUILDABLE", EncryptionClass: "PLATFORM_MANAGED",
				RebuildSource: "ledger_event",
			},
		},
	}
}
