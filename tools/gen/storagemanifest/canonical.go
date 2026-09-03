package storagemanifest

import (
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// CanonicalPropertyClosure is the read-only conformance check joining the
// canonical Go property registry to its physical manifests.  The manifests
// are deliberately inputs: this check never regenerates or edits them.
// MISMATCH dispositions are valid (they are an explicit migration gap), but
// an unaccounted-for or ambiguous disposition is not.
func CanonicalPropertyClosure(reg *model.Registry, properties PropertyMappingManifest, dispositions DispositionManifest) []error {
	if reg == nil {
		return []error{fmt.Errorf("canonical property closure: nil registry")}
	}
	want, err := BuildPropertyMappings(reg)
	if err != nil {
		return []error{fmt.Errorf("canonical property closure: build canonical SQL mappings: %w", err)}
	}
	var errs []error
	if properties.RegistryDigest != reg.Digest() {
		errs = append(errs, fmt.Errorf("SQL manifest registry digest %q does not match canonical registry %q", properties.RegistryDigest, reg.Digest()))
	}
	if dispositions.RegistryDigest != reg.Digest() {
		errs = append(errs, fmt.Errorf("disposition manifest registry digest %q does not match canonical registry %q", dispositions.RegistryDigest, reg.Digest()))
	}
	seen := map[string]bool{}
	got := map[string]PropertySQLMapping{}
	for _, p := range properties.Properties {
		if seen[p.PropertyRef] {
			errs = append(errs, fmt.Errorf("property %s appears more than once in SQL manifest", p.PropertyRef))
		}
		seen[p.PropertyRef] = true
		got[p.PropertyRef] = p
	}
	wantByRef := map[string]PropertySQLMapping{}
	for _, p := range want.Properties {
		wantByRef[p.PropertyRef] = p
	}
	for ref, p := range wantByRef {
		actual, ok := got[ref]
		if !ok {
			errs = append(errs, fmt.Errorf("property %s has no SQL mapping", ref))
			continue
		}
		if actual.Entity != p.Entity || actual.NotNull != p.NotNull || actual.DefaultBehavior != p.DefaultBehavior || actual.EncryptionPolicy != p.EncryptionPolicy || actual.SearchPolicy != p.SearchPolicy || actual.ConstraintNote != p.ConstraintNote || !sameColumns(actual.Columns, p.Columns) {
			errs = append(errs, fmt.Errorf("property %s SQL mapping drifts from canonical semantics", ref))
		}
		if _, err := reg.ResolveProperty(model.PropertyRef(ref)); err != nil {
			errs = append(errs, fmt.Errorf("property %s has incomplete owner/authority/retention lineage: %w", ref, err))
		}
	}
	for ref := range got {
		if _, ok := wantByRef[ref]; !ok {
			errs = append(errs, fmt.Errorf("SQL manifest contains unregistered property %s", ref))
		}
	}
	dispByEntity := map[string]EntityDisposition{}
	for _, d := range dispositions.Entities {
		if _, dup := dispByEntity[d.EntityRef]; dup {
			errs = append(errs, fmt.Errorf("entity %s appears more than once in disposition manifest", d.EntityRef))
		}
		dispByEntity[d.EntityRef] = d
	}
	wantEntities := map[string]model.EntityDefinition{}
	for _, e := range reg.Entities() {
		wantEntities[e.Ref.String()] = e
	}
	for ref, d := range dispByEntity {
		e, ok := wantEntities[ref]
		if !ok {
			errs = append(errs, fmt.Errorf("disposition manifest contains unregistered entity %s", ref))
			continue
		}
		if d.Key != e.Key || d.Owner != e.OwnerDomain || d.Class != string(e.Class) {
			errs = append(errs, fmt.Errorf("entity %s disposition identity drifts from canonical registry", ref))
		}
	}
	for ref := range wantEntities {
		if _, ok := dispByEntity[ref]; !ok {
			errs = append(errs, fmt.Errorf("entity %s has no disposition row", ref))
		}
	}
	for _, p := range want.Properties {
		entity := p.Entity
		d, ok := dispByEntity[entity]
		if !ok {
			errs = append(errs, fmt.Errorf("property %s has no authoritative disposition for %s", p.PropertyRef, entity))
			continue
		}
		if d.Disposition == DispositionMismatch && d.MismatchDetail == "" {
			errs = append(errs, fmt.Errorf("entity %s reports MISMATCH without a migration-gap reason", entity))
		}
	}
	return stableErrors(errs)
}

func sameColumns(a, b []ColumnMapping) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stableErrors(errs []error) []error {
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errs
}
