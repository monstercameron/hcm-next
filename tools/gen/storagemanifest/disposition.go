package storagemanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// DispositionKind is the physical storage classification DB-002 assigns to
// one registered item.
type DispositionKind string

// Disposition kinds.
const (
	DispositionTable       DispositionKind = "TABLE"
	DispositionEvent       DispositionKind = "EVENT"
	DispositionArtifact    DispositionKind = "ARTIFACT"
	DispositionProjection  DispositionKind = "PROJECTION"
	DispositionValueObject DispositionKind = "VALUE_OBJECT"

	// DispositionMismatch marks a registered item with no physical
	// counterpart in the scanned migrations yet. It is a legitimate,
	// reportable answer — never a generation failure — because DB-002's
	// REFACTOR clause requires reporting mismatches rather than inventing a
	// migration.
	DispositionMismatch DispositionKind = "MISMATCH"
)

// physicalHint is this generator's declared belief about where one entity's
// authoritative facts should physically live. It is hand-maintained, not
// inferred from name matching, because a name-matching heuristic would
// silently "verify" a coincidental match; [BuildDispositionManifest] checks
// every hint against the real [MigrationInventory] before trusting it.
type physicalHint struct {
	kind       DispositionKind
	table      string // set for TABLE/ARTIFACT/PROJECTION hints
	streamKind string // set for EVENT hints
}

// entityHints declares the intended physical disposition of every
// AGGREGATE_ROOT, EVIDENCE and READ_MODEL entity in the compiled catalog.
// CHILD-class entities are never listed here: their disposition always
// derives from their owner root (see [classifyChild]).
var entityHints = map[string]physicalHint{
	"person":                    {kind: DispositionEvent, streamKind: "PERSON"},
	"position_occupancy":        {kind: DispositionEvent, streamKind: "POSITION_OCCUPANCY"},
	"worker":                    {kind: DispositionEvent, streamKind: "WORKER"},
	"employment":                {kind: DispositionEvent, streamKind: "EMPLOYMENT"},
	"assignment":                {kind: DispositionEvent, streamKind: "ASSIGNMENT"},
	"position":                  {kind: DispositionEvent, streamKind: "POSITION"},
	"job":                       {kind: DispositionEvent, streamKind: "JOB"},
	"compensation_grade":        {kind: DispositionEvent, streamKind: "COMPENSATION_GRADE"},
	"compensation_package":      {kind: DispositionEvent, streamKind: "COMPENSATION_PACKAGE"},
	"organization_unit":         {kind: DispositionEvent, streamKind: "ORGANIZATION_UNIT"},
	"organization_relationship": {kind: DispositionEvent, streamKind: "ORGANIZATION_RELATIONSHIP"},
	"legal_entity":              {kind: DispositionEvent, streamKind: "LEGAL_ENTITY"},
	"intent_instance":           {kind: DispositionTable, table: "intent_instance"},
	"proposal_revision":         {kind: DispositionTable, table: "proposal_revision"},
	"transaction_plan":          {kind: DispositionEvent, streamKind: "TRANSACTION"},
	"evidence_record":           {kind: DispositionArtifact, table: "evidence_artifact"},
	"connector_operation":       {kind: DispositionEvent, streamKind: "INTEGRATION_OPERATION"},
	"budget_reservation":        {kind: DispositionEvent, streamKind: "BUDGET_RESERVATION"},
	"repair_plan":               {kind: DispositionEvent, streamKind: "REPAIR_PLAN"},
	"approval_binding":          {kind: DispositionArtifact, table: "approval_binding"},
	"execution_binding":         {kind: DispositionArtifact, table: "execution_binding"},
	"observation":               {kind: DispositionTable, table: "external_observation"},
	"worker_summary":            {kind: DispositionProjection, table: "projection_checkpoint"},
}

// EntityDisposition is one row of the DB-002 manifest.
type EntityDisposition struct {
	EntityRef      string
	Key            string
	Owner          string
	Class          string
	Disposition    DispositionKind
	Target         string
	MismatchDetail string
}

// DispositionManifest is the full DB-002 output.
type DispositionManifest struct {
	GeneratedBy    string
	RegistryDigest string
	MigrationFiles []string
	Entities       []EntityDisposition
}

// Digest returns a stable sha256 digest of the manifest's own content.
func (m DispositionManifest) Digest() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	w("REGISTRY", m.RegistryDigest)
	for _, f := range m.MigrationFiles {
		w("FILE", f)
	}
	for _, e := range m.Entities {
		w("ENTITY", e.EntityRef, e.Key, e.Owner, e.Class, string(e.Disposition), e.Target, e.MismatchDetail)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// BuildDispositionManifest generates the DB-002 manifest: exactly one
// disposition, owner and schema target per registered entity, resolved
// against inv. Every entity appears exactly once; a hint with no physical
// counterpart in inv becomes a MISMATCH rather than a silent success.
func BuildDispositionManifest(reg *model.Registry, inv MigrationInventory) (DispositionManifest, error) {
	entities := reg.Entities()
	resolved := make(map[string]EntityDisposition, len(entities))

	// Pass 1: every non-CHILD entity, from its declared hint.
	for _, e := range entities {
		if e.Class == model.ClassChild {
			continue
		}
		resolved[e.Key] = classifyRoot(e, inv)
	}
	// Pass 2: CHILD entities derive from their owner root.
	for _, e := range entities {
		if e.Class != model.ClassChild {
			continue
		}
		root, err := reg.OwnerRoot(e.Ref)
		if err != nil {
			resolved[e.Key] = EntityDisposition{
				EntityRef: e.Ref.String(), Key: e.Key, Owner: e.OwnerDomain, Class: string(e.Class),
				Disposition: DispositionMismatch, MismatchDetail: "child resolves to no owner root: " + err.Error(),
			}
			continue
		}
		ownerEntity, err := reg.Entity(root)
		if err != nil {
			return DispositionManifest{}, err
		}
		resolved[e.Key] = classifyChild(e, resolved[ownerEntity.Key])
	}

	out := make([]EntityDisposition, 0, len(entities))
	for _, e := range entities {
		d, ok := resolved[e.Key]
		if !ok {
			return DispositionManifest{}, fmt.Errorf("storagemanifest: %s produced no disposition", e.Ref)
		}
		out = append(out, d)
	}

	return DispositionManifest{
		GeneratedBy:    "tools/gen/storagemanifest (DB-002)",
		RegistryDigest: reg.Digest(),
		MigrationFiles: append([]string(nil), inv.SourceFiles...),
		Entities:       out,
	}, nil
}

func classifyRoot(e model.EntityDefinition, inv MigrationInventory) EntityDisposition {
	base := EntityDisposition{
		EntityRef: e.Ref.String(), Key: e.Key, Owner: e.OwnerDomain, Class: string(e.Class),
	}
	hint, ok := entityHints[e.Key]
	if !ok {
		base.Disposition = DispositionMismatch
		base.MismatchDetail = "no declared physical hint for this entity"
		return base
	}
	switch hint.kind {
	case DispositionEvent:
		if !inv.HasStreamKind(hint.streamKind) {
			base.Disposition = DispositionMismatch
			base.MismatchDetail = fmt.Sprintf(
				"no ledger_stream.stream_kind %q in the migrated schema; needs a migration", hint.streamKind)
			return base
		}
		base.Disposition = DispositionEvent
		base.Target = "ledger_event via ledger_stream.stream_kind=" + hint.streamKind
	case DispositionTable, DispositionArtifact, DispositionProjection:
		if !inv.HasTable(hint.table) {
			base.Disposition = DispositionMismatch
			base.MismatchDetail = fmt.Sprintf(
				"no table %q in the migrated schema; needs a migration", hint.table)
			return base
		}
		base.Disposition = hint.kind
		base.Target = "table " + hint.table
	default:
		base.Disposition = DispositionMismatch
		base.MismatchDetail = "declared hint has no recognized disposition kind"
	}
	return base
}

func classifyChild(e model.EntityDefinition, owner EntityDisposition) EntityDisposition {
	base := EntityDisposition{
		EntityRef: e.Ref.String(), Key: e.Key, Owner: e.OwnerDomain, Class: string(e.Class),
	}
	if owner.Disposition == DispositionMismatch {
		base.Disposition = DispositionMismatch
		base.MismatchDetail = "owner root " + owner.EntityRef + " has no physical disposition: " + owner.MismatchDetail
		return base
	}
	base.Disposition = DispositionValueObject
	base.Target = "embedded in " + owner.Target + " (owner " + owner.EntityRef + ")"
	return base
}
