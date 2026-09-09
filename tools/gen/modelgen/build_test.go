package modelgen

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustOpenInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	return iv
}

// syntheticRegistry builds a minimal, valid model.Registry with one
// EVIDENCE-class entity ("Thing") carrying properties of every fieldKind
// modelgen supports, including values.KnownAt and values.RecordedAt — types
// no property in the real compiled catalog uses yet, so this is the only
// place those two [fieldKind] cases are exercised end to end.
func syntheticRegistry(t *testing.T, extraProps ...model.PropertyDefinition) *model.Registry {
	t.Helper()
	authority := model.SourceAuthorityAssignment{
		AssignmentRef:    "authority.thing/v1",
		Kind:             model.AuthorityInternal,
		DomainScope:      "thing",
		Effective:        mustOpenInterval(t),
		Exclusive:        true,
		FreshnessSeconds: 0,
		Merge:            model.MergeAuthorityPrecedence,
		EvidenceRef:      "evidence.thing_registration/v1",
	}
	retention := model.RetentionClass{
		ClassRef:          "TEST_CLASS",
		DefaultPeriodDays: 365,
		TriggerEvent:      "RECORD_CREATED",
		DispositionOwner:  "test-owner",
		AuthorityRef:      "authority.thing/v1",
	}
	entity := model.EntityDefinition{
		Ref:                 model.EntityRef{Name: "Thing", Version: 1},
		Key:                 "thing",
		OwnerDomain:         "TEST",
		Class:               model.ClassEvidence,
		LifecycleAssignment: "TestLifecycle",
		TenantScoped:        true,
		Status:              model.StatusActive,
	}
	p := func(path, goType string) model.PropertyDefinition {
		return model.PropertyDefinition{
			Ref:               model.PropertyRef("thing." + path),
			Entity:            entity.Ref,
			GoType:            goType,
			SchemaPath:        "hcmnext.test.v1.Thing." + path,
			Presence:          model.PresenceRequired,
			Classification:    model.ClassInternal,
			Temporal:          model.TemporalPointInTime,
			AuthorityRef:      "authority.thing/v1",
			Correction:        model.CorrectionAppends,
			RetentionClassRef: "TEST_CLASS",
			Status:            model.StatusActive,
		}
	}
	props := []model.PropertyDefinition{
		p("known_at", "values.KnownAt"),
		p("recorded_at", "values.RecordedAt"),
	}
	props = append(props, extraProps...)

	reg, err := model.NewRegistry(
		[]model.EntityDefinition{entity},
		props,
		nil,
		nil,
		[]model.SourceAuthorityAssignment{authority},
		[]model.RetentionClass{retention},
	)
	if err != nil {
		t.Fatalf("model.NewRegistry: %v", err)
	}
	return reg
}

func TestBuildRealCatalog(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ms.SourceDigest != reg.Digest() {
		t.Fatalf("SourceDigest = %s, want %s", ms.SourceDigest, reg.Digest())
	}
	if len(ms.Entities) != len(reg.Entities()) {
		t.Fatalf("Entities len = %d, want %d", len(ms.Entities), len(reg.Entities()))
	}
	var totalFields int
	for _, e := range ms.Entities {
		totalFields += len(e.Fields)
		if e.TypeName == "" {
			t.Errorf("entity %s has empty TypeName", e.Entity.Ref)
		}
	}
	if totalFields != len(reg.Properties()) {
		t.Fatalf("total generated fields = %d, want %d properties", totalFields, len(reg.Properties()))
	}
	if len(ms.Relationships) != len(reg.Relationships()) {
		t.Fatalf("Relationships len = %d, want %d", len(ms.Relationships), len(reg.Relationships()))
	}
}

// TestBuildRealCatalogApprovalBindingWritesNotImmutable proves the
// isImmutableWrite rule does not flag ApprovalBinding's own IMMUTABLE,
// IMMUTABLE_NO_CORRECTION properties: approveProposalBinding and
// rejectProposalBinding in
// [github.com/monstercameron/human-capital-management-suite/internal/intent/definitions] legitimately
// write approval_binding.decision et al., and a false positive here would
// break MSRC-009's "exercise the real fourteen definitions" integration.
func TestBuildRealCatalogApprovalBindingWritesNotImmutable(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("model.Catalog: %v", err)
	}
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantWritable := map[string]bool{
		"approval_binding.decision":                 false,
		"approval_binding.approved_proposal_digest": false,
		"approval_binding.reason_ref":               false,
		"proposal_revision.material_digest":         true,
	}
	found := map[string]bool{}
	for _, e := range ms.Entities {
		for _, f := range e.Fields {
			ref := string(f.Property.Ref)
			if wantImmutable, ok := wantWritable[ref]; ok {
				found[ref] = true
				if f.Immutable != wantImmutable {
					t.Errorf("property %s Immutable = %t, want %t", ref, f.Immutable, wantImmutable)
				}
			}
		}
	}
	for ref := range wantWritable {
		if !found[ref] {
			t.Errorf("property %s not found in generated fields", ref)
		}
	}
}

func TestBuildUnknownGoTypeFails(t *testing.T) {
	reg := syntheticRegistry(t, model.PropertyDefinition{
		Ref:               "thing.bad",
		Entity:            model.EntityRef{Name: "Thing", Version: 1},
		GoType:            "map[string]any",
		SchemaPath:        "hcmnext.test.v1.Thing.bad",
		Presence:          model.PresenceRequired,
		Classification:    model.ClassInternal,
		Temporal:          model.TemporalPointInTime,
		AuthorityRef:      "authority.thing/v1",
		Correction:        model.CorrectionAppends,
		RetentionClassRef: "TEST_CLASS",
		Status:            model.StatusActive,
	})
	if _, err := Build(reg); err == nil {
		t.Fatal("Build succeeded on an unrecognized GoType; want an error")
	} else if !strings.Contains(err.Error(), "bad") {
		t.Fatalf("error %v does not name the offending property", err)
	}
}

func TestBuildKnownAtAndRecordedAt(t *testing.T) {
	reg := syntheticRegistry(t)
	ms, err := Build(reg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(ms.Entities) != 1 {
		t.Fatalf("Entities len = %d, want 1", len(ms.Entities))
	}
	fieldsByRef := map[string]FieldModel{}
	for _, f := range ms.Entities[0].Fields {
		fieldsByRef[string(f.Property.Ref)] = f
	}
	known, ok := fieldsByRef["thing.known_at"]
	if !ok || known.Type.kind != kindKnownAt {
		t.Fatalf("thing.known_at not resolved to kindKnownAt: %+v ok=%v", known, ok)
	}
	recorded, ok := fieldsByRef["thing.recorded_at"]
	if !ok || recorded.Type.kind != kindRecordedAt {
		t.Fatalf("thing.recorded_at not resolved to kindRecordedAt: %+v ok=%v", recorded, ok)
	}
}

func TestBuildNilRegistry(t *testing.T) {
	if _, err := Build(nil); err == nil {
		t.Fatal("Build(nil) succeeded; want an error")
	}
}

func TestEntityTypeNameVersioning(t *testing.T) {
	if got := entityTypeName(model.EntityRef{Name: "Thing", Version: 1}); got != "Thing" {
		t.Errorf("entityTypeName v1 = %q, want %q", got, "Thing")
	}
	if got := entityTypeName(model.EntityRef{Name: "Thing", Version: 2}); got != "ThingV2" {
		t.Errorf("entityTypeName v2 = %q, want %q", got, "ThingV2")
	}
}

func TestIsImmutableWrite(t *testing.T) {
	evidence := model.EntityDefinition{Class: model.ClassEvidence}
	root := model.EntityDefinition{Class: model.ClassAggregateRoot}
	immutableProp := model.PropertyDefinition{Temporal: model.TemporalImmutable, Correction: model.CorrectionNoCorrection}
	mutableProp := model.PropertyDefinition{Temporal: model.TemporalEffectiveDated, Correction: model.CorrectionSupersedes}

	if isImmutableWrite(evidence, immutableProp) {
		t.Error("an EVIDENCE-class entity's IMMUTABLE_NO_CORRECTION property must not be flagged immutable-for-write")
	}
	if !isImmutableWrite(root, immutableProp) {
		t.Error("a non-EVIDENCE entity's IMMUTABLE_NO_CORRECTION/IMMUTABLE property must be flagged immutable-for-write")
	}
	if isImmutableWrite(root, mutableProp) {
		t.Error("an EFFECTIVE_DATED/SUPERSEDES property must not be flagged immutable-for-write")
	}
}
