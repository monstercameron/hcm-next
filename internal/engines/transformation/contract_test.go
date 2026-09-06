package transformation

import (
	"strings"
	"testing"
)

// TestVersionAndExplain covers the ARCH-GO-009 engine contract: this
// package's own Version() and an Explain-shaped symbol.
func TestVersionAndExplain(t *testing.T) {
	if Version() != ContractVersion {
		t.Fatalf("Version()=%d, want ContractVersion=%d", Version(), ContractVersion)
	}
	d := definition()
	out := d.Explain()
	for _, want := range []string{"people-copy", "shared-engines", "P1A", "people@1", "worker@1", "copy people.given"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Explain() missing %q in:\n%s", want, out)
		}
	}
}

// TestExplainCoversEveryOperationKind exercises explainOperation's branches
// so a future op kind cannot silently fall through to the default case.
func TestExplainCoversEveryOperationKind(t *testing.T) {
	d := definition()
	d.Operations = []Operation{
		{Kind: OpRename, Source: &Path{Schema: "people", Field: "given", Type: TypeString}, Destination: Path{Schema: "worker", Field: "name", Type: TypeString}},
		{Kind: OpConvert, Source: &Path{Schema: "people", Field: "age", Type: TypeInt}, Destination: Path{Schema: "worker", Field: "age", Type: TypeInt}, TargetType: TypeInt},
		{Kind: OpDefault, Destination: Path{Schema: "worker", Field: "name", Type: TypeString}, Literal: "unknown"},
		{Kind: OpConcat, Destination: Path{Schema: "worker", Field: "name", Type: TypeString}, Sources: []Path{{Schema: "people", Field: "given", Type: TypeString}, {Schema: "people", Field: "given", Type: TypeString}}},
	}
	out := d.Explain()
	for _, want := range []string{"rename people.given", "convert people.age", "default worker.name", "concat ["} {
		if !strings.Contains(out, want) {
			t.Fatalf("Explain() missing %q in:\n%s", want, out)
		}
	}
}
