package schemaflux

import (
	"fmt"
	"sort"
)

// Compile resolves every symbolic reference in defs and manifest and returns
// the compiled [Catalog] plus every [UnresolvedReferenceError] found. It
// never stops at the first error: TOOL-005's RED fixtures inject more than
// one bad reference at a time in some cases, and a generator that only ever
// reports the first mistake makes fixing a batch of definitions slower than
// it needs to be.
//
// Compile returns a non-nil *Catalog even when errs is non-empty, but the
// catalog is not the publishable one in that case: "warnings cannot publish a
// P1A or P1B contract" (TOOL-005 GREEN) means a caller must treat any
// non-empty errs as "do not use this Catalog," which every caller in this
// package (WriteAll, the qualification fixture) does.
func Compile(defs []Definition, manifest []CapabilityManifestEntry) (*Catalog, []error) {
	var errs []error

	sorted := append([]Definition(nil), defs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].IntentTypeID < sorted[j].IntentTypeID })

	schemaSet := map[string]bool{}
	capSet := map[string]bool{}

	for _, d := range sorted {
		if d.IntentTypeID == "" {
			errs = append(errs, fmt.Errorf("%s:%d: definition has an empty intent_type_id", d.SourceFile, d.SourceLine))
			continue
		}
		if d.Version == 0 {
			errs = append(errs, fmt.Errorf("%s:%d: %s: version must be a positive integer", d.SourceFile, d.SourceLine, d.IntentTypeID))
		}

		if !declaredKernelFamilies[d.KernelFamily] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindFamily, SourceFile: d.SourceFile, SourceLine: d.SourceLine,
				IntentTypeID: d.IntentTypeID, Field: "kernel_family", Value: d.KernelFamily,
				Reason: "not one of the three live kernel families (CHANGE_REQUEST, CALCULATION_REQUEST, ANALYTICAL_REQUEST)",
			})
		}

		if !declaredOwnerPlanes[d.OwnerPlane] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindOwner, SourceFile: d.SourceFile, SourceLine: d.SourceLine,
				IntentTypeID: d.IntentTypeID, Field: "owner_plane", Value: d.OwnerPlane,
				Reason: "not a declared owner plane",
			})
		}
		if !declaredOwnerDomains[d.OwnerDomain] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindOwner, SourceFile: d.SourceFile, SourceLine: d.SourceLine,
				IntentTypeID: d.IntentTypeID, Field: "owner_domain", Value: d.OwnerDomain,
				Reason: "not a declared owner domain",
			})
		}

		if !looksLikeRef(d.InputSchemaRef) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindSchema, SourceFile: d.SourceFile, SourceLine: d.SourceLine,
				IntentTypeID: d.IntentTypeID, Field: "input_schema_ref", Value: d.InputSchemaRef,
				Reason: "does not parse as <protobuf full name>/v<version>",
			})
		}
		if !looksLikeRef(d.ResultSchemaRef) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindSchema, SourceFile: d.SourceFile, SourceLine: d.SourceLine,
				IntentTypeID: d.IntentTypeID, Field: "result_schema_ref", Value: d.ResultSchemaRef,
				Reason: "does not parse as <protobuf full name>/v<version>",
			})
		}

		for _, capRef := range d.RequiredCapabilities {
			if !looksLikeRef(capRef) {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindCapability, SourceFile: d.SourceFile, SourceLine: d.SourceLine,
					IntentTypeID: d.IntentTypeID, Field: "required_capabilities", Value: capRef,
					Reason: "does not parse as <capability id>/v<version>",
				})
			}
		}

		schemaSet[d.InputSchemaRef] = true
		schemaSet[d.ResultSchemaRef] = true
		for _, capRef := range d.RequiredCapabilities {
			capSet[capRef] = true
		}
	}

	for i, m := range manifest {
		if m.ID == "" {
			errs = append(errs, fmt.Errorf("%s: capability manifest entry %d has an empty id", m.SourceFile, i))
		}
		if m.Version == 0 {
			errs = append(errs, fmt.Errorf("%s: %s: capability manifest version must be a positive integer", m.SourceFile, m.ID))
		}
	}

	catalog := &Catalog{
		Definitions:        sorted,
		CapabilityManifest: append([]CapabilityManifestEntry(nil), manifest...),
		Schemas:            sortedKeys(schemaSet),
		Capabilities:       sortedKeys(capSet),
	}
	sort.Slice(catalog.CapabilityManifest, func(i, j int) bool {
		return catalog.CapabilityManifest[i].ID < catalog.CapabilityManifest[j].ID
	})

	return catalog, errs
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		if k == "" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
