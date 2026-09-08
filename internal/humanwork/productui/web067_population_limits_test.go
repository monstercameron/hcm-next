package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-067: population-query presentation limits. Discovery
// surfaces honor record verdicts, but the workforce directory — the
// population query itself — renders every projected worker regardless
// of verdicts: an undisclosable record keeps its row, its team and
// location keep their facet options, and the result counts keep denied
// workers in the total. The directory population must be limited to
// admitted records once the server speaks (a silent server keeps the
// current set), across rows, facets, and counts.
func TestTodo_WEB_067(t *testing.T) {
	home := testView(PageHome)
	all := []string{"worker-jordan", "worker-avery", "worker-elena"}
	admitted := func(view View) []string {
		people := admittedPeople(view)
		if len(people) == 0 {
			return nil
		}
		ids := make([]string, 0, len(people))
		for _, person := range people {
			ids = append(ids, person.ID)
		}
		return ids
	}
	for _, population := range []struct {
		name     string
		verdicts map[string]AuthorizedRecord
		want     []string
	}{
		{"silent server keeps the population", nil, all},
		{"silent empty map keeps the population", map[string]AuthorizedRecord{}, all},
		{"governed population drops undisclosable records", map[string]AuthorizedRecord{
			"worker-avery":  {ID: "worker-avery", Disclosable: true},
			"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
			"worker-elena":  {ID: "worker-elena", Disclosable: true},
		}, []string{"worker-avery", "worker-elena"}},
		{"records without a verdict drop once governed", map[string]AuthorizedRecord{
			"worker-avery": {ID: "worker-avery", Disclosable: true},
		}, []string{"worker-avery"}},
		{"fully denied population is empty", map[string]AuthorizedRecord{
			"worker-avery":  {ID: "worker-avery", Disclosable: false},
			"worker-jordan": {ID: "worker-jordan", Disclosable: false},
			"worker-elena":  {ID: "worker-elena", Disclosable: false},
		}, nil},
	} {
		view := home
		view.RecordVerdicts = population.verdicts
		if got := admitted(view); !reflect.DeepEqual(got, population.want) {
			t.Fatalf("%s = %q, want %q", population.name, got, population.want)
		}
	}
	people := append([]Person(nil), home.People...)
	governed := View{People: people, RecordVerdicts: map[string]AuthorizedRecord{"worker-avery": {ID: "worker-avery", Disclosable: true}}}
	_ = admittedPeople(governed)
	if !reflect.DeepEqual(people, home.People) {
		t.Fatal("population limiting mutates its inputs")
	}

	// The governed directory drops the denied worker's row, facet
	// options, and counts — admitted workers stay.
	doc, err := Render(web067View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	body := web063BodyText(t, doc)
	for _, leaked := range []string{"Jordan Lee", "Strategy", "New York"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("governed directory leaks %q", leaked)
		}
	}
	for _, want := range []string{"Avery Patel", "Elena Ruiz"} {
		if !strings.Contains(body, want) {
			t.Fatalf("governed directory loses admitted %q", want)
		}
	}
}

// Golden: the population-limit matrix digest (variant → admitted IDs).
func TestTodo_WEB_067_Golden(t *testing.T) {
	home := testView(PageHome)
	variants := map[string]map[string]AuthorizedRecord{
		"silent": nil,
		"all": {"worker-avery": {ID: "worker-avery", Disclosable: true},
			"worker-jordan": {ID: "worker-jordan", Disclosable: true},
			"worker-elena":  {ID: "worker-elena", Disclosable: true}},
		"jordan-hidden": {"worker-avery": {ID: "worker-avery", Disclosable: true},
			"worker-jordan": {ID: "worker-jordan", Disclosable: false},
			"worker-elena":  {ID: "worker-elena", Disclosable: true}},
		"avery-only": {"worker-avery": {ID: "worker-avery", Disclosable: true}},
		"none": {"worker-avery": {ID: "worker-avery", Disclosable: false},
			"worker-jordan": {ID: "worker-jordan", Disclosable: false},
			"worker-elena":  {ID: "worker-elena", Disclosable: false}},
	}
	var builder strings.Builder
	for _, name := range []string{"silent", "all", "jordan-hidden", "avery-only", "none"} {
		view := home
		view.RecordVerdicts = variants[name]
		ids := make([]string, 0)
		for _, person := range admittedPeople(view) {
			ids = append(ids, person.ID)
		}
		builder.WriteString(name)
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(ids, ","))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "2f1ac4a483f8f94a7fe1d32f75d91e6947f8154e3642e46cdcc69f86bed4cf2f"
	if got != want {
		t.Fatalf("population matrix digest = %s, want %s", got, want)
	}
}

// Browser: the governed directory parses with exactly the admitted
// rows, and no positive tabindex stops.
func TestTodo_WEB_067_Browser(t *testing.T) {
	for _, browser := range []struct {
		name string
		view View
		rows int
	}{
		{"governed", web067View("en-US"), 2},
		{"silent", testView(PagePeople), 3},
	} {
		doc, err := Render(browser.view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if count := countClassTokens(root, "people-row"); count != browser.rows {
			t.Fatalf("%s directory renders %d rows, want %d", browser.name, count, browser.rows)
		}
		var positive int
		var walk func(node *xhtml.Node)
		walk = func(node *xhtml.Node) {
			if node.Type == xhtml.ElementNode {
				for _, attr := range node.Attr {
					if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
						positive++
					}
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(root)
		if positive != 0 {
			t.Fatalf("%s directory carries %d positive tabindex stops", browser.name, positive)
		}
	}
}

// Conformance: no unresolved copy in three locales, governed
// all-disclosable renders identically to a silent server, determinism.
func TestTodo_WEB_067_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		doc, err := Render(web067View(locale))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("%s governed directory leaks an unresolved key", locale)
		}
		silent := testView(PagePeople)
		silent.Locale = ResolveProductLocale(locale)
		silent = ApplyLocale(silent, silent.Locale)
		silentDoc, err := Render(silent)
		if err != nil {
			t.Fatal(err)
		}
		allowed := silent
		allowed.RecordVerdicts = map[string]AuthorizedRecord{
			"worker-avery":  {ID: "worker-avery", Disclosable: true},
			"worker-jordan": {ID: "worker-jordan", Disclosable: true},
			"worker-elena":  {ID: "worker-elena", Disclosable: true},
		}
		allowedDoc, err := Render(allowed)
		if err != nil {
			t.Fatal(err)
		}
		if silentDoc != allowedDoc {
			t.Fatalf("%s all-disclosable directory differs from the silent server", locale)
		}
	}
	view := web067View("en-US")
	first := admittedPeople(view)
	second := admittedPeople(view)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("population limiting is nondeterministic")
	}
}

// Security: denied population details never reach the markup in any
// catalog locale; admitted workers are never over-withheld.
func TestTodo_WEB_067_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		doc, err := Render(web067View(locale))
		if err != nil {
			t.Fatal(err)
		}
		body := web063BodyText(t, doc)
		for _, leaked := range []string{"Jordan Lee", "Strategy", "New York"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("%s governed directory leaks %q", locale, leaked)
			}
		}
		for _, want := range []string{"Avery Patel", "Elena Ruiz"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s governed directory over-withholds %q", locale, want)
			}
		}
	}
}

func web067View(locale string) View {
	view := testView(PagePeople)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-avery":  {ID: "worker-avery", Disclosable: true},
		"worker-jordan": {ID: "worker-jordan", Disclosable: false, DenialReason: "No record access."},
		"worker-elena":  {ID: "worker-elena", Disclosable: true},
	}
	return view
}
