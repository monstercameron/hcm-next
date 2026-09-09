package schemaflux

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	compiled "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// CrossCheckCompiled compares catalog against the hand-authored compiled-in
// P1A registry in internal/intent/definitions (TOOL-002/TOOL-003, frozen and
// read-only from this package's lane) and returns one description per
// mismatch: a definition present in only one of the two sets, or one whose
// family or version disagrees between them. An empty result means the two
// catalogs name, family and version the same fourteen definitions.
//
// This function only reads internal/intent/definitions; it never regenerates
// or replaces that package. It exists so a change to either source is caught
// here rather than discovered later as two disagreeing sources of truth.
func CrossCheckCompiled(catalog *Catalog) ([]string, error) {
	generated := map[string]Definition{}
	for _, d := range catalog.Definitions {
		generated[d.IntentTypeID] = d
	}

	compiledDefs := compiled.All()
	compiledByID := map[string]intent.Definition{}
	for _, d := range compiledDefs {
		compiledByID[d.Ref.TypeID] = d
	}

	var mismatches []string

	for id, d := range generated {
		cd, ok := compiledByID[id]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: present in the generated catalog (%s) but not in internal/intent/definitions",
				id, d.SourceFile))
			continue
		}
		if cd.Ref.Version != d.Version {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: version mismatch: generated=%d compiled=%d", id, d.Version, cd.Ref.Version))
		}
		if cd.Family.String() != d.KernelFamily {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: kernel_family mismatch: generated=%s compiled=%s", id, d.KernelFamily, cd.Family.String()))
		}
		if cd.DisplayName != d.DisplayName {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: display_name mismatch: generated=%q compiled=%q", id, d.DisplayName, cd.DisplayName))
		}
	}

	for id, cd := range compiledByID {
		if _, ok := generated[id]; !ok {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s: present in internal/intent/definitions but not in the generated catalog", id))
			_ = cd
		}
	}

	sort.Strings(mismatches)
	return mismatches, nil
}
