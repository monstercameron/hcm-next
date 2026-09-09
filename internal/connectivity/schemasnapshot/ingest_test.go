package schemasnapshot_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

const testTenant = "5e3f1c2b-0000-4000-8000-000000000001"

func testProvider() schemasnapshot.ProviderRef {
	return schemasnapshot.ProviderRef{
		ConnectorID: "workday-hcm", ConnectionID: "harborcare-prod", SourceRef: "workday://harborcare",
	}
}

func baseRequest(raw []byte) schemasnapshot.IngestRequest {
	return schemasnapshot.IngestRequest{
		TenantID:            testTenant,
		Provider:            testProvider(),
		CapturedAt:          time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		DeclaredFormat:      schemasnapshot.FormatJSONSchema,
		Raw:                 raw,
		Classification:      model.ClassInternal,
		RetentionClass:      "integration-schema",
		CreatorPrincipalRef: "system:discovery-worker",
		EvidenceID:          "evidence:discovery-run-1",
		DecidedAt:           time.Date(2026, 9, 1, 12, 5, 0, 0, time.UTC),
	}
}

func newHarness() (*schemasnapshot.MemoryArtifactStore, *schemasnapshot.MemoryStore) {
	return schemasnapshot.NewMemoryArtifactStore(), schemasnapshot.NewMemoryStore()
}

// TestTodo_INTG_004 is the PRIMARY test: an untrusted discovered schema must
// never auto-publish. It enters quarantine, cannot be used as a mapping
// input while quarantined, and only a run of every declared validator can
// move it to a terminal, evidenced verdict.
func TestTodo_INTG_004(t *testing.T) {
	ctx := context.Background()
	artifactStore, store := newHarness()
	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)

	wellFormed := []byte(`{"type":"object","properties":{"id":{"type":"string"}}}`)

	// RED: a snapshot that has not yet been validated must not satisfy
	// RequireAdmitted -- there is no shortcut from "received" to "trusted".
	zeroDigest := strings.Repeat("0", 64)
	quarantined := schemasnapshot.SchemaSnapshot{
		SnapshotID: uuid.New(), TenantID: testTenant, Provider: testProvider(),
		CapturedAt: time.Now(), DeclaredFormat: schemasnapshot.FormatJSONSchema,
		DigestAlgorithm: schemasnapshot.Algorithm,
		CanonicalDigest: zeroDigest,
		ArtifactRef:     zeroDigest,
		ByteSize:        10, State: schemasnapshot.StateQuarantined,
	}
	if err := quarantined.RequireAdmitted(); !errors.Is(err, schemasnapshot.ErrNotAdmitted) {
		t.Fatalf("RequireAdmitted on a quarantined snapshot = %v, want ErrNotAdmitted", err)
	}

	result, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, baseRequest(wellFormed))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !result.SnapshotCreated || !result.ArtifactCreated {
		t.Fatalf("first ingest should create both artifact and snapshot, got %+v", result)
	}
	// GREEN: every declared validator passed, so the snapshot is now
	// ADMITTED, and only now may it satisfy RequireAdmitted.
	if result.Snapshot.State != schemasnapshot.StateAdmitted {
		t.Fatalf("snapshot state = %s, want ADMITTED", result.Snapshot.State)
	}
	if err := result.Snapshot.RequireAdmitted(); err != nil {
		t.Fatalf("RequireAdmitted on an admitted snapshot: %v", err)
	}
	if result.Evidence.Verdict != schemasnapshot.StateAdmitted {
		t.Fatalf("evidence verdict = %s, want ADMITTED", result.Evidence.Verdict)
	}
	if len(result.Evidence.Results) != len(validators) {
		t.Fatalf("evidence has %d results, want %d (one per validator, even on the happy path)",
			len(result.Evidence.Results), len(validators))
	}
	for _, r := range result.Evidence.Results {
		if !r.Passed {
			t.Fatalf("validator %s failed on well-formed input: %s", r.Name, r.Detail)
		}
	}

	// A malformed capture for the same provider is a different snapshot
	// (different canonical digest) and must be REJECTED, with its own
	// evidence record -- "an evidence record either way".
	malformed := []byte(`{"type": "object", `) // truncated JSON
	rejectedResult, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, baseRequest(malformed))
	if err != nil {
		t.Fatalf("Ingest malformed: %v", err)
	}
	if rejectedResult.Snapshot.State != schemasnapshot.StateRejected {
		t.Fatalf("malformed snapshot state = %s, want REJECTED", rejectedResult.Snapshot.State)
	}
	if err := rejectedResult.Snapshot.RequireAdmitted(); !errors.Is(err, schemasnapshot.ErrNotAdmitted) {
		t.Fatalf("RequireAdmitted on a rejected snapshot = %v, want ErrNotAdmitted", err)
	}
	if rejectedResult.Evidence.Verdict != schemasnapshot.StateRejected {
		t.Fatalf("rejected evidence verdict = %s, want REJECTED", rejectedResult.Evidence.Verdict)
	}

	// Idempotent re-ingest: byte-identical content for the same provider
	// resolves to the same snapshot id and is not re-decided.
	replay, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, baseRequest(wellFormed))
	if err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	if replay.SnapshotCreated || replay.ArtifactCreated {
		t.Fatalf("re-ingest of identical content reported creation: %+v", replay)
	}
	if replay.Snapshot.SnapshotID != result.Snapshot.SnapshotID {
		t.Fatalf("re-ingest produced a different snapshot id: %s vs %s",
			replay.Snapshot.SnapshotID, result.Snapshot.SnapshotID)
	}
	if replay.Evidence.EvidenceID != result.Evidence.EvidenceID {
		t.Fatalf("re-ingest produced a second evidence record: %s vs %s",
			replay.Evidence.EvidenceID, result.Evidence.EvidenceID)
	}

	// Supersession: a new capture may declare that it supersedes the
	// admitted one, and only for the same provider.
	newer := baseRequest([]byte(`{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"}}}`))
	supersedes := result.Snapshot.SnapshotID
	newer.Supersedes = &supersedes
	supersedingResult, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, newer)
	if err != nil {
		t.Fatalf("superseding ingest: %v", err)
	}
	if supersedingResult.Snapshot.Supersedes == nil || *supersedingResult.Snapshot.Supersedes != supersedes {
		t.Fatalf("superseding snapshot does not link back to %s", supersedes)
	}

	otherProvider := newer
	otherProvider.Provider = schemasnapshot.ProviderRef{ConnectorID: "adp-wfn", ConnectionID: "harborcare-adp", SourceRef: "adp://harborcare"}
	otherProvider.Raw = []byte(`{"type":"object","properties":{"different":{"type":"boolean"}}}`)
	if _, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, otherProvider); !errors.Is(err, schemasnapshot.ErrInvalid) {
		t.Fatalf("superseding across providers = %v, want ErrInvalid", err)
	}
}

// TestTodo_INTG_004_Golden pins the exact verdict and evidence shape for a
// fixed set of inputs across every declared format, so a change to a
// validator's behavior is caught here rather than downstream.
func TestTodo_INTG_004_Golden(t *testing.T) {
	cases := []struct {
		name    string
		format  schemasnapshot.DeclaredFormat
		raw     []byte
		verdict schemasnapshot.State
	}{
		{"json schema well formed", schemasnapshot.FormatJSONSchema, []byte(`{"type":"string"}`), schemasnapshot.StateAdmitted},
		{"json schema truncated", schemasnapshot.FormatJSONSchema, []byte(`{"type":`), schemasnapshot.StateRejected},
		{"openapi well formed", schemasnapshot.FormatOpenAPI, []byte(`{"openapi":"3.0.0"}`), schemasnapshot.StateAdmitted},
		{"xsd well formed", schemasnapshot.FormatXSD, []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`), schemasnapshot.StateAdmitted},
		{"wsdl malformed", schemasnapshot.FormatWSDL, []byte(`<definitions>`), schemasnapshot.StateRejected},
		{"csv header well formed", schemasnapshot.FormatCSVHeader, []byte("id,name,email\n1,a,b@example.com\n"), schemasnapshot.StateAdmitted},
		{"csv header duplicate column", schemasnapshot.FormatCSVHeader, []byte("id,id,email\n"), schemasnapshot.StateRejected},
		{"graphql sdl well formed", schemasnapshot.FormatGraphQLSDL, []byte("type Worker { id: ID! }"), schemasnapshot.StateAdmitted},
		{"protobuf empty", schemasnapshot.FormatProtobuf, []byte(""), schemasnapshot.StateRejected},
		{"forbidden pem marker", schemasnapshot.FormatJSONSchema, []byte(`{"note":"-----BEGIN PRIVATE KEY-----"}`), schemasnapshot.StateRejected},
	}

	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, results := schemasnapshot.Decide(tc.format, tc.raw, validators)
			if verdict != tc.verdict {
				t.Fatalf("verdict = %s, want %s; results=%+v", verdict, tc.verdict, results)
			}
			if len(results) != len(validators) {
				t.Fatalf("got %d results, want %d", len(results), len(validators))
			}
		})
	}
}

// TestTodo_INTG_004_Race ingests the same content for the same provider from
// many goroutines concurrently. Exactly one call may report having created
// the artifact and the snapshot; every call must agree on the final verdict.
func TestTodo_INTG_004_Race(t *testing.T) {
	ctx := context.Background()
	artifactStore, store := newHarness()
	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)
	req := baseRequest([]byte(`{"type":"object"}`))

	const workers = 16
	results := make([]schemasnapshot.IngestResult, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = schemasnapshot.Ingest(ctx, artifactStore, store, validators, req)
		}(i)
	}
	wg.Wait()

	artifactCreations, snapshotCreations := 0, 0
	var firstID uuid.UUID
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
		if results[i].ArtifactCreated {
			artifactCreations++
		}
		if results[i].SnapshotCreated {
			snapshotCreations++
		}
		if firstID == uuid.Nil {
			firstID = results[i].Snapshot.SnapshotID
		} else if results[i].Snapshot.SnapshotID != firstID {
			t.Fatalf("worker %d produced a different snapshot id", i)
		}
		if results[i].Snapshot.State != schemasnapshot.StateAdmitted {
			t.Fatalf("worker %d snapshot state = %s, want ADMITTED", i, results[i].Snapshot.State)
		}
	}
	if artifactCreations != 1 {
		t.Fatalf("artifact reported created %d times, want exactly 1", artifactCreations)
	}
	if snapshotCreations != 1 {
		t.Fatalf("snapshot reported created %d times, want exactly 1", snapshotCreations)
	}
}

// failingArtifactStore always fails Put, for TestTodo_INTG_004_Fault.
type failingArtifactStore struct{ err error }

func (f failingArtifactStore) Put(context.Context, schemasnapshot.ArtifactPutRequest) (string, int64, bool, error) {
	return "", 0, false, f.err
}

// failingStore fails whichever call name is configured, for
// TestTodo_INTG_004_Fault.
type failingStore struct {
	schemasnapshot.Store
	failInsert bool
	failDecide bool
	err        error
}

func (f failingStore) Insert(ctx context.Context, snap schemasnapshot.SchemaSnapshot) (schemasnapshot.SchemaSnapshot, bool, error) {
	if f.failInsert {
		return schemasnapshot.SchemaSnapshot{}, false, f.err
	}
	return f.Store.Insert(ctx, snap)
}

func (f failingStore) Decide(ctx context.Context, tenantID string, id uuid.UUID, verdict schemasnapshot.State, reason string, decidedAt time.Time, results []schemasnapshot.ValidatorResult) (schemasnapshot.SchemaSnapshot, schemasnapshot.Evidence, error) {
	if f.failDecide {
		return schemasnapshot.SchemaSnapshot{}, schemasnapshot.Evidence{}, f.err
	}
	return f.Store.Decide(ctx, tenantID, id, verdict, reason, decidedAt, results)
}

// TestTodo_INTG_004_Fault proves that a failure at any step of Ingest is
// surfaced to the caller rather than silently admitting the snapshot, and
// that malformed requests are rejected before any store is touched.
func TestTodo_INTG_004_Fault(t *testing.T) {
	ctx := context.Background()
	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)
	boom := errors.New("boom")

	t.Run("artifact store failure", func(t *testing.T) {
		_, store := newHarness()
		_, err := schemasnapshot.Ingest(ctx, failingArtifactStore{err: boom}, store, validators, baseRequest([]byte(`{}`)))
		if err == nil || !errors.Is(err, schemasnapshot.ErrStore) || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("Ingest = %v, want an ErrStore mentioning %v", err, boom)
		}
	})

	t.Run("insert failure", func(t *testing.T) {
		artifactStore, base := newHarness()
		store := failingStore{Store: base, failInsert: true, err: boom}
		_, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, baseRequest([]byte(`{}`)))
		if !errors.Is(err, boom) {
			t.Fatalf("Ingest = %v, want an error wrapping %v", err, boom)
		}
	})

	t.Run("decide failure leaves the snapshot quarantined, not admitted", func(t *testing.T) {
		artifactStore, base := newHarness()
		store := failingStore{Store: base, failDecide: true, err: boom}
		_, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, baseRequest([]byte(`{}`)))
		if !errors.Is(err, boom) {
			t.Fatalf("Ingest = %v, want an error wrapping %v", err, boom)
		}
		// The row Insert wrote through the underlying store is still there,
		// still quarantined: the failure never admitted it as a side effect.
		req := baseRequest([]byte(`{}`))
		identity := schemasnapshot.Identity{TenantID: req.TenantID, Provider: req.Provider, CanonicalDigest: sha256Hex(req.Raw)}
		snap, err := base.Get(ctx, testTenant, identity.SnapshotID())
		if err != nil {
			t.Fatalf("Get after failed decide: %v", err)
		}
		if snap.State != schemasnapshot.StateQuarantined {
			t.Fatalf("snapshot state after failed decide = %s, want QUARANTINED", snap.State)
		}
		if err := snap.RequireAdmitted(); !errors.Is(err, schemasnapshot.ErrNotAdmitted) {
			t.Fatalf("RequireAdmitted after failed decide = %v, want ErrNotAdmitted", err)
		}
	})

	t.Run("no validators configured", func(t *testing.T) {
		artifactStore, store := newHarness()
		_, err := schemasnapshot.Ingest(ctx, artifactStore, store, nil, baseRequest([]byte(`{}`)))
		if !errors.Is(err, schemasnapshot.ErrIncomplete) {
			t.Fatalf("Ingest with no validators = %v, want ErrIncomplete", err)
		}
	})

	t.Run("incomplete request", func(t *testing.T) {
		artifactStore, store := newHarness()
		req := baseRequest([]byte(`{}`))
		req.TenantID = ""
		if _, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, req); !errors.Is(err, schemasnapshot.ErrIncomplete) {
			t.Fatalf("Ingest with no tenant = %v, want ErrIncomplete", err)
		}
	})

	t.Run("supersedes an unknown snapshot", func(t *testing.T) {
		artifactStore, store := newHarness()
		req := baseRequest([]byte(`{}`))
		missing := uuid.New()
		req.Supersedes = &missing
		if _, err := schemasnapshot.Ingest(ctx, artifactStore, store, validators, req); !errors.Is(err, schemasnapshot.ErrIncomplete) {
			t.Fatalf("Ingest superseding an unknown snapshot = %v, want ErrIncomplete", err)
		}
	})
}

// TestTodo_INTG_004_Mutation proves the structural guards RED names: an
// illegal transition, a redecided snapshot, a self-superseding link and a
// mismatched digest algorithm are all rejected rather than silently
// accepted.
func TestTodo_INTG_004_Mutation(t *testing.T) {
	ctx := context.Background()
	base := validSnapshot()

	t.Run("illegal transition from a terminal state", func(t *testing.T) {
		store := schemasnapshot.NewMemoryStore()
		if _, _, err := store.Insert(ctx, base); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if _, _, err := store.Decide(ctx, base.TenantID, base.SnapshotID, schemasnapshot.StateAdmitted, "ok", time.Now(), []schemasnapshot.ValidatorResult{{Name: "X", Passed: true}}); err != nil {
			t.Fatalf("first decide: %v", err)
		}
		if _, _, err := store.Decide(ctx, base.TenantID, base.SnapshotID, schemasnapshot.StateRejected, "changed my mind", time.Now(), []schemasnapshot.ValidatorResult{{Name: "X", Passed: false}}); !errors.Is(err, schemasnapshot.ErrImmutable) {
			t.Fatalf("redeciding a terminal snapshot = %v, want ErrImmutable", err)
		}
	})

	t.Run("Insert refuses a snapshot not in QUARANTINED", func(t *testing.T) {
		store := schemasnapshot.NewMemoryStore()
		admitted := base
		admitted.State = schemasnapshot.StateAdmitted
		admitted.StateReason = "ok"
		admitted.DecidedAt = time.Now()
		if _, _, err := store.Insert(ctx, admitted); !errors.Is(err, schemasnapshot.ErrInvalid) {
			t.Fatalf("Insert of a non-quarantined snapshot = %v, want ErrInvalid", err)
		}
	})

	t.Run("different identity metadata under the same id is immutable", func(t *testing.T) {
		store := schemasnapshot.NewMemoryStore()
		if _, _, err := store.Insert(ctx, base); err != nil {
			t.Fatalf("insert: %v", err)
		}
		conflicting := base
		conflicting.ByteSize = base.ByteSize + 1
		if _, _, err := store.Insert(ctx, conflicting); !errors.Is(err, schemasnapshot.ErrImmutable) {
			t.Fatalf("conflicting insert = %v, want ErrImmutable", err)
		}
	})

	t.Run("self-supersede is rejected", func(t *testing.T) {
		snap := base
		self := snap.SnapshotID
		snap.Supersedes = &self
		if err := snap.Validate(); !errors.Is(err, schemasnapshot.ErrInvalid) {
			t.Fatalf("Validate with self-supersede = %v, want ErrInvalid", err)
		}
	})

	t.Run("wrong digest algorithm", func(t *testing.T) {
		snap := base
		snap.DigestAlgorithm = "md5"
		if err := snap.Validate(); !errors.Is(err, schemasnapshot.ErrIncomplete) {
			t.Fatalf("Validate with wrong algorithm = %v, want ErrIncomplete", err)
		}
	})

	t.Run("decided snapshot without a reason", func(t *testing.T) {
		snap := base
		snap.State = schemasnapshot.StateAdmitted
		snap.DecidedAt = time.Now()
		if err := snap.Validate(); !errors.Is(err, schemasnapshot.ErrIncomplete) {
			t.Fatalf("Validate with no reason = %v, want ErrIncomplete", err)
		}
	})

	t.Run("quarantined snapshot carrying a decision", func(t *testing.T) {
		snap := base
		snap.StateReason = "should not be here"
		if err := snap.Validate(); !errors.Is(err, schemasnapshot.ErrInvalid) {
			t.Fatalf("Validate of a quarantined snapshot with a reason = %v, want ErrInvalid", err)
		}
	})

	t.Run("IsLegalTransition table", func(t *testing.T) {
		if !schemasnapshot.IsLegalTransition(schemasnapshot.StateQuarantined, schemasnapshot.StateAdmitted) {
			t.Fatal("QUARANTINED -> ADMITTED must be legal")
		}
		if !schemasnapshot.IsLegalTransition(schemasnapshot.StateQuarantined, schemasnapshot.StateRejected) {
			t.Fatal("QUARANTINED -> REJECTED must be legal")
		}
		if schemasnapshot.IsLegalTransition(schemasnapshot.StateAdmitted, schemasnapshot.StateRejected) {
			t.Fatal("ADMITTED -> REJECTED must not be legal")
		}
		if schemasnapshot.IsLegalTransition(schemasnapshot.StateQuarantined, schemasnapshot.StateQuarantined) {
			t.Fatal("a self-transition must not be legal")
		}
	})
}

// FuzzTodo_INTG_004 fuzzes Decide directly: no format or byte string may
// panic, and the verdict must always be terminal (ADMITTED or REJECTED),
// with exactly one result per configured validator.
func FuzzTodo_INTG_004(f *testing.F) {
	f.Add(string(schemasnapshot.FormatJSONSchema), []byte(`{"type":"string"}`))
	f.Add(string(schemasnapshot.FormatXSD), []byte(`<a/>`))
	f.Add(string(schemasnapshot.FormatCSVHeader), []byte("a,b,c\n"))
	f.Add("", []byte(nil))
	f.Add(string(schemasnapshot.FormatProtobuf), []byte{0, 1, 2, 0xff})
	f.Add(string(schemasnapshot.FormatGraphQLSDL), []byte("\xff\xfe not utf8"))

	validators := schemasnapshot.DefaultValidators(schemasnapshot.DefaultMaxBytes)
	f.Fuzz(func(t *testing.T, format string, raw []byte) {
		verdict, results := schemasnapshot.Decide(schemasnapshot.DeclaredFormat(format), raw, validators)
		if !verdict.Terminal() {
			t.Fatalf("Decide returned non-terminal state %s", verdict)
		}
		if len(results) != len(validators) {
			t.Fatalf("got %d results, want %d", len(results), len(validators))
		}
	})
}

func validSnapshot() schemasnapshot.SchemaSnapshot {
	digest := sha256Hex([]byte(`{"type":"object"}`))
	return schemasnapshot.SchemaSnapshot{
		SnapshotID:      uuid.New(),
		TenantID:        testTenant,
		Provider:        testProvider(),
		CapturedAt:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		DeclaredFormat:  schemasnapshot.FormatJSONSchema,
		DigestAlgorithm: schemasnapshot.Algorithm,
		CanonicalDigest: digest,
		ArtifactRef:     digest,
		ByteSize:        18,
		State:           schemasnapshot.StateQuarantined,
	}
}
