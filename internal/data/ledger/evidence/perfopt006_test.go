package evidence_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
)

func perfEvidenceContent(t testing.TB, count int) evidence.Content {
	t.Helper()
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	digester := hashchain.NewDigester(registry)
	stream := goldenStream(t, digester, streamOne, count)
	root, err := checkpoint.ComputeRootDigest([]evidence.StreamHead{stream.Head})
	if err != nil {
		t.Fatal(err)
	}
	epoch := evidence.Epoch{
		SchemaVersion: checkpoint.ManifestSchemaVersion,
		Tenant:        goldenTenant, EpochID: goldenEpochID, EpochNumber: 1,
		Schema: goldenSchema, Streams: []evidence.StreamHead{stream.Head},
		RootDigest: root, RootDigestAlgorithm: checkpoint.DigestAlgorithm,
		CoversFrom: goldenFrom, CoversTo: goldenTo, CreatedAt: goldenTo,
	}
	return evidence.Content{
		Tenant: goldenTenant, CoversFrom: goldenFrom, CoversTo: goldenTo,
		Schema: goldenSchema, Streams: []evidence.Stream{stream},
		Epochs: []evidence.Epoch{signEpoch(t, epoch)},
	}
}

func perfEvidencePackage(t testing.TB, count int) evidence.Package {
	t.Helper()
	pkg, err := evidence.Build(perfEvidenceContent(t, count))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestTodo_PERFOPT_006_Golden(t *testing.T) {
	full, err := json.Marshal(perfEvidencePackage(t, 10).Files())
	if err != nil {
		t.Fatal(err)
	}
	edge, err := json.Marshal(perfEvidencePackage(t, 1).Files())
	if err != nil {
		t.Fatal(err)
	}
	assertEvidenceGolden(t, "perfopt006_evidence_full.json", full)
	assertEvidenceGolden(t, "perfopt006_evidence_edge.json", edge)
}

func BenchmarkTodo_PERFOPT_006(b *testing.B) {
	content := perfEvidenceContent(b, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := evidence.Build(content); err != nil {
			b.Fatal(err)
		}
	}
}

func assertEvidenceGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with HCMNEXT_UPDATE_GOLDEN=1)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s differs", path)
	}
}
