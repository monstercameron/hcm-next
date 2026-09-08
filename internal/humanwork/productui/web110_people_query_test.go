package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-110: the bounded people-query builder. The
// directory query travels as seven loose request parts with
// bounds applied ad hoc at each use site — trimming here,
// page clamps there, size allowlists elsewhere — so a new
// consumer assembles an unbounded query by omission. The
// compiler needs the single governed builder: trimmed
// strings, the typed sort contract, page floors, and the
// size allowlist in one value.
func TestTodo_WEB_110(t *testing.T) {
	query := BuildPeopleQuery("  amy  ", " ENG ", "", "team", "desc", 0, 7)
	if query.Query != "amy" || query.Team != "eng" || query.Location != "" {
		t.Fatalf("query strings unnormalized: %+v", query)
	}
	if query.Sort != PeopleSortTeam || query.Direction != PeopleSortDescending {
		t.Fatalf("query sort untyped: %+v", query)
	}
	if query.Page != 1 {
		t.Fatalf("query page unfloored: %+v", query)
	}
	if query.PageSize != defaultPageSize {
		t.Fatalf("query size unallowlisted: %+v", query)
	}

	bounded := BuildPeopleQuery("", "", "", "", "", 3, 50)
	if bounded.Page != 3 || bounded.PageSize != 50 {
		t.Fatalf("valid bounds rewritten: %+v", bounded)
	}
	if bounded.Sort != PeopleSortName || bounded.Direction != PeopleSortAscending {
		t.Fatalf("empty sort escapes defaults: %+v", bounded)
	}
}

// Golden: built queries over raw request parts.
func TestTodo_WEB_110_Golden(t *testing.T) {
	parts := [][7]string{
		{"", "", "", "", "", "0", "0"},
		{" amy ", "", "", "name", "asc", "1", "10"},
		{"", "eng", "nyc", "team", "desc", "-2", "7"},
		{"x", "y", "z", "bogus", "sideways", "99", "100"},
	}
	var builder strings.Builder
	for _, part := range parts {
		var page, size int
		fmt.Sscanf(part[5], "%d", &page)
		fmt.Sscanf(part[6], "%d", &size)
		query := BuildPeopleQuery(part[0], part[1], part[2], part[3], part[4], page, size)
		fmt.Fprintf(&builder, "%s|%s|%s|%d|%d|%d|%d\x00", query.Query, query.Team, query.Location, query.Sort, query.Direction, query.Page, query.PageSize)
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "5409f435998e9ab6f792b7e5ec2e79dce017b470d4923bd2833bf9ff06d85850"
	if got != want {
		t.Fatalf("query digest = %s, want %s", got, want)
	}
}

// Browser: building over raw patterns is deterministic and
// keeps valid bounds intact.
func TestTodo_WEB_110_Browser(t *testing.T) {
	patterns := [][7]string{
		{"  q  ", "  t  ", "  l  ", "role", "desc", "2", "20"},
		{"", "", "", "", "", "1", "10"},
		{"a", "b", "c", "manager", "asc", "-5", "1000"},
	}
	for _, part := range patterns {
		var page, size int
		fmt.Sscanf(part[5], "%d", &page)
		fmt.Sscanf(part[6], "%d", &size)
		first := BuildPeopleQuery(part[0], part[1], part[2], part[3], part[4], page, size)
		second := BuildPeopleQuery(part[0], part[1], part[2], part[3], part[4], page, size)
		if !reflect.DeepEqual(first, second) {
			t.Fatal("query building is nondeterministic")
		}
		if first.Page < 1 {
			t.Fatalf("page escapes its floor: %+v", first)
		}
	}
}

// Conformance: the built sort drives the governed directory
// order; page sizes stay allowlisted; stability.
func TestTodo_WEB_110_Conformance(t *testing.T) {
	people := []Person{
		{ID: "p-b", Name: "Bob", Team: "Alpha"},
		{ID: "p-a", Name: "Amy", Team: "Beta"},
	}
	for _, raw := range [][2]string{{"name", "asc"}, {"team", "desc"}, {"bogus", "bogus"}} {
		query := BuildPeopleQuery("", "", "", raw[0], raw[1], 1, 10)
		if !reflect.DeepEqual(
			SortPeopleDirectory(people, query.Sort, query.Direction),
			sortedPeople(people, raw[0], raw[1]),
		) {
			t.Fatalf("built query diverges for %q", raw)
		}
	}
	for _, size := range []int{-100, 0, 7, 10, 20, 50, 100, 1000} {
		if BuildPeopleQuery("", "", "", "", "", 1, size).PageSize != normalizePageSize(size) {
			t.Fatalf("size %d escapes the allowlist", size)
		}
	}
	if !reflect.DeepEqual(
		BuildPeopleQuery("q", "t", "l", "role", "desc", 2, 20),
		BuildPeopleQuery("q", "t", "l", "role", "desc", 2, 20),
	) {
		t.Fatal("query building is unstable")
	}
}
