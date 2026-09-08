package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-116: the worker pay and benefits section. The
// page renders compensation facts inline with no independent
// constructor: money and percentage formatting plus verdict
// behavior live only in the page adapter, so a second pay
// consumer reformats by convention. The compiler needs the
// independent constructor over the shared section engine —
// the page's exact compensation fact set with locale-aware
// formatting and the page's exact silent/governed behavior.
func TestTodo_WEB_116(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{
		ID: "worker-avery", BasePay: testMoney("118000", "CAD"), BonusTarget: "0.12",
		PayZone: "CA-ON",
	}

	silent := ResolveWorkerPay(locale, person, nil)
	if silent.Title != locale.Text("person.compensation") {
		t.Fatalf("pay title = %q", silent.Title)
	}
	if silent.Description != locale.Text("person.compensation_detail") {
		t.Fatalf("pay description = %q", silent.Description)
	}
	var names []string
	values := map[string]string{}
	for _, fact := range silent.Facts {
		names = append(names, fact.Name)
		values[fact.Name] = fact.Value
	}
	wantNames := []string{"base_pay", "bonus_target", "pay_zone", "pay_frequency"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("pay facts = %q", names)
	}
	if values["base_pay"] != money(locale, person.BasePay) || values["bonus_target"] != percentage(locale, "0.12") {
		t.Fatalf("pay values = %+v", values)
	}
	if values["pay_zone"] != "CA-ON" {
		t.Fatalf("pay zone = %q", values["pay_zone"])
	}
	if values["pay_frequency"] != locale.Text("common.not_reported") {
		t.Fatalf("empty fact reads as %q", values["pay_frequency"])
	}

	// Governed records hide withheld facts and project the rest.
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"base_pay": {Effect: PresentationAllow},
			"pay_zone": {Disposition: FieldHide},
		}},
	}
	governed := ResolveWorkerPay(locale, person, verdicts)
	for _, fact := range governed.Facts {
		if fact.Name == "pay_zone" {
			t.Fatal("hidden fact survives")
		}
	}
	if len(governed.Facts) != len(silent.Facts)-1 {
		t.Fatalf("governed facts = %d, want %d", len(governed.Facts), len(silent.Facts)-1)
	}
}

// Golden: pay outcomes over person/verdict pairs.
func TestTodo_WEB_116_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "w-1", BasePay: testMoney("118000", "CAD"), BonusTarget: "0.12", PayZone: "CA-ON"},
		{ID: "w-2", BonusTarget: "bogus"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"pay_zone": {Disposition: FieldHide}}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			section := ResolveWorkerPay(locale, person, verdicts)
			for _, fact := range section.Facts {
				fmt.Fprintf(&builder, "%s|%s\x00", fact.Name, fact.Value)
			}
			builder.WriteString("\n")
		}
		builder.WriteString("==\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "8529e2290e0ae88e0e29ec87344ccb9ca23b05a8ebccb73c7ce9f2981869a03d"
	if got != want {
		t.Fatalf("pay digest = %s, want %s", got, want)
	}
}

// Browser: pay over person patterns resolves
// deterministically without mutating the record.
func TestTodo_WEB_116_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", BasePay: testMoney("95000", "USD"), BonusTarget: "0.1", PayZone: "US-CA"},
		{ID: "w-2"},
	}
	for _, person := range patterns {
		before := person
		first := ResolveWorkerPay(locale, person, nil)
		second := ResolveWorkerPay(locale, person, nil)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("pay resolution mutates its record")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("pay resolution is nondeterministic")
		}
		if len(first.Facts) != 4 {
			t.Fatalf("silent pay holds %d facts", len(first.Facts))
		}
	}
}

// Conformance: sections share one engine — identical
// silent/governed behavior, disjoint fact sets; invalid
// money stays undisclosed.
func TestTodo_WEB_116_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-3", BasePay: testMoney("100", "USD"), PayZone: "Z1"}
	pay := ResolveWorkerPay(locale, person, nil)
	overview := ResolveWorkerOverview(locale, person, nil)
	seen := map[string]bool{}
	for _, fact := range overview.Facts {
		seen[fact.Name] = true
	}
	for _, fact := range pay.Facts {
		if seen[fact.Name] {
			t.Fatalf("fact %q in both sections", fact.Name)
		}
	}
	if pay.Facts[0].Value != money(locale, person.BasePay) {
		t.Fatalf("pay rewrites money: %+v", pay.Facts[0])
	}
	broken := ResolveWorkerPay(locale, Person{ID: "w-4", BonusTarget: "bogus"}, nil)
	if broken.Facts[1].Value != locale.Text("common.not_disclosed") {
		t.Fatalf("bogus ratio reads as %q", broken.Facts[1].Value)
	}
	again := ResolveWorkerPay(locale, person, nil)
	if !reflect.DeepEqual(pay, again) {
		t.Fatal("pay resolution is unstable")
	}
}
