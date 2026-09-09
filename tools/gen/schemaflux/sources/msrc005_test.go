package sources_test

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

// TestTodo_MSRC_005 is the MSRC-005 primary test: it compiles the checked-in
// connectivity/assurance/operations/dataops family sources and asserts that
// ConnectorOperation — the one entity in this cluster
// internal/intent/model.Catalog() already registers — round-trips with zero
// cross-check mismatches, while the RED-named categories (connector,
// message, artifact, privacy, incident, recovery, configuration, import)
// each have at least one encoded entity so none of them "exists only in
// prose" any longer.
func TestTodo_MSRC_005(t *testing.T) {
	manifest, bundle := compileValid(t)

	connOp := findEntity(t, bundle, "ConnectorOperation")
	if connOp.Family != "connectivity" {
		t.Errorf("ConnectorOperation.Family = %q, want connectivity", connOp.Family)
	}
	if !connOp.Covered {
		t.Error("ConnectorOperation: covered = false, want true")
	}

	families := map[string]bool{"connectivity": true, "assurance": true, "operations": true, "dataops": true}
	var names []string
	for _, e := range bundle.Entities {
		if families[e.Family] {
			names = append(names, e.Name)
		}
	}
	joined := strings.Join(names, " ")
	categories := map[string]string{
		"connector":     "ConnectorDefinition",
		"message":       "MessageIntent",
		"artifact":      "ArtifactObject",
		"privacy":       "ProcessingAuthority",
		"incident":      "IncidentSignal",
		"recovery":      "RecoveryContract",
		"configuration": "ConfigurationPackage",
		"import":        "ImportJob",
	}
	for category, entity := range categories {
		if !strings.Contains(joined, entity) {
			t.Errorf("category %q has no representative entity %q among %v", category, entity, names)
		}
	}

	mismatches, err := sources.CrossCheckModel(manifest)
	if err != nil {
		t.Fatalf("CrossCheckModel: %v", err)
	}
	for _, m := range mismatches {
		t.Errorf("cross-check mismatch: %s", m)
	}
}

// TestTodo_MSRC_005_Golden pins the exact sorted entity-name list for each of
// the four families.
func TestTodo_MSRC_005_Golden(t *testing.T) {
	bundle := loadValidBundle(t)

	cases := []struct {
		family string
		want   []string
	}{
		{"connectivity", []string{
			"ArtifactObject", "ConnectorConnection", "ConnectorDefinition", "ConnectorHealth",
			"ConnectorOperation", "DeliveryAttempt", "Document", "EventSubscription",
			"ExternalConflict", "IntegrationReceipt", "MappingProfile", "MessageIntent",
			"SyncJob", "WebhookReceipt",
		}},
		{"assurance", []string{
			"Consent", "DataCopy", "DataSubjectRequest", "DeletionPlan", "LegalHold",
			"PrivacyBreachAssessment", "ProcessingAuthority", "RecordDeclaration", "RetentionSchedule",
		}},
		{"operations", []string{
			"BackupPolicy", "DriftFinding", "IncidentSignal", "ReconciliationRun",
			"RecoveryContract", "RepairExecution", "RepairPlanRevision", "RepairVerification",
		}},
		{"dataops", []string{
			"BatchOperation", "ConfigurationPackage", "ConfigurationPublication", "ExportDefinition",
			"ImportJob", "ImportRun", "ReferenceConcept", "Schema", "SchemaRelease", "StagedRecord",
			"SystemComparison",
		}},
	}
	for _, c := range cases {
		var got []string
		for _, e := range entitiesInFamily(bundle, c.family) {
			got = append(got, e.Name)
		}
		sort.Strings(got)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		assertStringSlicesEqual(t, c.family, got, want)
	}
}

// TestTodo_MSRC_005_Integration loads and compiles every family file, the
// registries, and the metamodel together as one bundle (proving cross-file
// reference resolution: e.g. ConnectorOperation's authority resolves from
// registries.yaml, and the ManagerRelationship/AssignmentPosition
// relationships declared in people.yaml resolve entities declared in
// people.yaml and position.yaml respectively), then writes and re-reads the
// compiled manifest at definitions/generation/model-sources.yaml exactly as
// the MSRC-001 pipeline does for its own manifest.
func TestTodo_MSRC_005_Integration(t *testing.T) {
	root := findRepoRoot(t)
	manifest, _ := compileValid(t)

	if len(manifest.Entities) < 100 {
		t.Fatalf("compiled manifest has only %d entities; the multi-family load appears incomplete", len(manifest.Entities))
	}
	if len(manifest.Relationships) == 0 {
		t.Fatal("compiled manifest has zero relationships")
	}
	if len(manifest.Authorities) == 0 || len(manifest.Retentions) == 0 {
		t.Fatal("compiled manifest has zero authorities or retention classes")
	}

	out := filepath.Join(root, "definitions", "generation", "model-sources.yaml")
	if err := sources.WriteFile(out, manifest.Relativize(root).YAML()); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back %s: %v", out, err)
	}
	text := string(written)
	if !strings.Contains(text, "entities: 114") && !strings.Contains(text, "entities:") {
		t.Error("written manifest does not report an entity count")
	}
	if !strings.Contains(text, "ConnectorOperation/v1") {
		t.Error("written manifest does not mention ConnectorOperation/v1")
	}
}

// TestTodo_MSRC_005_Fault is the MSRC-005 RED table over the connectivity/
// operations/dataops planes: an unresolved relationship endpoint, an
// unresolved authority, and an unresolved child reference must each produce
// a typed [sources.UnresolvedReferenceError], matching the "connector...
// object exists only in prose" RED clause's underlying requirement that
// these objects have real, checkable structure rather than accepting
// anything.
func TestTodo_MSRC_005_Fault(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*sources.Bundle)
		wantKind sources.ReferenceKind
	}{
		{
			name: "connector_operation_authority_unresolved",
			mutate: func(b *sources.Bundle) {
				mutateFirstProperty(b, "ConnectorOperation", func(p *sources.PropertySource) {
					p.AuthorityRef = "authority.does_not_exist/v1"
				})
			},
			wantKind: sources.ReferenceKindAuthority,
		},
		{
			name: "import_job_retention_unresolved",
			mutate: func(b *sources.Bundle) {
				mutateFirstProperty(b, "ImportJob", func(p *sources.PropertySource) {
					p.RetentionClassRef = "NO_SUCH_RETENTION_CLASS"
				})
			},
			wantKind: sources.ReferenceKindRetention,
		},
		{
			name: "relationship_target_unresolved",
			mutate: func(b *sources.Bundle) {
				for i := range b.Relationships {
					if b.Relationships[i].Name == "PositionOccupant" {
						b.Relationships[i].TargetEntity = "NoSuchEntity/v1"
					}
				}
			},
			wantKind: sources.ReferenceKindEntity,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bundle := loadValidBundle(t)
			tc.mutate(&bundle)
			_, errs := sources.Compile(bundle)
			if len(errs) == 0 {
				t.Fatal("Compile returned zero errors, want at least one")
			}
			var found bool
			for _, err := range errs {
				var ref *sources.UnresolvedReferenceError
				if errors.As(err, &ref) && ref.Kind == tc.wantKind {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s error among: %v", tc.wantKind, errs)
			}
		})
	}
}

// TestTodo_MSRC_005_Security asserts a privacy-relevant classification
// invariant over the assurance family: every property of Consent and
// DataSubjectRequest — entities whose entire purpose is handling personal
// data subject to erasure/access/portability rights — must be classified at
// PII or above, never PUBLIC or bare INTERNAL. This is the concrete,
// checkable form of "privacy... object exists only in prose" no longer being
// true: the encoded source also carries the classification floor a real
// implementation would need to enforce.
func TestTodo_MSRC_005_Security(t *testing.T) {
	bundle := loadValidBundle(t)
	weak := map[string]bool{"PUBLIC": true, "INTERNAL": true}
	for _, name := range []string{"Consent", "DataSubjectRequest"} {
		e := findEntity(t, bundle, name)
		if len(e.Properties) == 0 {
			t.Errorf("%s declares no properties to classify", name)
		}
		for _, p := range e.Properties {
			if weak[p.Classification] {
				t.Errorf("%s.%s: classification %q is weaker than PII", name, p.Name, p.Classification)
			}
		}
	}
}

// TestTodo_MSRC_005_Mutation mutates ConnectorOperation's sole ACTIVE
// property and asserts [sources.CrossCheckModel] reports the exact expected
// mismatch, mirroring TestTodo_MSRC_003_Mutation's pattern for this family
// cluster's one Go-registered entity.
func TestTodo_MSRC_005_Mutation(t *testing.T) {
	bundle := loadValidBundle(t)
	mutateFirstProperty(&bundle, "ConnectorOperation", func(p *sources.PropertySource) {
		p.RetentionClassRef = "GOVERNANCE_EVIDENCE" // real model.go value is OPERATIONAL_EVIDENCE
	})
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		t.Fatalf("Compile of the mutated bundle returned errors: %v", errs)
	}
	mismatches, err := sources.CrossCheckModel(manifest)
	if err != nil {
		t.Fatalf("CrossCheckModel: %v", err)
	}
	found := false
	for _, m := range mismatches {
		if containsAll(m, "ConnectorOperation/v1", "watermark", "retention_class_ref mismatch") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a watermark retention_class_ref mismatch among: %v", mismatches)
	}
}
