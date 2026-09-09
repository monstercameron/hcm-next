package snapshot

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestVersionIsTheEngineContractVersion(t *testing.T) {
	if got := Version(); got != 1 {
		t.Fatalf("Version() = %d, want 1", got)
	}
}

func TestExplainNamesEveryEntryWithoutValues(t *testing.T) {
	wm7, err := values.NewSequenceRevision("people.worker_facts", 7)
	if err != nil {
		t.Fatal(err)
	}
	wm2, err := values.NewSequenceRevision("rewards.pay_band", 2)
	if err != nil {
		t.Fatal(err)
	}
	s := ReadSnapshot{
		Tenant: "tenant-a",
		Digest: "sha256:abc",
		Entries: []InputEntry{
			{Name: "people.worker_facts", Owner: "people", Authority: AuthorityNativeState, Watermark: wm7},
			{Name: "rewards.pay_band", Owner: "rewards", Authority: AuthorityReferenceConfig, Watermark: wm2},
		},
	}
	got := s.Explain()
	for _, want := range []string{"sha256:abc", "tenant-a", "2 input(s)", "people.worker_facts (owner people", "rewards.pay_band (owner rewards", wm7.String(), wm2.String()} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, missing %q", got, want)
		}
	}
	if strings.Count(got, "\n- ") != 2 {
		t.Fatalf("Explain() should list exactly one line per entry:\n%s", got)
	}
}
