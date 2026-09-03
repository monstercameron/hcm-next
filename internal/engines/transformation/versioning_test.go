package transformation

import "testing"

func versioningDefinition() TransformationDefinition {
	return TransformationDefinition{Version: ContractVersion, Name: "versioned-map", Owner: "tests", Phase: "P1A",
		Source:        Schema{Name: "source", Version: 1, Fields: []Field{{Name: "value", Type: TypeString}}},
		Destination:   Schema{Name: "destination", Version: 1, Fields: []Field{{Name: "value", Type: TypeString}}},
		Operations:    []Operation{{Kind: OpCopy, Source: &Path{Schema: "source", Field: "value", Type: TypeString}, Destination: Path{Schema: "destination", Field: "value", Type: TypeString}}},
		Compatibility: Compatibility{MinimumSourceVersion: 1}, Limits: ResourceLimits{MaxOperations: 4, MaxInputBytes: 100, MaxOutputBytes: 100, MaxExpansion: 1}, Failure: FailureReject, SideEffects: SideEffectsNone}
}

func TestTodo_XFORM_005(t *testing.T) {
	r := NewRegistry()
	d := versioningDefinition()
	v1, err := r.Publish(d, 1, []Consumer{{Kind: "WORKFLOW", ID: "wf-1"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Pin(d.Name, 1)
	if err != nil {
		t.Fatal(err)
	}
	d.Operations[0].Kind = OpRename
	if _, err = r.Publish(d, 2, nil); err != nil {
		t.Fatal(err)
	}
	got, err := r.ResolvePin(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != v1.Digest || got.Revision != 1 {
		t.Fatalf("pin moved: %#v", got)
	}
}

func TestTodo_XFORM_005_Property(t *testing.T) {
	d := versioningDefinition()
	r := NewRegistry()
	a, err := r.Publish(d, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Resolve(d.Name, 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || a.Revision != b.Revision {
		t.Fatal("published version is not stable")
	}
}

func TestTodo_XFORM_005_Golden(t *testing.T) {
	d := versioningDefinition()
	report, err := CompareVersions(d, d, []Consumer{{Kind: "IMPORT", ID: "i1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Compatible || report.Breaking || len(report.Reasons) != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func FuzzTodo_XFORM_005(f *testing.F) {
	f.Add(1)
	f.Add(2)
	f.Fuzz(func(t *testing.T, revision int) {
		if revision < 1 {
			return
		}
		r := NewRegistry()
		if _, err := r.Publish(versioningDefinition(), revision, nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTodo_XFORM_005_Conformance(t *testing.T) {
	d := versioningDefinition()
	d2 := d
	d2.Destination.Fields = append([]Field(nil), d.Destination.Fields...)
	r, err := CompareVersions(d, d2, []Consumer{{Kind: "EXPORT", ID: "e1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Compatible || len(r.Affected) != 1 || r.Affected[0].ID != "e1" {
		t.Fatalf("report mismatch: %#v", r)
	}
}

func TestTodo_XFORM_005_Mutation(t *testing.T) {
	d := versioningDefinition()
	changed := d
	changed.Destination.Fields = append([]Field(nil), d.Destination.Fields...)
	changed.Destination.Fields[0].Type = TypeInt
	changed.Operations = []Operation{{Kind: OpConvert, Source: &Path{Schema: "source", Field: "value", Type: TypeString}, Destination: Path{Schema: "destination", Field: "value", Type: TypeInt}, TargetType: TypeInt}}
	r, err := CompareVersions(d, changed, []Consumer{{Kind: "CONSUMER", ID: "c1"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Compatible || !r.Breaking || len(r.Affected) != 1 {
		t.Fatalf("breaking mutation accepted: %#v", r)
	}
}
