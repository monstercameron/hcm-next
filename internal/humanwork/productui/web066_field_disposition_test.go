package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-066: field-disposition renderings. The profile renders the
// four WEB-061 effects but defines no rendering for the six spec field
// dispositions (SHOW, MASK, REDACT, HIDE, SUMMARY_ONLY, DERIVED_ONLY): a
// masked salary would dump its raw value, a hidden fact would still
// occupy a row, and an unknown disposition has no fail-closed mapping.
// Every disposition needs a defined rendering — stand-ins render instead
// of raw values, HIDE omits the row entirely, unknowns withhold — wired
// through the shared person/myself profile adapter without drifting the
// legacy effect path.
func TestTodo_WEB_066(t *testing.T) {
	lc := ResolveProductLocale("en-US")
	for _, projection := range []struct {
		name         string
		value        string
		field        AuthorizedField
		wantText     string
		wantAdmitted bool
	}{
		{"show renders the value", "pos-design", AuthorizedField{Disposition: FieldShow}, "pos-design", true},
		{"show blank reads not-reported", "", AuthorizedField{Disposition: FieldShow}, "Not reported", true},
		{"mask renders the stand-in", "CAD 118,000", AuthorizedField{Disposition: FieldMask, StandIn: "Masked pay"}, "Masked pay", true},
		{"mask without a stand-in withholds", "CAD 118,000", AuthorizedField{Disposition: FieldMask, Reason: "Need to know."}, "Withheld", true},
		{"redact reads as redacted", "Avery Patel", AuthorizedField{Disposition: FieldRedact}, "Redacted", true},
		{"hide omits the row", "NW-40118", AuthorizedField{Disposition: FieldHide}, "", false},
		{"hide emits no reason", "NW-40118", AuthorizedField{Disposition: FieldHide, Reason: "Secret."}, "", false},
		{"summary renders the stand-in", "2022-04-11", AuthorizedField{Disposition: FieldSummaryOnly, StandIn: "April 2022"}, "April 2022", true},
		{"summary without a stand-in withholds", "2022-04-11", AuthorizedField{Disposition: FieldSummaryOnly}, "Withheld", true},
		{"derived renders the stand-in", "12%", AuthorizedField{Disposition: FieldDerivedOnly, StandIn: "Target on file"}, "Target on file", true},
		{"derived without a stand-in withholds", "12%", AuthorizedField{Disposition: FieldDerivedOnly, Reason: "Need to know."}, "Withheld", true},
		{"unknown disposition withholds", "CAD 118,000", AuthorizedField{Disposition: "CURTAIN", Reason: "Need to know."}, "Withheld", true},
		{"legacy allow passes through", "Avery Patel", AuthorizedField{Effect: PresentationAllow}, "Avery Patel", true},
		{"legacy denied passes through", "CAD 118,000", AuthorizedField{Effect: PresentationDenied, Reason: "Not for this purpose."}, "Unavailable", true},
	} {
		got, admitted := ProjectField(lc, projection.value, projection.field)
		if got.Text != projection.wantText || admitted != projection.wantAdmitted {
			t.Fatalf("%s = (%q, %t), want (%q, %t)", projection.name, got.Text, admitted, projection.wantText, projection.wantAdmitted)
		}
		if projection.field.Disposition == FieldHide && got.Reason != "" {
			t.Fatalf("%s leaks reason %q", projection.name, got.Reason)
		}
	}

	// The governed profile renders stand-ins, omits hidden rows, and
	// withholds unknown dispositions — raw values never reach the page.
	avery := web063Person("worker-avery")
	salary := money(ResolveProductLocale("en-US"), avery.BasePay)
	bonus := percentage(ResolveProductLocale("en-US"), avery.BonusTarget)
	doc, err := Render(web066View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	body := web063BodyText(t, doc)
	for _, want := range []string{"Masked pay", "Target on file", "April 2022", "Redacted", "Withheld", "pos-design"} {
		if !strings.Contains(body, want) {
			t.Fatalf("governed profile shows no %q", want)
		}
	}
	for _, leaked := range []string{salary, bonus, "NW-40118", "2022-04-11"} {
		if leaked != "" && strings.Contains(body, leaked) {
			t.Fatalf("governed profile leaks %q", leaked)
		}
	}
	workerLabel := ResolveProductLocale("en-US").Text("person.worker_number")
	if strings.Contains(body, workerLabel) {
		t.Fatalf("hidden worker number keeps its %q row", workerLabel)
	}
}

// Golden: the disposition matrix digest (disposition × locale → text,
// plus admission).
func TestTodo_WEB_066_Golden(t *testing.T) {
	var builder strings.Builder
	for _, disposition := range []FieldDisposition{FieldShow, FieldMask, FieldRedact, FieldHide, FieldSummaryOnly, FieldDerivedOnly, "CURTAIN", ""} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			projected, admitted := ProjectField(ResolveProductLocale(locale), "118000", AuthorizedField{Disposition: disposition, StandIn: "on file"})
			builder.WriteString(string(disposition))
			builder.WriteString("\x00")
			builder.WriteString(locale)
			builder.WriteString("\x00")
			builder.WriteString(projected.Text)
			builder.WriteString("\x00")
			if admitted {
				builder.WriteString("show")
			} else {
				builder.WriteString("hide")
			}
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "8d109a6e8ac02f20952518a471bdd7bd4473493103794eeb6f756fb2d0db9f29"
	if got != want {
		t.Fatalf("disposition matrix digest = %s, want %s", got, want)
	}
}

// Browser: the governed profile parses with its hero, stand-ins, and
// markers, no hidden rows, and no positive tabindex stops.
func TestTodo_WEB_066_Browser(t *testing.T) {
	doc, err := Render(web066View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if findClassNode(root, "person-hero") == nil {
		t.Fatal("governed profile loses the hero")
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
		t.Fatalf("governed profile carries %d positive tabindex stops", positive)
	}
}

// Conformance: disposition copy in three locales, determinism, and no
// drift on the legacy effect path.
func TestTodo_WEB_066_Conformance(t *testing.T) {
	for locale := range map[string]string{"en-US": "Withheld", "de-DE": "Zurückgehalten", "ar": "محجوب"} {
		doc, err := Render(web066View(locale))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("%s governed render leaks an unresolved key", locale)
		}
	}
	lc := ResolveProductLocale("en-US")
	field := AuthorizedField{Disposition: FieldMask, StandIn: "Masked pay", Reason: "Need to know."}
	first, firstAdmitted := ProjectField(lc, "CAD 118,000", field)
	second, secondAdmitted := ProjectField(lc, "CAD 118,000", field)
	if !reflect.DeepEqual(first, second) || firstAdmitted != secondAdmitted {
		t.Fatal("disposition projection is nondeterministic")
	}
	for _, effect := range []PresentationEffect{PresentationAllow, PresentationRedact, PresentationDenied, PresentationWithheld, ""} {
		legacy := AuthorizedField{Effect: effect, Reason: "Need to know."}
		want := ProjectAuthorizedValue(lc, "118000", legacy).Text
		if got, admitted := ProjectField(lc, "118000", legacy); got.Text != want || !admitted {
			t.Fatalf("legacy effect %q drifts to (%q, %t), want (%q, true)", effect, got.Text, admitted, want)
		}
	}
}

// Security: raw values behind MASK, SUMMARY_ONLY, DERIVED_ONLY, HIDE,
// and unknown dispositions never reach the markup in any catalog locale;
// stand-ins do.
func TestTodo_WEB_066_Security(t *testing.T) {
	avery := web063Person("worker-avery")
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		lc := ResolveProductLocale(locale)
		salary := money(lc, avery.BasePay)
		bonus := percentage(lc, avery.BonusTarget)
		doc, err := Render(web066View(locale))
		if err != nil {
			t.Fatal(err)
		}
		body := web063BodyText(t, doc)
		for _, leaked := range []string{salary, bonus, "NW-40118", "2022-04-11"} {
			if leaked != "" && strings.Contains(body, leaked) {
				t.Fatalf("%s governed profile leaks %q", locale, leaked)
			}
		}
		for _, want := range []string{"Masked pay", "Target on file", "April 2022"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s governed profile loses stand-in %q", locale, want)
			}
		}
	}
}

func web066View(locale string) View {
	view := testView(PagePerson)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name":          {Effect: PresentationAllow},
			"role":          {Effect: PresentationAllow},
			"position_id":   {Disposition: FieldShow},
			"base_pay":      {Disposition: FieldMask, StandIn: "Masked pay"},
			"bonus_target":  {Disposition: FieldDerivedOnly, StandIn: "Target on file"},
			"hire_date":     {Disposition: FieldSummaryOnly, StandIn: "April 2022"},
			"worker_number": {Disposition: FieldHide},
			"legal_name":    {Disposition: FieldRedact},
			"manager":       {Disposition: "CURTAIN"},
		}},
	}
	return view
}
