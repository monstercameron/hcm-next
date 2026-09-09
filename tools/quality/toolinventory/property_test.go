package toolinventory_test

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/toolinventory"
)

// TestTodo_TOOL_025_Property proves Entry.MissingFields() is precise -
// neither over- nor under-reporting - across many pseudo-randomly chosen
// subsets of the seven required fields. The seed is fixed so any failure
// reproduces exactly.
func TestTodo_TOOL_025_Property(t *testing.T) {
	fields := []string{"version", "source", "license", "cve_status", "owner", "update_sla", "replacement_path"}
	rng := rand.New(rand.NewSource(20260903))

	for i := 0; i < 100; i++ {
		e := validEntry()
		var blanked []string
		for _, f := range fields {
			if rng.Intn(2) == 0 {
				blankField(&e, f)
				blanked = append(blanked, f)
			}
		}
		sort.Strings(blanked)

		got := e.MissingFields()
		sort.Strings(got)

		if !reflect.DeepEqual(got, blanked) {
			t.Fatalf("iteration %d: MissingFields() = %v, want %v (entry: %+v)", i, got, blanked, e)
		}

		// Cross-check against Validate: a manifest holding exactly this
		// entry is valid if and only if nothing was blanked.
		m := toolinventory.Manifest{Version: 1, Tools: []toolinventory.Entry{e}}
		err := m.Validate()
		if len(blanked) == 0 && err != nil {
			t.Fatalf("iteration %d: complete entry rejected by Validate: %v", i, err)
		}
		if len(blanked) > 0 && err == nil {
			t.Fatalf("iteration %d: incomplete entry (missing %v) accepted by Validate", i, blanked)
		}
	}
}
