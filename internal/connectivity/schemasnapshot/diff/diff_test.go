package diff_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/schemasnapshot/diff"
)

func admitted(provider schemasnapshot.ProviderRef, digest string) schemasnapshot.SchemaSnapshot {
	return schemasnapshot.SchemaSnapshot{Provider: provider, CanonicalDigest: digest, State: schemasnapshot.StateAdmitted}
}

func provider() schemasnapshot.ProviderRef {
	return schemasnapshot.ProviderRef{ConnectorID: "workday", ConnectionID: "prod", SourceRef: "workday://prod"}
}

func oldBody() diff.Body {
	return diff.Body{Entities: []diff.Entity{{Name: "Worker", Fields: []diff.Field{
		{Name: "id", Type: "string"}, {Name: "name", Type: "string"}, {Name: "email", Type: "string"},
	}}}}
}

func newBody() diff.Body {
	return diff.Body{Entities: []diff.Entity{
		{Name: "Worker", Fields: []diff.Field{
			{Name: "id", Type: "integer", Required: true},
			{Name: "display_name", Type: "string"},
			{Name: "start_date", Type: "date"},
		}},
		{Name: "Department", Fields: []diff.Field{{Name: "code", Type: "string"}}},
	}, Renames: []diff.RenameDeclaration{{Entity: "Worker", From: "name", To: "display_name"}}}
}

func TestTodo_INTG_005(t *testing.T) {
	before, after := admitted(provider(), "sha256:before"), admitted(provider(), "sha256:after")
	got, err := diff.Diff(diff.Request{Before: before, After: after, BeforeBody: oldBody(), AfterBody: newBody(), Mappings: []diff.ConsumedFieldMapping{
		{ID: "map-id", Entity: "Worker", Field: "id"},
		{ID: "map-name", Entity: "Worker", Field: "name"},
		{ID: "map-email", Entity: "Worker", Field: "email"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest == "" || !strings.HasPrefix(got.Digest, "sha256:") || got.Explain() == "" {
		t.Fatalf("incomplete diff evidence: %+v", got)
	}
	find := func(kind diff.ChangeKind, field string) diff.Change {
		t.Helper()
		for _, change := range got.Changes {
			if change.Kind == kind && change.Field == field {
				return change
			}
		}
		t.Fatalf("missing %s %s in %+v", kind, field, got.Changes)
		return diff.Change{}
	}
	if c := find(diff.ChangeFieldTypeChanged, "id"); c.Impact != diff.ImpactBreaking || len(c.Consumers) != 1 || c.Consumers[0] != "map-id" {
		t.Fatalf("type impact = %+v", c)
	}
	if c := find(diff.ChangeFieldConstraintChanged, "id"); c.Impact != diff.ImpactBreaking {
		t.Fatalf("constraint impact = %+v", c)
	}
	if c := find(diff.ChangeFieldRemoved, "email"); c.Impact != diff.ImpactBreaking || len(c.Consumers) != 1 {
		t.Fatalf("removal impact = %+v", c)
	}
	if c := find(diff.ChangeFieldRenamed, "display_name"); c.Impact != diff.ImpactCosmetic || len(c.Consumers) != 1 || c.Consumers[0] != "map-name" {
		t.Fatalf("rename impact = %+v", c)
	}
	if c := find(diff.ChangeFieldAdded, "start_date"); c.Impact != diff.ImpactAdditive {
		t.Fatalf("addition impact = %+v", c)
	}
	var entityAdded bool
	for _, c := range got.Changes {
		if c.Kind == diff.ChangeEntityAdded && c.Entity == "Department" && c.Impact == diff.ImpactAdditive {
			entityAdded = true
		}
	}
	if !entityAdded {
		t.Fatalf("missing additive entity change: %+v", got.Changes)
	}
}

func TestTodo_INTG_005_Golden(t *testing.T) {
	before, after := admitted(provider(), "sha256:before"), admitted(provider(), "sha256:after")
	a, err := diff.Diff(diff.Request{Before: before, After: after, BeforeBody: oldBody(), AfterBody: newBody(), Mappings: []diff.ConsumedFieldMapping{{ID: "m", Entity: "Worker", Field: "id"}}})
	if err != nil {
		t.Fatal(err)
	}
	b := newBody()
	b.Entities[0].Fields[0], b.Entities[0].Fields[2] = b.Entities[0].Fields[2], b.Entities[0].Fields[0]
	b.Entities[0], b.Entities[1] = b.Entities[1], b.Entities[0]
	second, err := diff.Diff(diff.Request{Before: before, After: after, BeforeBody: oldBody(), AfterBody: b, ConsumedFieldMappings: []diff.ConsumedFieldMapping{{ID: "m", Entity: "Worker", Field: "id"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != second.Digest {
		t.Fatalf("reordered bodies changed digest: %s != %s", a.Digest, second.Digest)
	}
	if a.CanonicalDigest() != a.Digest {
		t.Fatal("CanonicalDigest does not expose the digest")
	}
}

func TestTodo_INTG_005_Integration(t *testing.T) {
	before, after := admitted(provider(), "sha256:before"), admitted(provider(), "sha256:after")
	got, err := diff.DiffSnapshots(before, oldBody(), after, diff.Body{Entities: oldBody().Entities}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changes) != 0 || !strings.Contains(got.Explain(), "no schema changes") {
		t.Fatalf("unchanged comparison = %+v", got)
	}
}

func TestTodo_INTG_005_Fault(t *testing.T) {
	before, after := admitted(provider(), "sha256:before"), admitted(provider(), "sha256:after")
	before.State = schemasnapshot.StateQuarantined
	if _, err := diff.DiffSnapshots(before, oldBody(), after, newBody(), nil); !errors.Is(err, schemasnapshot.ErrNotAdmitted) {
		t.Fatalf("quarantined diff = %v", err)
	}
	other := provider()
	other.SourceRef = "adp://prod"
	after.Provider = other
	if _, err := diff.DiffSnapshots(admitted(provider(), "sha256:before"), oldBody(), after, newBody(), nil); !errors.Is(err, diff.ErrDifferentProvider) {
		t.Fatalf("cross-provider diff = %v", err)
	}
	bad := newBody()
	bad.Renames = []diff.RenameDeclaration{{Entity: "Worker", From: "name", To: "missing"}}
	if _, err := diff.DiffSnapshots(admitted(provider(), "sha256:before"), oldBody(), admitted(provider(), "sha256:after"), bad, nil); err == nil || !errors.Is(err, diff.ErrInvalidRename) {
		t.Fatalf("invalid rename = %v", err)
	}
}

func TestTodo_INTG_005_Conformance(t *testing.T) {
	before, after := admitted(provider(), "sha256:before"), admitted(provider(), "sha256:after")
	req := diff.Request{Before: before, After: after, BeforeBody: oldBody(), AfterBody: newBody(), Mappings: []diff.ConsumedFieldMapping{{ID: "m", Entity: "Worker", Field: "email"}}}
	a, err := diff.Diff(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := diff.Compare(req)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || len(a.Changes) != len(b.Changes) {
		t.Fatalf("comparison APIs disagree: %q/%q", a.Digest, b.Digest)
	}
}

func FuzzTodo_INTG_005(f *testing.F) {
	f.Add("Worker", "id", "string")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, entityName, fieldName, fieldType string) {
		before := admitted(provider(), "sha256:before")
		after := admitted(provider(), "sha256:after")
		body := diff.Body{Entities: []diff.Entity{{Name: entityName, Fields: []diff.Field{{Name: fieldName, Type: fieldType}}}}}
		_, _ = diff.DiffSnapshots(before, body, after, body, nil)
	})
}
