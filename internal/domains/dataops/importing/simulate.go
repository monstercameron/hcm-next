package importing

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// Simulation errors are deliberately small and matchable by callers.
var (
	ErrSimulationInput    = errors.New("importing: simulation input is invalid")
	ErrSimulationConflict = errors.New("importing: simulation conflict")
)

// DraftWrite is a write which a simulated BusinessIntent would request. It is
// descriptive only: this package has no repository or effect port.
type DraftWrite struct {
	Property string
	Current  string
	Proposed string
}

// BusinessIntentDraft is the ordered, per-row proposal input produced by the
// simulator. Invalid rows are retained as drafts with an error, making the
// preview complete and independently auditable.
type BusinessIntentDraft struct {
	Ordinal         int
	RowID           string
	IntentType      string
	SubjectKind     string
	SubjectID       string
	AuthorityDomain string
	Status          string // CREATE, CHANGE, NO_OP, ERROR, CONFLICT
	Writes          []DraftWrite
	Errors          []ValidationError
}

// ImportSimulation is a signed (content-digested) zero-effect projection of
// an import. Counts and drafts are deterministic for a fixed batch, mapping,
// validation result and current-state snapshot.
type ImportSimulation struct {
	BatchDigest      string
	MappingDigest    string
	Creates          int
	Changes          int
	NoOps            int
	Errors           int
	Conflicts        int
	PlannedWrites    []DraftWrite
	Obligations      []string
	Drafts           []BusinessIntentDraft
	CommittedEffects int
	Digest           string
	Signature        string
	ZeroEffects      bool
}

// SimulationInput supplies the already validated pipeline output and an
// optional read-only baseline. Current is keyed by row identity (RowID), then
// canonical property reference. A missing baseline means CREATE.
type SimulationInput struct {
	Batch           Batch
	Mapping         MappingProfile
	Validation      Result
	Registry        *model.Registry
	Current         map[string]map[string]string
	IntentType      string
	SubjectKind     string
	AuthorityDomain string
	Obligations     []string
}

// SimulateImportRequest is the descriptive name used by API adapters.
type SimulateImportRequest = SimulationInput

// SimulateImport constructs ordered draft BusinessIntents without committing
// anything. It never reads the clock, storage, connectors, or intent runtime.
func SimulateImport(in SimulationInput) (ImportSimulation, error) {
	if err := in.Batch.Validate(); err != nil {
		return ImportSimulation{}, fmt.Errorf("%w: batch: %v", ErrSimulationInput, err)
	}
	if in.Mapping.Digest == "" || in.Validation.Summary.BatchDigest != in.Batch.Digest() || in.Validation.Summary.MappingDigest != in.Mapping.Digest {
		return ImportSimulation{}, fmt.Errorf("%w: pipeline outputs do not match batch and mapping", ErrSimulationInput)
	}
	if in.IntentType == "" {
		in.IntentType = "hcmnext.dataops.import_row"
	}
	if in.SubjectKind == "" {
		in.SubjectKind = "WORKER"
	}
	if in.AuthorityDomain == "" {
		in.AuthorityDomain = "hcmnext"
	}

	out := ImportSimulation{BatchDigest: in.Batch.Digest(), MappingDigest: in.Mapping.Digest, Obligations: append([]string(nil), in.Obligations...), ZeroEffects: true}
	sort.Strings(out.Obligations)
	byRow := make(map[string]RowResult, len(in.Validation.Rows))
	for _, rr := range in.Validation.Rows {
		byRow[rr.RowID] = rr
	}
	for i, row := range in.Batch.Rows() {
		rr := byRow[row.ID()]
		d := BusinessIntentDraft{Ordinal: i, RowID: row.ID(), IntentType: in.IntentType, SubjectKind: in.SubjectKind, SubjectID: row.ID(), AuthorityDomain: in.AuthorityDomain, Status: "CREATE", Errors: append([]ValidationError(nil), rr.Errors...)}
		if len(rr.Errors) != 0 {
			d.Status = "ERROR"
			out.Errors++
			out.Drafts = append(out.Drafts, d)
			continue
		}
		mapped, err := in.Mapping.Apply(in.Batch.Header(), row)
		if err != nil {
			return ImportSimulation{}, fmt.Errorf("%w: apply row %s: %v", ErrSimulationInput, row.ID(), err)
		}
		baseline, exists := in.Current[row.ID()]
		changed := false
		for _, mv := range mapped {
			w := DraftWrite{Property: string(mv.Target), Proposed: mv.Value}
			if exists {
				w.Current = baseline[string(mv.Target)]
				if w.Current != w.Proposed {
					changed = true
				}
			} else {
				changed = true
			}
			d.Writes = append(d.Writes, w)
			out.PlannedWrites = append(out.PlannedWrites, w)
		}
		if exists && !changed {
			d.Status = "NO_OP"
			out.NoOps++
		} else if exists {
			d.Status = "CHANGE"
			out.Changes++
		} else {
			out.Creates++
		}
		out.Drafts = append(out.Drafts, d)
	}
	w := newCanonWriter("hcmnext.dataops.importing.ImportSimulation", 1)
	w.str(out.BatchDigest).str(out.MappingDigest).u64(uint64(out.Creates)).u64(uint64(out.Changes)).u64(uint64(out.NoOps)).u64(uint64(out.Errors)).u64(uint64(out.Conflicts))
	for _, d := range out.Drafts {
		w.u64(uint64(d.Ordinal)).str(d.RowID).str(d.Status)
		for _, x := range d.Writes {
			w.str(x.Property).str(x.Current).str(x.Proposed)
		}
		for _, e := range d.Errors {
			w.str(e.Identity)
		}
	}
	for _, o := range out.Obligations {
		w.str(o)
	}
	out.Digest = w.digestHex()
	out.Signature = out.Digest
	return out, nil
}

// Simulate is a concise alias retained for callers that model pipeline stages
// as functions named after their operation.
func Simulate(in SimulationInput) (ImportSimulation, error) { return SimulateImport(in) }
