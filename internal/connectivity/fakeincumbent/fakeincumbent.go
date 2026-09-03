// Package fakeincumbent is an in-memory incumbent HRIS that satisfies
// [connectivity.Connector].
//
// Semantic owner: connectivity. Phase: P1A.
//
// It exists so that the read-only claim of P1A can be tested rather than
// asserted. The seed data is a copy of the worker and pay-band fixture shapes
// the domain packages use, plus a position catalogue derived from them, so an
// observation run here produces records a mapping profile will recognize.
//
// Three things make it useful as a test double rather than a stub:
//
//   - It is deterministic. Ordering, snapshot identity, page boundaries and
//     content digests are functions of the seed data alone. No wall clock, no
//     map iteration order, no randomness.
//   - It has a call log. Every call is recorded with its operation, object,
//     cursor and outcome, so a test can assert exactly what was asked of the
//     external system - and that nothing mutating was.
//   - Faults are injectable. Transient failures, permission denials, cursor
//     expiry, partial pages and schema drift between versions can be scheduled
//     on specific call ordinals, which is how restart, resume and drift
//     handling get tested without a real provider misbehaving on cue.
package fakeincumbent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

//go:embed testdata/workers.json
var workersJSON []byte

//go:embed testdata/positions.json
var positionsJSON []byte

//go:embed testdata/bands.json
var bandsJSON []byte

// Operation names in the call log. They are all read-only; the log has no
// vocabulary for a write because the connector has no method that could
// perform one.
const (
	OpSchemaVersion = "SCHEMA_VERSION"
	OpSnapshot      = "SNAPSHOT"
	OpRead          = "READ"
)

// readOnlyOps is the closed set of operations this incumbent can perform. A
// call log entry outside it would be a defect, and [Incumbent.MutatingCalls]
// counts exactly that.
var readOnlyOps = map[string]bool{
	OpSchemaVersion: true,
	OpSnapshot:      true,
	OpRead:          true,
}

// SchemaVersion identifiers. Version 2 is the drifted shape: it renames
// legal_name to full_legal_name and adds a field, which is the smallest change
// that is both breaking for a mapping and invisible to a naive reader.
const (
	SchemaWorkerV1       = "workday.worker/2026.1"
	SchemaWorkerV2       = "workday.worker/2026.2"
	SchemaPositionV1     = "workday.position/2026.1"
	SchemaPositionV2     = "workday.position/2026.2"
	SchemaCompensationV1 = "workday.compensation/2026.1"
	SchemaCompensationV2 = "workday.compensation/2026.2"
)

// Faults schedules provider misbehaviour. Every field is opt-in; the zero
// value is a well-behaved incumbent.
type Faults struct {
	// CredentialInvalid fails every call with a credential error.
	CredentialInvalid bool
	// PermissionDenied denies these objects entirely.
	PermissionDenied []connectivity.ObjectKind
	// TransientOnReads fires a transient failure on these 1-based read
	// ordinals, counted across all objects.
	TransientOnReads []int
	// PartialOnReads returns fewer records than requested on these 1-based
	// read ordinals, without ending the traversal.
	PartialOnReads []int
	// ExpireCursorsFromPage rejects any cursor at or beyond this page number.
	// Zero disables cursor expiry.
	ExpireCursorsFromPage uint64
	// DriftAfterReads switches every object to its version 2 schema once this
	// many reads have been served. Zero disables drift.
	DriftAfterReads int
}

// Call is one recorded interaction with the incumbent.
type Call struct {
	// Sequence is the 1-based call ordinal across all operations.
	Sequence int
	// Op is one of the Op* constants.
	Op string
	// Object is the record family addressed, if any.
	Object connectivity.ObjectKind
	// Cursor is the incoming cursor token, empty at the start of a traversal.
	Cursor string
	// Limit is the requested page size, zero for non-read operations.
	Limit int
	// Records is how many records the call returned.
	Records int
	// Class is the error class the call produced, empty on success.
	Class connectivity.Class
}

// Options configures a fake incumbent.
type Options struct {
	// Descriptor identifies the connector version and external system. Its
	// zero value is filled in with a usable default.
	Descriptor connectivity.Descriptor
	// Bounds is the read envelope. Its zero value is filled in with a default
	// that pages the seed data more than once.
	Bounds connectivity.Bounds
	// Now supplies retrieval times. It defaults to a deterministic clock that
	// advances one second per call, so a page's retrieval time is a function
	// of how many calls preceded it rather than of when the test ran.
	Now func() time.Time
}

// DefaultDescriptor is the descriptor used when Options leaves it zero.
func DefaultDescriptor() connectivity.Descriptor {
	return connectivity.Descriptor{
		ConnectorID:  "fake.incumbent.hris",
		Version:      connectivity.Version{Major: 1, Minor: 0, Patch: 0},
		ConnectionID: "conn-fake-incumbent",
		SourceRef:    "fakeincumbent://harborcare/prod",
		AuthorityRef: "authority.incumbent.hris",
	}
}

// DefaultBounds is the read envelope used when Options leaves it zero.
func DefaultBounds() connectivity.Bounds {
	return connectivity.Bounds{
		MaxPageSize:        2,
		MaxPagesPerRun:     64,
		MaxRecordsPerRun:   1024,
		MaxRecordBytes:     8192,
		MinRequestInterval: 0,
	}
}

// DefaultDefinition is a publishable connector definition describing this
// incumbent. Tests that need a registry entry use it rather than restating the
// same twenty fields.
func DefaultDefinition() connectivity.ConnectorDefinition {
	return connectivity.ConnectorDefinition{
		ConnectorID:  DefaultDescriptor().ConnectorID,
		Vendor:       "HCM Next",
		Product:      "Fake Incumbent HRIS",
		Version:      DefaultDescriptor().Version,
		Maturity:     connectivity.MaturityPreview,
		Objects:      connectivity.ObjectKinds(),
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		AuthModes:    []connectivity.AuthMode{connectivity.AuthOAuth2ClientCredentials},
		ReadModes: []connectivity.ReadMode{
			connectivity.ReadFull, connectivity.ReadIncremental, connectivity.ReadDelta,
		},
		// Explicitly empty, not absent: this connector cannot write and cannot
		// subscribe, and says so.
		WriteModes: []string{},
		EventModes: []string{},
		SchemaRefs: []connectivity.SchemaRef{
			{Object: connectivity.ObjectWorker, SchemaID: "workday.worker", SchemaVer: "2026.1",
				Descriptor: SchemaWorkerV1},
			{Object: connectivity.ObjectPosition, SchemaID: "workday.position", SchemaVer: "2026.1",
				Descriptor: SchemaPositionV1},
			{Object: connectivity.ObjectCompensation, SchemaID: "workday.compensation", SchemaVer: "2026.1",
				Descriptor: SchemaCompensationV1},
		},
		Bounds: DefaultBounds(),
		Pagination: connectivity.PaginationContract{
			Style:               "KEYSET",
			SortKeyField:        "sort_key",
			TieBreakField:       "external_id",
			StableUnderSnapshot: true,
			TokenTTL:            15 * time.Minute,
		},
		Rate: connectivity.RateContract{
			RequestsPerMinute: 600, ConcurrentReads: 4, BurstRequests: 20,
		},
		Idempotency: connectivity.IdempotencyContract{
			KeyField: "Idempotency-Key", RetentionWindow: 24 * time.Hour, ReadsAreIdempotent: true,
		},
		Observation: connectivity.ObservationContract{
			WatermarkField: "recorded_at", FreshnessBudget: 24 * time.Hour, SupportsCompleteness: true,
		},
		Reconciliation: connectivity.ReconciliationContract{
			KeyFields:         []string{"external_id"},
			ComparableFields:  []string{"job_code", "grade", "pay_zone"},
			SupportsPointRead: true,
		},
		Health: connectivity.HealthContract{
			ProbeObject: connectivity.ObjectWorker, Interval: time.Minute, DegradedAfterFailures: 3,
		},
	}
}

// Incumbent is the in-memory HRIS. It is safe for concurrent use.
type Incumbent struct {
	descriptor connectivity.Descriptor
	bounds     connectivity.Bounds
	now        func() time.Time

	mu        sync.Mutex
	records   map[connectivity.ObjectKind][]connectivity.Record
	faults    Faults
	calls     []Call
	readCount int
	drifted   bool
}

// New builds an incumbent seeded from the embedded fixtures.
func New(opts Options) (*Incumbent, error) {
	descriptor := opts.Descriptor
	if descriptor.ConnectorID == "" {
		descriptor = DefaultDescriptor()
	}
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	bounds := opts.Bounds
	if bounds == (connectivity.Bounds{}) {
		bounds = DefaultBounds()
	}
	if err := bounds.Validate(); err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = deterministicClock()
	}

	records, err := seed()
	if err != nil {
		return nil, err
	}
	return &Incumbent{
		descriptor: descriptor,
		bounds:     bounds,
		now:        now,
		records:    records,
	}, nil
}

// MustNew is New for a test that has nothing useful to do with an error.
func MustNew(opts Options) *Incumbent {
	inc, err := New(opts)
	if err != nil {
		panic(err)
	}
	return inc
}

// deterministicClock returns a clock starting at a fixed instant and advancing
// one second per call, so retrieval times are reproducible.
func deterministicClock() func() time.Time {
	var mu sync.Mutex
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var tick int64
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := base.Add(time.Duration(tick) * time.Second)
		tick++
		return t
	}
}

// Descriptor implements [connectivity.Connector].
func (i *Incumbent) Descriptor() connectivity.Descriptor { return i.descriptor }

// Bounds implements [connectivity.Connector].
func (i *Incumbent) Bounds() connectivity.Bounds { return i.bounds }

// Capabilities implements [connectivity.Connector]. It publishes reads only.
func (i *Incumbent) Capabilities() []connectivity.Capability {
	return connectivity.ReadCapabilities(connectivity.ObjectKinds()...)
}

// InjectFaults schedules provider misbehaviour, replacing any previous plan.
func (i *Incumbent) InjectFaults(f Faults) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.faults = f
}

// ClearFaults removes every scheduled fault and un-drifts the schema.
func (i *Incumbent) ClearFaults() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.faults = Faults{}
	i.drifted = false
}

// Calls returns the recorded call log in order.
func (i *Incumbent) Calls() []Call {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]Call(nil), i.calls...)
}

// ResetCalls clears the call log without touching the data or the fault plan.
func (i *Incumbent) ResetCalls() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls = nil
}

// MutatingCalls counts recorded calls outside the read-only operation set. It
// is the evidence behind "this run performed zero external writes": the count
// is taken from the log the incumbent kept, not from the caller's intent.
func (i *Incumbent) MutatingCalls() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	n := 0
	for _, c := range i.calls {
		if !readOnlyOps[c.Op] {
			n++
		}
	}
	return n
}

// Fingerprint digests the entire stored dataset. A test takes it before and
// after an observation run: an equal fingerprint is proof the run changed
// nothing, independent of what the call log claims.
func (i *Incumbent) Fingerprint() (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	w := canonicalbytes.New("hcmnext.connectivity.fakeincumbent.Store", 1)
	for _, object := range connectivity.ObjectKinds() {
		recs := i.records[object]
		w.String("object", string(object))
		w.Count("record", len(recs))
		for _, r := range recs {
			w.String("external_id", r.ExternalID).
				String("sort_key", r.SortKey).
				String("source_version", r.SourceVersion).
				Int("observed_at_unix", r.ObservedAt.UTC().Unix())
			for _, k := range r.FieldKeys() {
				w.String("field."+k, r.Fields[k])
			}
		}
	}
	return w.Digest()
}

// RecordCount reports how many records the incumbent holds for an object.
func (i *Incumbent) RecordCount(object connectivity.ObjectKind) int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.records[object])
}

// SchemaVersion implements [connectivity.Connector].
func (i *Incumbent) SchemaVersion(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	const op = "fakeincumbent.SchemaVersion"
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", i.record(OpSchemaVersion, object, "", 0, 0,
			connectivity.Fail(op, connectivity.ErrTransient, "context: %v", err))
	}
	if err := i.gate(op, object); err != nil {
		return "", i.record(OpSchemaVersion, object, "", 0, 0, err)
	}
	version := i.schemaVersionLocked(object)
	i.record(OpSchemaVersion, object, "", 0, 0, nil)
	return version, nil
}

// Snapshot implements [connectivity.Connector]. The id is derived from the
// object's current content and schema version, so it is stable while nothing
// changes and necessarily different once the schema drifts.
func (i *Incumbent) Snapshot(ctx context.Context, object connectivity.ObjectKind) (string, error) {
	const op = "fakeincumbent.Snapshot"
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", i.record(OpSnapshot, object, "", 0, 0,
			connectivity.Fail(op, connectivity.ErrTransient, "context: %v", err))
	}
	if err := i.gate(op, object); err != nil {
		return "", i.record(OpSnapshot, object, "", 0, 0, err)
	}
	snapshot, err := i.snapshotIDLocked(object)
	if err != nil {
		return "", i.record(OpSnapshot, object, "", 0, 0, err)
	}
	i.record(OpSnapshot, object, "", 0, 0, nil)
	return snapshot, nil
}

// Read implements [connectivity.Connector]. It returns one bounded page in
// (sort key, external id) order, honouring the scheduled fault plan.
func (i *Incumbent) Read(ctx context.Context, req connectivity.ReadRequest) (connectivity.Page, error) {
	const op = "fakeincumbent.Read"
	i.mu.Lock()
	defer i.mu.Unlock()

	token := req.Cursor.MustToken()
	fail := func(err error) (connectivity.Page, error) {
		return connectivity.Page{}, i.record(OpRead, req.Object, token, req.Limit, 0, err)
	}

	if err := ctx.Err(); err != nil {
		return fail(connectivity.Fail(op, connectivity.ErrTransient, "context: %v", err))
	}
	if err := req.Validate(i.bounds); err != nil {
		return fail(err)
	}
	if err := i.gate(op, req.Object); err != nil {
		return fail(err)
	}

	i.readCount++
	ordinal := i.readCount
	if containsInt(i.faults.TransientOnReads, ordinal) {
		return fail(connectivity.Fail(op, connectivity.ErrTransient,
			"provider is throttling read %d", ordinal))
	}
	if i.faults.ExpireCursorsFromPage > 0 && req.Cursor.Page >= i.faults.ExpireCursorsFromPage {
		return fail(connectivity.Fail(op, connectivity.ErrCursor,
			"continuation token for page %d has expired", req.Cursor.Page))
	}
	if i.faults.DriftAfterReads > 0 && ordinal > i.faults.DriftAfterReads {
		i.drifted = true
	}

	snapshot, err := i.snapshotIDLocked(req.Object)
	if err != nil {
		return fail(err)
	}
	if !req.Cursor.IsStart() || req.Cursor.SnapshotID != "" {
		if req.Cursor.SnapshotID != snapshot {
			if schemaOf(req.Cursor.SnapshotID) != schemaOf(snapshot) {
				return fail(connectivity.Fail(op, connectivity.ErrSchema,
					"schema drifted mid-traversal: cursor was minted under %q, source now serves %q",
					schemaOf(req.Cursor.SnapshotID), schemaOf(snapshot)))
			}
			return fail(connectivity.Fail(op, connectivity.ErrCursor,
				"cursor was minted against snapshot %q, source now serves %q",
				req.Cursor.SnapshotID, snapshot))
		}
	}

	all := i.viewLocked(req.Object)
	start := 0
	if !req.Cursor.IsStart() {
		start = len(all)
		for idx, rec := range all {
			if follows(req.Cursor.LastSortKey, req.Cursor.LastExternalID, rec.SortKey, rec.ExternalID) {
				start = idx
				break
			}
		}
	}
	if req.Mode == connectivity.ReadIncremental {
		filtered := make([]connectivity.Record, 0, len(all)-start)
		for _, rec := range all[start:] {
			if !rec.ObservedAt.Before(req.Since) {
				filtered = append(filtered, rec)
			}
		}
		all, start = filtered, 0
	}

	limit := req.Limit
	if containsInt(i.faults.PartialOnReads, ordinal) && limit > 1 {
		limit = 1
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	page := connectivity.Page{
		Object:        req.Object,
		SchemaVersion: i.schemaVersionLocked(req.Object),
		SnapshotID:    snapshot,
		Records:       append([]connectivity.Record(nil), all[start:end]...),
		Complete:      end >= len(all),
		Partial:       containsInt(i.faults.PartialOnReads, ordinal),
		RetrievedAt:   i.now().UTC(),
	}
	if len(page.Records) > 0 {
		last := page.Records[len(page.Records)-1]
		page.Watermark = last.ObservedAt.UTC()
		page.NextCursor = connectivity.Cursor{
			SnapshotID:     snapshot,
			LastSortKey:    last.SortKey,
			LastExternalID: last.ExternalID,
			Page:           req.Cursor.Page + 1,
		}
	} else {
		page.Watermark = page.RetrievedAt
		page.NextCursor = req.Cursor
		page.NextCursor.SnapshotID = snapshot
	}
	if page.Complete {
		page.NextCursor = connectivity.Cursor{}
	}

	i.record(OpRead, req.Object, token, req.Limit, len(page.Records), nil)
	return page, nil
}

// gate applies the connection-wide faults that precede any object work.
func (i *Incumbent) gate(op string, object connectivity.ObjectKind) error {
	if i.faults.CredentialInvalid {
		return connectivity.Fail(op, connectivity.ErrCredential,
			"provider rejected the presented credential")
	}
	if !object.Valid() {
		return connectivity.Fail(op, connectivity.ErrUnsupported,
			"object %q is not served by this incumbent", string(object))
	}
	for _, denied := range i.faults.PermissionDenied {
		if denied == object {
			return connectivity.Fail(op, connectivity.ErrPermission,
				"credential lacks the scope required to read %s", object)
		}
	}
	return nil
}

// record appends a call log entry and returns err unchanged, so a caller can
// write `return i.record(...)` at every failure site and never forget to log.
func (i *Incumbent) record(op string, object connectivity.ObjectKind, cursor string, limit, records int, err error) error {
	entry := Call{
		Sequence: len(i.calls) + 1,
		Op:       op,
		Object:   object,
		Cursor:   cursor,
		Limit:    limit,
		Records:  records,
	}
	if err != nil {
		if class, ok := connectivity.ClassOf(err); ok {
			entry.Class = class
		} else {
			entry.Class = connectivity.ClassInvalid
		}
	}
	i.calls = append(i.calls, entry)
	return err
}

func (i *Incumbent) schemaVersionLocked(object connectivity.ObjectKind) string {
	switch object {
	case connectivity.ObjectWorker:
		if i.drifted {
			return SchemaWorkerV2
		}
		return SchemaWorkerV1
	case connectivity.ObjectPosition:
		if i.drifted {
			return SchemaPositionV2
		}
		return SchemaPositionV1
	case connectivity.ObjectCompensation:
		if i.drifted {
			return SchemaCompensationV2
		}
		return SchemaCompensationV1
	default:
		return ""
	}
}

// snapshotIDLocked derives the snapshot id as "<schema version>#<digest>".
// Both halves matter: the digest changes when data changes, and the schema
// half is what lets a stale cursor be reported as drift rather than as an
// ordinary expired token.
func (i *Incumbent) snapshotIDLocked(object connectivity.ObjectKind) (string, error) {
	const op = "fakeincumbent.Snapshot"
	w := canonicalbytes.New("hcmnext.connectivity.fakeincumbent.Snapshot", 1).
		String("object", string(object)).
		String("schema_version", i.schemaVersionLocked(object))
	for _, r := range i.viewLocked(object) {
		w.String("external_id", r.ExternalID).String("source_version", r.SourceVersion)
	}
	digest, err := w.Digest()
	if err != nil {
		return "", connectivity.Fail(op, connectivity.ErrInvalid, "digest snapshot: %v", err)
	}
	hex := strings.TrimPrefix(digest, canonicalbytes.DigestAlgorithm+":")
	return i.schemaVersionLocked(object) + "#" + hex[:12], nil
}

// schemaOf extracts the schema-version half of a snapshot id.
func schemaOf(snapshotID string) string {
	schema, _, _ := strings.Cut(snapshotID, "#")
	return schema
}

// viewLocked returns the records for an object under the current schema
// version. Drift is applied as a projection over the same seed data rather
// than as a second copy, so the two versions cannot fall out of sync.
func (i *Incumbent) viewLocked(object connectivity.ObjectKind) []connectivity.Record {
	base := i.records[object]
	if !i.drifted {
		return base
	}
	out := make([]connectivity.Record, len(base))
	for idx, r := range base {
		fields := make(map[string]string, len(r.Fields)+1)
		for k, v := range r.Fields {
			if k == "legal_name" {
				fields["full_legal_name"] = v
				continue
			}
			fields[k] = v
		}
		fields["schema_generation"] = "2"
		out[idx] = connectivity.Record{
			ExternalID:    r.ExternalID,
			SortKey:       r.SortKey,
			SourceVersion: r.SourceVersion + "+drift",
			ObservedAt:    r.ObservedAt,
			Fields:        fields,
		}
	}
	return out
}

// follows reports whether (sortB, idB) is strictly after (sortA, idA).
func follows(sortA, idA, sortB, idB string) bool {
	if sortA != sortB {
		return sortA < sortB
	}
	return idA < idB
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// seed decodes the embedded fixtures into sorted record slices.
func seed() (map[connectivity.ObjectKind][]connectivity.Record, error) {
	workers, err := seedWorkers()
	if err != nil {
		return nil, err
	}
	positions, err := seedPositions()
	if err != nil {
		return nil, err
	}
	compensation, err := seedCompensation()
	if err != nil {
		return nil, err
	}
	return map[connectivity.ObjectKind][]connectivity.Record{
		connectivity.ObjectWorker:       workers,
		connectivity.ObjectPosition:     positions,
		connectivity.ObjectCompensation: compensation,
	}, nil
}

type workerFile struct {
	Workers []struct {
		ID              string `json:"id"`
		WorkerNumber    string `json:"worker_number"`
		LifecycleStatus string `json:"lifecycle_status"`
		LegalName       string `json:"legal_name"`
		PreferredName   string `json:"preferred_name"`
		EmploymentID    string `json:"employment_id"`
		WorkerType      string `json:"worker_type"`
		HireDate        string `json:"hire_date"`
		JobCode         string `json:"job_code"`
		Grade           string `json:"grade"`
		OrgUnit         string `json:"org_unit"`
		PositionID      string `json:"position_id"`
		Location        string `json:"location"`
		PayZone         string `json:"pay_zone"`
		FTE             string `json:"fte"`
		EffectiveFrom   string `json:"effective_from"`
		RevisionSeq     int64  `json:"revision_sequence"`
		RecordedAt      string `json:"recorded_at"`
	} `json:"workers"`
}

func seedWorkers() ([]connectivity.Record, error) {
	var file workerFile
	if err := json.Unmarshal(workersJSON, &file); err != nil {
		return nil, fmt.Errorf("fakeincumbent: decode workers fixture: %w", err)
	}
	records := make([]connectivity.Record, 0, len(file.Workers))
	for _, w := range file.Workers {
		observed, err := time.Parse(time.RFC3339, w.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("fakeincumbent: worker %s recorded_at: %w", w.WorkerNumber, err)
		}
		records = append(records, connectivity.Record{
			ExternalID:    w.ID,
			SortKey:       w.WorkerNumber,
			SourceVersion: "r" + strconv.FormatInt(w.RevisionSeq, 10),
			ObservedAt:    observed.UTC(),
			Fields: map[string]string{
				"worker_number":    w.WorkerNumber,
				"lifecycle_status": w.LifecycleStatus,
				"legal_name":       w.LegalName,
				"preferred_name":   w.PreferredName,
				"employment_id":    w.EmploymentID,
				"worker_type":      w.WorkerType,
				"hire_date":        w.HireDate,
				"job_code":         w.JobCode,
				"grade":            w.Grade,
				"org_unit":         w.OrgUnit,
				"position_id":      w.PositionID,
				"location":         w.Location,
				"pay_zone":         w.PayZone,
				"fte":              w.FTE,
				"effective_from":   w.EffectiveFrom,
			},
		})
	}
	return sortRecords(records), nil
}

type positionFile struct {
	Positions []struct {
		ID            string `json:"id"`
		Version       string `json:"version"`
		JobCode       string `json:"job_code"`
		Grade         string `json:"grade"`
		OrgUnit       string `json:"org_unit"`
		Location      string `json:"location"`
		PayZone       string `json:"pay_zone"`
		Status        string `json:"status"`
		Headcount     string `json:"headcount"`
		Incumbent     string `json:"incumbent_worker_number"`
		EffectiveFrom string `json:"effective_from"`
		RecordedAt    string `json:"recorded_at"`
	} `json:"positions"`
}

func seedPositions() ([]connectivity.Record, error) {
	var file positionFile
	if err := json.Unmarshal(positionsJSON, &file); err != nil {
		return nil, fmt.Errorf("fakeincumbent: decode positions fixture: %w", err)
	}
	records := make([]connectivity.Record, 0, len(file.Positions))
	for _, p := range file.Positions {
		observed, err := time.Parse(time.RFC3339, p.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("fakeincumbent: position %s recorded_at: %w", p.ID, err)
		}
		records = append(records, connectivity.Record{
			ExternalID:    p.ID,
			SortKey:       p.ID,
			SourceVersion: p.Version,
			ObservedAt:    observed.UTC(),
			Fields: map[string]string{
				"job_code":                p.JobCode,
				"grade":                   p.Grade,
				"org_unit":                p.OrgUnit,
				"location":                p.Location,
				"pay_zone":                p.PayZone,
				"status":                  p.Status,
				"headcount":               p.Headcount,
				"incumbent_worker_number": p.Incumbent,
				"effective_from":          p.EffectiveFrom,
			},
		})
	}
	return sortRecords(records), nil
}

type bandFile struct {
	RecordedAt string `json:"recorded_at"`
	Bands      []struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		JobCode  string `json:"job_code"`
		Grade    string `json:"grade"`
		PayZone  string `json:"pay_zone"`
		Currency string `json:"currency"`
		Minimum  string `json:"minimum"`
		Midpoint string `json:"midpoint"`
		Maximum  string `json:"maximum"`
		Blocking bool   `json:"blocking"`
	} `json:"bands"`
}

func seedCompensation() ([]connectivity.Record, error) {
	var file bandFile
	if err := json.Unmarshal(bandsJSON, &file); err != nil {
		return nil, fmt.Errorf("fakeincumbent: decode bands fixture: %w", err)
	}
	observed, err := time.Parse(time.RFC3339, file.RecordedAt)
	if err != nil {
		return nil, fmt.Errorf("fakeincumbent: bands recorded_at: %w", err)
	}
	records := make([]connectivity.Record, 0, len(file.Bands))
	for _, b := range file.Bands {
		records = append(records, connectivity.Record{
			ExternalID:    b.ID,
			SortKey:       b.ID,
			SourceVersion: b.Version,
			ObservedAt:    observed.UTC(),
			Fields: map[string]string{
				"job_code": b.JobCode,
				"grade":    b.Grade,
				"pay_zone": b.PayZone,
				"currency": b.Currency,
				"minimum":  b.Minimum,
				"midpoint": b.Midpoint,
				"maximum":  b.Maximum,
				"blocking": strconv.FormatBool(b.Blocking),
			},
		})
	}
	return sortRecords(records), nil
}

func sortRecords(records []connectivity.Record) []connectivity.Record {
	sort.Slice(records, func(a, b int) bool {
		return follows(records[a].SortKey, records[a].ExternalID, records[b].SortKey, records[b].ExternalID)
	})
	return records
}
