package propagation

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func meta(state values.PresenceState, class Classification, provenance, taint []string) Metadata {
	return Metadata{Presence: state, Classification: class, Provenance: provenance, Taint: taint}
}

func TestTodo_XFORM_004(t *testing.T) {
	got, err := Propagate(transformation.OpCopy, []Metadata{meta(values.PresenceValue, ClassificationInternal, []string{"source:a"}, []string{"untrusted"})}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Presence != values.PresenceValue || got.Classification != ClassificationInternal || len(got.Provenance) != 1 || len(got.Taint) != 1 {
		t.Fatalf("unexpected propagation: %+v", got)
	}
	got, err = WithOperation(got, "copy-1")
	if err != nil || len(got.Provenance) != 2 {
		t.Fatalf("operation lineage missing: %+v %v", got, err)
	}
}

func TestTodo_XFORM_004_Property(t *testing.T) {
	got, err := Propagate(transformation.OpConcat, []Metadata{
		meta(values.PresenceValue, ClassificationInternal, []string{"z"}, []string{"a"}),
		meta(values.PresenceUnknown, ClassificationSecret, []string{"a"}, []string{"b"}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Presence != values.PresenceUnknown || got.Classification != ClassificationSecret {
		t.Fatalf("lost restrictive metadata: %+v", got)
	}
	if len(got.Provenance) != 2 || got.Provenance[0] != "a" || len(got.Taint) != 2 {
		t.Fatalf("union not canonical: %+v", got)
	}
}

func TestTodo_XFORM_004_Golden(t *testing.T) {
	got, err := Propagate(transformation.OpConcat, []Metadata{
		meta(values.PresenceValue, ClassificationInternal, []string{"z"}, []string{"t2"}),
		meta(values.PresenceValue, ClassificationConfidential, []string{"a"}, []string{"t1"}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Metadata{Presence: values.PresenceValue, Classification: ClassificationConfidential, Provenance: []string{"a", "z"}, Taint: []string{"t1", "t2"}}
	if got.Presence != want.Presence || got.Classification != want.Classification || got.Provenance[0] != want.Provenance[0] || got.Provenance[1] != want.Provenance[1] || got.Taint[0] != want.Taint[0] || got.Taint[1] != want.Taint[1] {
		t.Fatalf("golden mismatch: got %+v want %+v", got, want)
	}
}

func FuzzTodo_XFORM_004(f *testing.F) {
	f.Add("source:a", "taint:a")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, provenance, taint string) {
		_, _ = Propagate(transformation.OpCopy, []Metadata{meta(values.PresenceValue, ClassificationInternal, []string{provenance}, []string{taint})}, nil)
	})
}

func TestTodo_XFORM_004_Security(t *testing.T) {
	got, err := Propagate(transformation.OpCopy, []Metadata{meta(values.PresenceValue, ClassificationSecret, []string{"src"}, []string{"untrusted"})}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != ClassificationSecret || len(got.Taint) != 1 {
		t.Fatalf("security metadata downgraded: %+v", got)
	}
	if _, err := Propagate(transformation.OpCopy, []Metadata{meta(values.PresenceUnspecified, ClassificationPublic, nil, nil)}, nil); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("invalid presence accepted: %v", err)
	}
}

func TestTodo_XFORM_004_Mutation(t *testing.T) {
	for _, state := range []values.PresenceState{values.PresenceNull, values.PresenceRedacted, values.PresenceUnavailable, values.PresenceNotApplicable} {
		got, err := Propagate(transformation.OpCopy, []Metadata{meta(state, ClassificationInternal, nil, nil)}, nil)
		if err != nil || got.Presence != state {
			t.Fatalf("state %s changed to %+v (%v)", state, got, err)
		}
	}
	defaultMeta := meta(values.PresenceValue, ClassificationInternal, []string{"default"}, nil)
	got, err := Propagate(transformation.OpDefault, nil, &defaultMeta)
	if err != nil || got.Classification != ClassificationInternal {
		t.Fatalf("default metadata lost: %+v %v", got, err)
	}
}
