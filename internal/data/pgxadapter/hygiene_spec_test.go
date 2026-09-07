package pgxadapter

import (
	"strings"
	"testing"
)

// DISCARD ALL cannot run inside the implicit transaction that a
// multi-statement simple-protocol message opens, so the hygiene statement
// spells out its components. This pins that every component PostgreSQL
// documents for DISCARD ALL is present, so the single round trip is not a
// weaker reset than the three it replaced.
func TestHygieneSQLSpellsOutEveryDiscardAllComponent(t *testing.T) {
	components := []string{
		"CLOSE ALL",
		"SET SESSION AUTHORIZATION DEFAULT",
		"RESET ALL",
		"DEALLOCATE ALL",
		"UNLISTEN *",
		"SELECT pg_advisory_unlock_all()",
		"DISCARD PLANS",
		"DISCARD TEMPORARY",
		"DISCARD SEQUENCES",
		// Not part of DISCARD ALL, but required because SET ROLE survives it.
		"RESET ROLE",
	}
	for _, component := range components {
		if !strings.Contains(hygieneSQL, component) {
			t.Errorf("hygiene statement lacks %q", component)
		}
	}
	if strings.Contains(hygieneSQL, "DISCARD ALL") {
		t.Error("hygiene statement contains DISCARD ALL, which PostgreSQL refuses inside the multi-statement message")
	}
}
