package sources

import (
	"fmt"
	"sort"

	model "github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// CrossCheckModel compares every covered:true entity, relationship,
// authority and retention class in manifest against the live
// internal/intent/model.Catalog() registry (MODEL-011..MODEL-030) and
// returns one description per mismatch: a covered entry with no Go
// counterpart, a Go entry this manifest does not cover, or a field that
// disagrees between the two. An empty result means every claimed coverage
// is accurate.
//
// This function only reads internal/intent/model; it never regenerates or
// replaces that package, matching tools/gen/schemaflux.CrossCheckCompiled's
// read-only posture toward internal/intent/definitions.
func CrossCheckModel(manifest *Manifest) ([]string, error) {
	reg, err := model.Catalog()
	if err != nil {
		return nil, fmt.Errorf("sources: internal/intent/model.Catalog(): %w", err)
	}

	var mismatches []string

	// --- Entities and their properties -----------------------------------
	goEntities := map[string]model.EntityDefinition{}
	for _, e := range reg.Entities() {
		goEntities[e.Ref.String()] = e
	}
	goPropsByEntity := map[string][]model.PropertyDefinition{}
	for _, p := range reg.Properties() {
		goPropsByEntity[p.Entity.String()] = append(goPropsByEntity[p.Entity.String()], p)
	}

	claimedEntities := map[string]bool{}
	for _, e := range manifest.Entities {
		if !e.Covered {
			continue
		}
		ref := e.Ref()
		claimedEntities[ref] = true
		ge, ok := goEntities[ref]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: covered:true but %s is not registered in internal/intent/model.Catalog()", e.SourceFile, ref))
			continue
		}
		if ge.Key != e.Key {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: key mismatch: source=%q model=%q", e.SourceFile, ref, e.Key, ge.Key))
		}
		if ge.OwnerDomain != e.OwnerDomain {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: owner_domain mismatch: source=%q model=%q", e.SourceFile, ref, e.OwnerDomain, ge.OwnerDomain))
		}
		if string(ge.Class) != e.Class {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: class mismatch: source=%q model=%q", e.SourceFile, ref, e.Class, ge.Class))
		}
		if string(ge.Status) != e.Status {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: status mismatch: source=%q model=%q", e.SourceFile, ref, e.Status, ge.Status))
		}
		if ge.TenantScoped != e.TenantScoped {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: tenant_scoped mismatch: source=%v model=%v", e.SourceFile, ref, e.TenantScoped, ge.TenantScoped))
		}
		if ge.LifecycleAssignment != e.LifecycleAssignment {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: lifecycle_assignment mismatch: source=%q model=%q", e.SourceFile, ref, e.LifecycleAssignment, ge.LifecycleAssignment))
		}
		if !stringSetEqual(ge.Aliases, e.Aliases) {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: aliases mismatch: source=%v model=%v", e.SourceFile, ref, e.Aliases, ge.Aliases))
		}

		// Every Go-registered ACTIVE property must appear in the source with
		// matching type/schema/presence/classification/temporal/authority/
		// correction/retention. A source may declare additional DRAFT
		// properties beyond what Go has registered — that is a planned
		// extension, not a mismatch.
		sourceByName := map[string]PropertySource{}
		for _, p := range e.Properties {
			sourceByName[p.Name] = p
		}
		for _, gp := range goPropsByEntity[ref] {
			name := gp.Ref.Path()
			sp, ok := sourceByName[name]
			if !ok {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s: %s: model property %s has no matching entry in properties[]", e.SourceFile, ref, gp.Ref))
				continue
			}
			if sp.Status != "ACTIVE" {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s: %s.%s: model registers this property ACTIVE but the source marks it %q", e.SourceFile, ref, name, sp.Status))
			}
			if sp.GoType != gp.GoType {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: go_type mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.GoType, gp.GoType))
			}
			if sp.SchemaPath != gp.SchemaPath {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: schema_path mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.SchemaPath, gp.SchemaPath))
			}
			if sp.Presence != string(gp.Presence) {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: presence mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.Presence, gp.Presence))
			}
			if sp.Classification != string(gp.Classification) {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: classification mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.Classification, gp.Classification))
			}
			if sp.Temporal != string(gp.Temporal) {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: temporal mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.Temporal, gp.Temporal))
			}
			if sp.AuthorityRef != gp.AuthorityRef {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: authority_ref mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.AuthorityRef, gp.AuthorityRef))
			}
			if sp.Correction != string(gp.Correction) {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: correction mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.Correction, gp.Correction))
			}
			if sp.RetentionClassRef != gp.RetentionClassRef {
				mismatches = append(mismatches, fmt.Sprintf("%s: %s.%s: retention_class_ref mismatch: source=%q model=%q", e.SourceFile, ref, name, sp.RetentionClassRef, gp.RetentionClassRef))
			}
		}
	}
	for ref := range goEntities {
		if !claimedEntities[ref] {
			mismatches = append(mismatches, fmt.Sprintf(
				"internal/intent/model.Catalog() registers %s but no covered:true source entity claims it", ref))
		}
	}

	// --- Relationships -----------------------------------------------------
	goRels := map[string]model.RelationshipDefinition{}
	for _, r := range reg.Relationships() {
		goRels[r.Ref.String()] = r
	}
	claimedRels := map[string]bool{}
	for _, r := range manifest.Relationships {
		if !r.Covered {
			continue
		}
		ref := r.Ref()
		claimedRels[ref] = true
		gr, ok := goRels[ref]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: covered:true but relationship %s is not registered in internal/intent/model.Catalog()", r.SourceFile, ref))
			continue
		}
		if gr.SourceEntity.String() != r.SourceEntity {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: source_entity mismatch: source=%q model=%q", r.SourceFile, ref, r.SourceEntity, gr.SourceEntity))
		}
		if gr.TargetEntity.String() != r.TargetEntity {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: target_entity mismatch: source=%q model=%q", r.SourceFile, ref, r.TargetEntity, gr.TargetEntity))
		}
		if string(gr.Cardinality) != r.Cardinality {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: cardinality mismatch: source=%q model=%q", r.SourceFile, ref, r.Cardinality, gr.Cardinality))
		}
		if gr.Exclusive != r.Exclusive {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: exclusive mismatch: source=%v model=%v", r.SourceFile, ref, r.Exclusive, gr.Exclusive))
		}
		if gr.AllowCycles != r.AllowCycles {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: allow_cycles mismatch: source=%v model=%v", r.SourceFile, ref, r.AllowCycles, gr.AllowCycles))
		}
		if gr.TenantScoped != r.TenantScoped {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: tenant_scoped mismatch: source=%v model=%v", r.SourceFile, ref, r.TenantScoped, gr.TenantScoped))
		}
	}
	for ref := range goRels {
		if !claimedRels[ref] {
			mismatches = append(mismatches, fmt.Sprintf(
				"internal/intent/model.Catalog() registers relationship %s but no covered:true source claims it", ref))
		}
	}

	// --- Authorities ---------------------------------------------------
	goAuth := map[string]model.SourceAuthorityAssignment{}
	for _, a := range reg.Authorities() {
		goAuth[a.AssignmentRef] = a
	}
	claimedAuth := map[string]bool{}
	for _, a := range manifest.Authorities {
		if !a.Covered {
			continue
		}
		claimedAuth[a.Ref] = true
		ga, ok := goAuth[a.Ref]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: covered:true but authority %s is not registered in internal/intent/model.Catalog()", a.SourceFile, a.Ref))
			continue
		}
		if string(ga.Kind) != a.Kind {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: kind mismatch: source=%q model=%q", a.SourceFile, a.Ref, a.Kind, ga.Kind))
		}
		if ga.DomainScope != a.DomainScope {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: domain_scope mismatch: source=%q model=%q", a.SourceFile, a.Ref, a.DomainScope, ga.DomainScope))
		}
		if ga.Exclusive != a.Exclusive {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: exclusive mismatch: source=%v model=%v", a.SourceFile, a.Ref, a.Exclusive, ga.Exclusive))
		}
		if ga.FreshnessSeconds != a.FreshnessSeconds {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: freshness_seconds mismatch: source=%d model=%d", a.SourceFile, a.Ref, a.FreshnessSeconds, ga.FreshnessSeconds))
		}
		if string(ga.Merge) != a.Merge {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: merge mismatch: source=%q model=%q", a.SourceFile, a.Ref, a.Merge, ga.Merge))
		}
		if ga.EvidenceRef != a.EvidenceRef {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: evidence_ref mismatch: source=%q model=%q", a.SourceFile, a.Ref, a.EvidenceRef, ga.EvidenceRef))
		}
	}
	for ref := range goAuth {
		if !claimedAuth[ref] {
			mismatches = append(mismatches, fmt.Sprintf(
				"internal/intent/model.Catalog() registers authority %s but no covered:true source claims it", ref))
		}
	}

	// --- Retention classes -----------------------------------------------
	goRet := map[string]model.RetentionClass{}
	for _, r := range reg.RetentionClasses() {
		goRet[r.ClassRef] = r
	}
	claimedRet := map[string]bool{}
	for _, r := range manifest.Retentions {
		if !r.Covered {
			continue
		}
		claimedRet[r.Ref] = true
		gr, ok := goRet[r.Ref]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: covered:true but retention class %s is not registered in internal/intent/model.Catalog()", r.SourceFile, r.Ref))
			continue
		}
		if gr.DefaultPeriodDays != r.DefaultPeriodDays {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: default_period_days mismatch: source=%d model=%d", r.SourceFile, r.Ref, r.DefaultPeriodDays, gr.DefaultPeriodDays))
		}
		if gr.TriggerEvent != r.TriggerEvent {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: trigger_event mismatch: source=%q model=%q", r.SourceFile, r.Ref, r.TriggerEvent, gr.TriggerEvent))
		}
		if gr.DispositionOwner != r.DispositionOwner {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: disposition_owner mismatch: source=%q model=%q", r.SourceFile, r.Ref, r.DispositionOwner, gr.DispositionOwner))
		}
		if gr.AuthorityRef != r.AuthorityRef {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: authority_ref mismatch: source=%q model=%q", r.SourceFile, r.Ref, r.AuthorityRef, gr.AuthorityRef))
		}
		if !uint32MapEqual(gr.JurisdictionOverrides, r.JurisdictionOverrides) {
			mismatches = append(mismatches, fmt.Sprintf("%s: %s: jurisdiction_overrides mismatch: source=%v model=%v", r.SourceFile, r.Ref, r.JurisdictionOverrides, gr.JurisdictionOverrides))
		}
	}
	for ref := range goRet {
		if !claimedRet[ref] {
			mismatches = append(mismatches, fmt.Sprintf(
				"internal/intent/model.Catalog() registers retention class %s but no covered:true source claims it", ref))
		}
	}

	sort.Strings(mismatches)
	return mismatches, nil
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return len(a) == 0 && len(b) == 0
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func uint32MapEqual(a, b map[string]uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
