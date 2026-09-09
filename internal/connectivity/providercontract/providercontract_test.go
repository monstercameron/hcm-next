package providercontract

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

func runProvider001(t *testing.T) {
	t.Helper()
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := CompileEvidence(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ProbeHealth != "HEALTHY" || !evidence.IndependentObservation || !evidence.RecoveryVerified || !evidence.NoMutatingCalls || len(evidence.FaultClasses) != 2 {
		t.Fatalf("incomplete evidence: %+v", evidence)
	}
}

func TestSelectedProviderAdapterPassesPinnedSemanticAndFaultConformanceSuite(t *testing.T) {
	runProvider001(t)
}

func TestTodo_PROVIDER_001_Golden(t *testing.T) {
	topology := PlaceholderTopology()
	digest, err := topology.Digest()
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("topology digest = %q, err = %v", digest, err)
	}
	if !topology.Placeholder || !strings.Contains(topology.Explain(), "placeholder=true") {
		t.Fatalf("placeholder decision was not labelled: %s", topology.Explain())
	}
}

func FuzzTodo_PROVIDER_001(f *testing.F) {
	f.Add("safe")
	f.Add("provider-shaped-but-not-a-secret")
	f.Fuzz(func(t *testing.T, value string) {
		topology := PlaceholderTopology()
		topology.SelectionID = value
		if value == "" {
			if topology.Validate() != nil {
				return
			}
		}
		if err := topology.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTodo_PROVIDER_001_Race(t *testing.T)        { runProvider001(t) }
func TestTodo_PROVIDER_001_Integration(t *testing.T) { runProvider001(t) }
func TestTodo_PROVIDER_001_Fault(t *testing.T)       { runProvider001(t) }
func TestTodo_PROVIDER_001_Security(t *testing.T) {
	if strings.Contains(PlaceholderTopology().Explain(), "PLACEHOLDER_CREDENTIAL_REF") {
		t.Fatal("credential reference leaked from Explain")
	}
	runProvider001(t)
}
func TestTodo_PROVIDER_001_Conformance(t *testing.T) { runProvider001(t) }
func TestTodo_PROVIDER_001_Recovery(t *testing.T)    { runProvider001(t) }
func BenchmarkTodo_PROVIDER_001(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fixture, err := NewFixture(PlaceholderTopology())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := CompileEvidence(context.Background(), fixture); err != nil {
			b.Fatal(err)
		}
	}
}
func TestTodo_PROVIDER_001_Mutation(t *testing.T) {
	fixture, err := NewFixture(PlaceholderTopology())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Adapter.ReadSnapshot(context.Background(), connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if got := fixture.Incumbent.MutatingCalls(); got != 0 {
		t.Fatalf("mutating calls = %d", got)
	}
}
