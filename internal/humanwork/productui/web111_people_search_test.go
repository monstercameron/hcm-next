package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-111: people-search enumeration leakage. The
// directory browse path legitimately lists the admitted
// population, but the search path shares its matcher: a
// blank query returns everyone, so typing nothing (or
// paging an empty search) enumerates every admitted worker.
// The compiler needs the separated search — blank queries
// match nothing, never the population — reusing the
// directory's own normalized index and narrowing facets.
func TestTodo_WEB_111(t *testing.T) {
	population := []Person{
		{ID: "p-a", Name: "Amy", Team: "Alpha", Location: "SF"},
		{ID: "p-b", Name: "Bob", Team: "Beta", Location: "NYC"},
		{ID: "p-c", Name: "Cara", Team: "Alpha", Location: "NYC"},
	}
	if got := SearchPeopleDirectory(population, BuildPeopleQuery("", "", "", "", "", 1, 10)); len(got) != 0 {
		t.Fatalf("blank search enumerates: %+v", got)
	}
	if got := SearchPeopleDirectory(population, BuildPeopleQuery("", "Alpha", "", "", "", 1, 10)); len(got) != 0 {
		t.Fatalf("facet-only search enumerates: %+v", got)
	}
	got := SearchPeopleDirectory(population, BuildPeopleQuery("am", "", "", "", "", 1, 10))
	if len(got) != 1 || got[0].ID != "p-a" {
		t.Fatalf("substring search = %+v", got)
	}
	got = SearchPeopleDirectory(population, BuildPeopleQuery("a", "Alpha", "", "", "", 1, 10))
	var ids []string
	for _, person := range got {
		ids = append(ids, person.ID)
	}
	if !reflect.DeepEqual(ids, []string{"p-a", "p-c"}) {
		t.Fatalf("faceted search = %+v", got)
	}
	if len(SearchPeopleDirectory(nil, BuildPeopleQuery("am", "", "", "", "", 1, 10))) != 0 {
		t.Fatal("nil population matches")
	}
}

// Golden: search outcomes over query/population pairs.
func TestTodo_WEB_111_Golden(t *testing.T) {
	population := []Person{
		{ID: "p-a", Name: "Amy", Team: "Alpha"},
		{ID: "p-b", Name: "Bob", Team: "Beta"},
	}
	queries := [][3]string{
		{"", "", ""},
		{"am", "", ""},
		{"b", "Beta", ""},
		{"", "Alpha", ""},
		{"zzz", "", ""},
	}
	var builder strings.Builder
	for _, parts := range queries {
		for _, person := range SearchPeopleDirectory(population, BuildPeopleQuery(parts[0], parts[1], parts[2], "", "", 1, 10)) {
			builder.WriteString(person.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "e01e3abff0c0092deda43e13fca158a4fe7708615e0faad58c1918755f26ab83"
	if got != want {
		t.Fatalf("search digest = %s, want %s", got, want)
	}
}

// Browser: searches over populations keep admission order
// deterministically and never exceed the population.
func TestTodo_WEB_111_Browser(t *testing.T) {
	populations := [][]Person{
		{{ID: "p-1", Name: "Ann"}, {ID: "p-2", Name: "Andrew"}, {ID: "p-3", Name: "Bob"}},
		{{ID: "p-x", Name: "Xed", Team: "T1"}, {ID: "p-y", Name: "Yol", Team: "T2"}},
	}
	queries := []PeopleQuery{
		BuildPeopleQuery("an", "", "", "", "", 1, 10),
		BuildPeopleQuery("o", "T2", "", "", "", 1, 10),
		BuildPeopleQuery("", "", "", "", "", 1, 10),
	}
	for _, population := range populations {
		for _, query := range queries {
			first := SearchPeopleDirectory(population, query)
			second := SearchPeopleDirectory(population, query)
			if len(first) > len(population) {
				t.Fatal("search exceeds its population")
			}
			previous := -1
			for _, person := range first {
				current := -1
				for i, candidate := range population {
					if candidate.ID == person.ID {
						current = i
						break
					}
				}
				if current < 0 || current < previous {
					t.Fatalf("admission order broken: %+v", first)
				}
				previous = current
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("search is nondeterministic")
			}
		}
	}
}

// Conformance: every hit contains the query; blank never
// hits; results stay inside the population.
func TestTodo_WEB_111_Conformance(t *testing.T) {
	population := []Person{
		{ID: "p-a", Name: "Amy Archer", Team: "Alpha"},
		{ID: "p-b", Name: "Bob", Team: "Beta"},
	}
	hits := SearchPeopleDirectory(population, BuildPeopleQuery("archer", "", "", "", "", 1, 10))
	if len(hits) != 1 || hits[0].ID != "p-a" {
		t.Fatalf("search misses: %+v", hits)
	}
	if !strings.Contains(normalizedPerson(hits[0]).search, "archer") {
		t.Fatal("hit does not contain its query")
	}
	inPopulation := false
	for _, person := range population {
		if person.ID == hits[0].ID {
			inPopulation = true
		}
	}
	if !inPopulation {
		t.Fatal("hit escapes its population")
	}
	for _, blank := range []PeopleQuery{
		BuildPeopleQuery("", "", "", "", "", 1, 10),
		BuildPeopleQuery("   ", "", "", "", "", 1, 10),
	} {
		if len(SearchPeopleDirectory(population, blank)) != 0 {
			t.Fatal("blank query hits")
		}
	}
}
