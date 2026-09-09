package schemasnapshot_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func validArtifactRequest(raw []byte) schemasnapshot.ArtifactPutRequest {
	return schemasnapshot.ArtifactPutRequest{
		TenantID:            testTenant,
		Raw:                 raw,
		MediaType:           "application/json",
		Classification:      model.ClassInternal,
		RetentionClass:      "integration-schema",
		CreatorPrincipalRef: "system:discovery-worker",
		EvidenceID:          "evidence:1",
	}
}

func TestMemoryArtifactStorePut(t *testing.T) {
	ctx := context.Background()
	store := schemasnapshot.NewMemoryArtifactStore()

	id1, size1, created1, err := store.Put(ctx, validArtifactRequest([]byte("hello")))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !created1 || size1 != 5 {
		t.Fatalf("first put created=%v size=%d, want true and 5", created1, size1)
	}

	id2, size2, created2, err := store.Put(ctx, validArtifactRequest([]byte("hello")))
	if err != nil {
		t.Fatalf("Put (repeat): %v", err)
	}
	if created2 {
		t.Fatal("repeat put of identical content reported created")
	}
	if id1 != id2 || size1 != size2 {
		t.Fatalf("repeat put returned (%s, %d), want (%s, %d)", id2, size2, id1, size1)
	}

	cases := []struct {
		name string
		req  schemasnapshot.ArtifactPutRequest
	}{
		{"no tenant", func() schemasnapshot.ArtifactPutRequest {
			r := validArtifactRequest([]byte("x"))
			r.TenantID = ""
			return r
		}()},
		{"no bytes", func() schemasnapshot.ArtifactPutRequest { r := validArtifactRequest(nil); return r }()},
		{"no media type", func() schemasnapshot.ArtifactPutRequest {
			r := validArtifactRequest([]byte("x"))
			r.MediaType = ""
			return r
		}()},
		{"no classification", func() schemasnapshot.ArtifactPutRequest {
			r := validArtifactRequest([]byte("x"))
			r.Classification = ""
			return r
		}()},
		{"no retention class", func() schemasnapshot.ArtifactPutRequest {
			r := validArtifactRequest([]byte("x"))
			r.RetentionClass = ""
			return r
		}()},
		{"no creator principal", func() schemasnapshot.ArtifactPutRequest {
			r := validArtifactRequest([]byte("x"))
			r.CreatorPrincipalRef = ""
			return r
		}()},
		{"no evidence id", func() schemasnapshot.ArtifactPutRequest {
			r := validArtifactRequest([]byte("x"))
			r.EvidenceID = ""
			return r
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := store.Put(ctx, tc.req); !errors.Is(err, schemasnapshot.ErrIncomplete) {
				t.Fatalf("Put(%+v) = %v, want ErrIncomplete", tc.req, err)
			}
		})
	}
}

func TestMemoryStoreGetAndEvidenceNotFound(t *testing.T) {
	ctx := context.Background()
	store := schemasnapshot.NewMemoryStore()

	if _, err := store.Get(ctx, testTenant, uuid.New()); !errors.Is(err, schemasnapshot.ErrNotFound) {
		t.Fatalf("Get of an unknown snapshot = %v, want ErrNotFound", err)
	}
	if _, ok, err := store.Evidence(ctx, testTenant, uuid.New()); err != nil || ok {
		t.Fatalf("Evidence of an undecided snapshot = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestMemoryStoreDecideRejectsIncompleteInputs(t *testing.T) {
	ctx := context.Background()
	store := schemasnapshot.NewMemoryStore()
	snap := validSnapshot()
	if _, _, err := store.Insert(ctx, snap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	cases := []struct {
		name      string
		verdict   schemasnapshot.State
		reason    string
		decidedAt time.Time
		results   []schemasnapshot.ValidatorResult
	}{
		{"non-terminal verdict", schemasnapshot.StateQuarantined, "x", time.Now(), []schemasnapshot.ValidatorResult{{Name: "A", Passed: true}}},
		{"no reason", schemasnapshot.StateAdmitted, "", time.Now(), []schemasnapshot.ValidatorResult{{Name: "A", Passed: true}}},
		{"no decision time", schemasnapshot.StateAdmitted, "x", time.Time{}, []schemasnapshot.ValidatorResult{{Name: "A", Passed: true}}},
		{"no results", schemasnapshot.StateAdmitted, "x", time.Now(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := store.Decide(ctx, snap.TenantID, snap.SnapshotID, tc.verdict, tc.reason, tc.decidedAt, tc.results); !errors.Is(err, schemasnapshot.ErrIncomplete) && !errors.Is(err, schemasnapshot.ErrInvalid) {
				t.Fatalf("Decide(%+v) = %v, want ErrIncomplete or ErrInvalid", tc, err)
			}
		})
	}
}

func TestMemoryStoreDecideUnknownSnapshot(t *testing.T) {
	ctx := context.Background()
	store := schemasnapshot.NewMemoryStore()
	_, _, err := store.Decide(ctx, testTenant, uuid.New(), schemasnapshot.StateAdmitted, "x", time.Now(), []schemasnapshot.ValidatorResult{{Name: "A", Passed: true}})
	if !errors.Is(err, schemasnapshot.ErrNotFound) {
		t.Fatalf("Decide on an unknown snapshot = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreInsertSupersedesUnknownProvider(t *testing.T) {
	ctx := context.Background()
	store := schemasnapshot.NewMemoryStore()

	prior := validSnapshot()
	if _, _, err := store.Insert(ctx, prior); err != nil {
		t.Fatalf("insert prior: %v", err)
	}

	next := validSnapshot()
	next.SnapshotID = uuid.New()
	next.Provider = schemasnapshot.ProviderRef{ConnectorID: "other", ConnectionID: "other-conn", SourceRef: "other://x"}
	next.CanonicalDigest = sha256Hex([]byte("different content"))
	next.ArtifactRef = next.CanonicalDigest
	priorID := prior.SnapshotID
	next.Supersedes = &priorID

	if _, _, err := store.Insert(ctx, next); !errors.Is(err, schemasnapshot.ErrInvalid) {
		t.Fatalf("Insert superseding a different provider = %v, want ErrInvalid", err)
	}
}

func TestEvidenceValidate(t *testing.T) {
	base := schemasnapshot.Evidence{
		EvidenceID: uuid.New(), SnapshotID: uuid.New(), TenantID: testTenant,
		Verdict: schemasnapshot.StateAdmitted, Results: []schemasnapshot.ValidatorResult{{Name: "A", Passed: true}},
		DecidedAt: time.Now(),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("Validate on a complete evidence record: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(schemasnapshot.Evidence) schemasnapshot.Evidence
		wantErr error
	}{
		{"no id", func(e schemasnapshot.Evidence) schemasnapshot.Evidence { e.EvidenceID = uuid.Nil; return e }, schemasnapshot.ErrIncomplete},
		{"no snapshot", func(e schemasnapshot.Evidence) schemasnapshot.Evidence { e.SnapshotID = uuid.Nil; return e }, schemasnapshot.ErrIncomplete},
		{"no tenant", func(e schemasnapshot.Evidence) schemasnapshot.Evidence { e.TenantID = ""; return e }, schemasnapshot.ErrIncomplete},
		{"quarantined verdict", func(e schemasnapshot.Evidence) schemasnapshot.Evidence {
			e.Verdict = schemasnapshot.StateQuarantined
			return e
		}, schemasnapshot.ErrInvalid},
		{"no results", func(e schemasnapshot.Evidence) schemasnapshot.Evidence { e.Results = nil; return e }, schemasnapshot.ErrIncomplete},
		{"no decision time", func(e schemasnapshot.Evidence) schemasnapshot.Evidence { e.DecidedAt = time.Time{}; return e }, schemasnapshot.ErrIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
