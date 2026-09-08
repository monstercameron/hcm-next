package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-109: the authorized People directory. The
// directory pipeline is authorized end to end, but its sort
// travels as raw request strings through an unexported
// matcher: a typo'd sort field silently becomes name order
// by accident rather than by contract, and nothing names the
// directory's sort vocabulary. The compiler needs the typed
// contract — one enum per sort field and direction, with the
// documented name/ascending defaults — governing the real
// sort path by delegation.
func TestTodo_WEB_109(t *testing.T) {
	people := []Person{
		{ID: "p-c", Name: "Cara", Role: "Engineer", Team: "Beta", Manager: "Zed", Location: "NYC"},
		{ID: "p-a", Name: "Amy", Role: "Manager", Team: "Alpha", Manager: "Yol", Location: "SF"},
		{ID: "p-b", Name: "Bob", Role: "Engineer", Team: "Alpha", Manager: "Amy", Location: "NYC"},
	}
	byName := SortPeopleDirectory(people, PeopleSortName, PeopleSortAscending)
	if len(byName) != 3 || byName[0].ID != "p-a" || byName[1].ID != "p-b" || byName[2].ID != "p-c" {
		t.Fatalf("name ascending = %+v", byName)
	}
	byTeamDesc := SortPeopleDirectory(people, PeopleSortTeam, PeopleSortDescending)
	if len(byTeamDesc) != 3 || byTeamDesc[0].ID != "p-c" || byTeamDesc[1].ID != "p-a" || byTeamDesc[2].ID != "p-b" {
		t.Fatalf("team descending = %+v", byTeamDesc)
	}

	// Request strings parse to the typed contract with the
	// documented defaults.
	field, direction := ParsePeopleSort("", "")
	if field != PeopleSortName || direction != PeopleSortAscending {
		t.Fatal("empty request is not name ascending")
	}
	field, direction = ParsePeopleSort("team", "desc")
	if field != PeopleSortTeam || direction != PeopleSortDescending {
		t.Fatal("team/desc does not parse")
	}
	field, direction = ParsePeopleSort("bogus", "sideways")
	if field != PeopleSortName || direction != PeopleSortAscending {
		t.Fatal("bogus request escapes the defaults")
	}

	// The directory path delegates: identical outcomes.
	for _, raw := range [][2]string{{"", ""}, {"role", "asc"}, {"manager", "desc"}, {"location", "desc"}, {"bogus", "bogus"}} {
		field, direction := ParsePeopleSort(raw[0], raw[1])
		if !reflect.DeepEqual(sortedPeople(people, raw[0], raw[1]), SortPeopleDirectory(people, field, direction)) {
			t.Fatalf("directory path ungoverned for %q", raw)
		}
	}
}

// Golden: typed directory orders over field/direction pairs.
func TestTodo_WEB_109_Golden(t *testing.T) {
	people := []Person{
		{ID: "p-c", Name: "Cara", Team: "Beta"},
		{ID: "p-a", Name: "Amy", Team: "Alpha"},
		{ID: "p-b", Name: "Bob", Team: "Alpha"},
	}
	pairs := [][2]string{{"", ""}, {"name", "desc"}, {"team", "asc"}, {"role", "desc"}, {"bogus", "bogus"}}
	var builder strings.Builder
	for _, pair := range pairs {
		field, direction := ParsePeopleSort(pair[0], pair[1])
		for _, person := range SortPeopleDirectory(people, field, direction) {
			builder.WriteString(person.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "18a0c622ebf3b3e2c95b4d62fac81c53f8ae5cf0fc5c0f9e321ccc4bde76e52a"
	if got != want {
		t.Fatalf("directory digest = %s, want %s", got, want)
	}
}

// Browser: directory orders over person patterns are
// deterministic with stable tie-breaks.
func TestTodo_WEB_109_Browser(t *testing.T) {
	patterns := [][]Person{
		{{ID: "p-1", Name: "Same", Team: "T"}, {ID: "p-2", Name: "Same", Team: "T"}},
		{{ID: "p-x", Name: "Zed", Role: "R"}, {ID: "p-y", Name: "Amy", Role: "R"}},
	}
	fields := []PeopleSortField{PeopleSortName, PeopleSortTeam, PeopleSortRole, PeopleSortManager, PeopleSortLocation}
	for _, population := range patterns {
		for _, field := range fields {
			for _, direction := range []PeopleSortDirection{PeopleSortAscending, PeopleSortDescending} {
				first := SortPeopleDirectory(population, field, direction)
				second := SortPeopleDirectory(population, field, direction)
				if !reflect.DeepEqual(first, second) {
					t.Fatal("directory order is nondeterministic")
				}
				if len(first) != len(population) {
					t.Fatal("directory order drops people")
				}
			}
		}
	}
}

// Conformance: typed orders equal the legacy string orders
// pair-for-pair; stability.
func TestTodo_WEB_109_Conformance(t *testing.T) {
	people := []Person{
		{ID: "p-c", Name: "Cara", Role: "Engineer", Team: "Beta", Manager: "Zed", Location: "NYC"},
		{ID: "p-a", Name: "Amy", Role: "Manager", Team: "Alpha", Manager: "Yol", Location: "SF"},
	}
	pairs := [][2]string{{"name", "asc"}, {"role", "desc"}, {"team", "asc"}, {"manager", "desc"}, {"location", "asc"}}
	for _, pair := range pairs {
		field, direction := ParsePeopleSort(pair[0], pair[1])
		typed := SortPeopleDirectory(people, field, direction)
		if !reflect.DeepEqual(typed, sortedPeople(people, pair[0], pair[1])) {
			t.Fatalf("typed order differs for %q", pair)
		}
	}
	if !reflect.DeepEqual(
		SortPeopleDirectory(people, PeopleSortLocation, PeopleSortAscending),
		SortPeopleDirectory(people, PeopleSortLocation, PeopleSortAscending),
	) {
		t.Fatal("directory order is unstable")
	}
}
