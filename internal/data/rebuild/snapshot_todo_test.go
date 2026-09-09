package rebuild_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/rebuild"
)

func snapshotFixture(t *testing.T) rebuild.SnapshotManifest {
	t.Helper()
	m := rebuild.SnapshotManifest{
		Tenant: uuid.New(), ProjectionName: "worker_state", SchemaDigest: "schema-1", ConfigDigest: "config-1",
		KeyRefs: []string{"key:b", "key:a"}, CreatedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Streams: []rebuild.SnapshotStream{{StreamKey: "worker:1", Head: 2, HeadDigest: "head-2", SchemaRef: "schema:event@1"}},
	}
	digest, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	m.ManifestDigest = digest
	return m
}

func TestTodo_DATA_013(t *testing.T) {
	m := snapshotFixture(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := m.CompatibleWithCurrentHeads(map[string]rebuild.SnapshotStream{"worker:1": m.Streams[0]}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_DATA_013_Golden(t *testing.T) {
	m := snapshotFixture(t)
	first, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	m.KeyRefs[0], m.KeyRefs[1] = m.KeyRefs[1], m.KeyRefs[0]
	second, err := m.Digest()
	if err != nil || first != second {
		t.Fatalf("key ordering changed digest: %s != %s (%v)", first, second, err)
	}
}

func TestTodo_DATA_013_Security(t *testing.T) {
	m := snapshotFixture(t)
	m.Streams[0].HeadDigest = "forged"
	if err := m.Validate(); !errors.Is(err, rebuild.ErrSnapshotInvalid) {
		t.Fatalf("tampered manifest error = %v, want invalid", err)
	}
}

func TestTodo_DATA_013_Recovery(t *testing.T) {
	m := snapshotFixture(t)
	heads := map[string]rebuild.SnapshotStream{"worker:1": m.Streams[0]}
	if err := m.CompatibleWithCurrentHeads(heads); err != nil {
		t.Fatal(err)
	}
	heads["worker:1"] = rebuild.SnapshotStream{StreamKey: "worker:1", Head: 3, HeadDigest: "head-3", SchemaRef: "schema:event@1"}
	if err := m.CompatibleWithCurrentHeads(heads); !errors.Is(err, rebuild.ErrSnapshotDrift) {
		t.Fatalf("head drift error = %v, want drift", err)
	}
}
