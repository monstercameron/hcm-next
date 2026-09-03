package transformation

import (
	"errors"
	"testing"
)

func definition() TransformationDefinition {
	return TransformationDefinition{Version: ContractVersion, Name: "people-copy", Owner: "shared-engines", Phase: "P1A",
		Source:        Schema{Name: "people", Version: 1, Fields: []Field{{Name: "given", Type: TypeString, Required: true}, {Name: "age", Type: TypeInt}}},
		Destination:   Schema{Name: "worker", Version: 1, Fields: []Field{{Name: "name", Type: TypeString, Required: true}, {Name: "age", Type: TypeInt}}},
		Operations:    []Operation{{Kind: OpCopy, Source: &Path{Schema: "people", Field: "given", Type: TypeString}, Destination: Path{Schema: "worker", Field: "name", Type: TypeString}}},
		Compatibility: Compatibility{MinimumSourceVersion: 1}, Limits: ResourceLimits{MaxOperations: 4, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxExpansion: 1}, Failure: FailureReject, SideEffects: SideEffectsNone}
}

func TestValidateAndDigest(t *testing.T) {
	d := definition()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	digest, err := d.Digest()
	if err != nil || len(digest) != 71 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
}
func TestRejectsUntypedAndArbitraryOperations(t *testing.T) {
	d := definition()
	d.Operations[0].Source.Type = TypeBool
	if !errors.Is(d.Validate(), ErrUntypedPath) {
		t.Fatal("expected typed path error")
	}
	d = definition()
	d.Operations[0].Kind = "script"
	if !errors.Is(d.Validate(), ErrArbitraryCode) {
		t.Fatal("expected code error")
	}
}
func TestDigestStableAcrossSchemaFieldOrder(t *testing.T) {
	a := definition()
	b := definition()
	b.Source.Fields[0], b.Source.Fields[1] = b.Source.Fields[1], b.Source.Fields[0]
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da != db {
		t.Fatalf("digest changed: %s != %s", da, db)
	}
}
