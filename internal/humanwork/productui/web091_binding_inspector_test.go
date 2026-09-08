package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-091: the authorized binding inspector. Binding
// validators (WEB-077/078) verdict single bindings, but the
// studio has no unified inspection: no one view lists every
// composed binding with its validity and authorization-relevant
// posture, so authors review validity, sensitivity, masking,
// and token state across separate verdicts. The lifecycle needs
// a pure per-binding inspection in composition order — validity
// with verbatim reasons plus declared-attribute notes — that
// never echoes token material. Authorization itself stays
// server-side; the inspector reports, never decides.
func TestTodo_WEB_091(t *testing.T) {
	composition := PageComposition{
		Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
				Classification: "internal", SourceType: "projection", SourceID: "journeys", Masked: true, DisplayValue: "•••"},
			{WidgetType: "teleporter", Sensitive: true},
		},
		Actions: []ActionBinding{
			{Capability: "journeys.create", Token: "secret-token-123", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"},
			{Capability: "journeys.approve"},
		},
	}
	findings := InspectBindings(composition, RegisteredWidgets())
	if len(findings) != 4 {
		t.Fatalf("inspection lists %d findings, want 4", len(findings))
	}
	first := findings[0]
	if first.Kind != "widget" || first.Ref != "metric-display" || first.Index != 0 || !first.Valid || len(first.Reasons) != 0 {
		t.Fatalf("valid widget finding = %+v", first)
	}
	for _, note := range []string{`classification "internal"`, `authority "canonical"`, `source-type "projection"`, `source-id "journeys"`, `masked`} {
		found := false
		for _, actual := range first.Notes {
			if actual == note {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing note %q in %+v", note, first)
		}
	}
	second := findings[1]
	if second.Valid || second.Index != 1 {
		t.Fatalf("invalid widget finding = %+v", second)
	}
	if len(second.Reasons) == 0 || second.Reasons[0] != `unknown widget "teleporter"` {
		t.Fatalf("widget reasons = %q", second.Reasons)
	}

	third := findings[2]
	if third.Kind != "action" || third.Ref != "journeys.create" || third.Index != 0 || !third.Valid {
		t.Fatalf("valid action finding = %+v", third)
	}
	for _, note := range []string{`token present`, `input "CreateInput"`, `version 1`} {
		found := false
		for _, actual := range third.Notes {
			if actual == note {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing note %q in %+v", note, third)
		}
	}
	fourth := findings[3]
	if fourth.Valid || fourth.Index != 1 {
		t.Fatalf("invalid action finding = %+v", fourth)
	}
	if len(fourth.Reasons) == 0 || fourth.Reasons[0] != "missing action token" {
		t.Fatalf("action reasons = %q", fourth.Reasons)
	}

	// Token material never reaches the report.
	for _, finding := range findings {
		for _, note := range finding.Notes {
			if strings.Contains(note, "secret-token-123") {
				t.Fatalf("token material echoed in %+v", finding)
			}
		}
		for _, reason := range finding.Reasons {
			if strings.Contains(reason, "secret-token-123") {
				t.Fatalf("token material echoed in %+v", finding)
			}
		}
		if strings.Contains(finding.Ref, "secret-token-123") {
			t.Fatalf("token material echoed in %+v", finding)
		}
	}

	if findings := InspectBindings(PageComposition{}, RegisteredWidgets()); len(findings) != 0 {
		t.Fatalf("empty composition inspects %d findings", len(findings))
	}
}

// Golden: inspection findings over a binding matrix.
func TestTodo_WEB_091_Golden(t *testing.T) {
	compositions := []PageComposition{
		{Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
		}},
		{Widgets: []WidgetBinding{
			{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external", Classification: "public", SourceType: "embed", SourceID: "partner", Sensitive: true},
		}},
		{Actions: []ActionBinding{
			{Capability: "journeys.create", Token: "t", ExpectedVersion: 2, IdempotencyKey: "k", InputType: "CreateInput"},
		}},
		{Actions: []ActionBinding{
			{Capability: "/synthesized/endpoint"},
		}},
		{},
	}
	var builder strings.Builder
	for _, composition := range compositions {
		for _, finding := range InspectBindings(composition, RegisteredWidgets()) {
			builder.WriteString(finding.Kind)
			builder.WriteString("\x00")
			builder.WriteString(finding.Ref)
			builder.WriteString("\x00")
			if finding.Valid {
				builder.WriteString("valid")
			} else {
				builder.WriteString("invalid")
			}
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(finding.Reasons, ";"))
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(finding.Notes, ";"))
			builder.WriteString("\n")
		}
		builder.WriteString("---\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "15b3b244934e8019a66b9faa1428ccf6b8187f045ba84a24aca9a405ff48bd09"
	if got != want {
		t.Fatalf("inspector digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget's canonical binding inspects
// valid with its limit noted — deterministically.
func TestTodo_WEB_091_Browser(t *testing.T) {
	registry := RegisteredWidgets()
	for _, widget := range registry.Widgets {
		composition := PageComposition{Widgets: []WidgetBinding{{WidgetType: widget.ID, WidgetVersion: widget.Version,
			AuthorityClass: "canonical", Classification: widget.ClassificationLimit, SourceType: "projection", SourceID: "catalog"}}}
		first := InspectBindings(composition, registry)
		second := InspectBindings(composition, registry)
		if len(first) != 1 || !first[0].Valid {
			t.Fatalf("widget %q canonical binding inspects invalid: %+v", widget.ID, first)
		}
		want := `classification "` + widget.ClassificationLimit + `"`
		found := false
		for _, note := range first[0].Notes {
			if note == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("widget %q limit not noted: %+v", widget.ID, first[0])
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("widget %q inspection is nondeterministic", widget.ID)
		}
	}
}

// Conformance: reasons match the validators verbatim, notes keep
// fixed order, token absence noted.
func TestTodo_WEB_091_Conformance(t *testing.T) {
	registry := RegisteredWidgets()
	binding := WidgetBinding{WidgetType: "teleporter", Sensitive: true}
	findings := InspectBindings(PageComposition{Widgets: []WidgetBinding{binding}}, registry)
	direct := ValidateWidgetBinding(binding, registry)
	if !reflect.DeepEqual(findings[0].Reasons, direct.Reasons) {
		t.Fatalf("inspector reasons %q differ from validator %q", findings[0].Reasons, direct.Reasons)
	}
	action := ActionBinding{Capability: "journeys.approve"}
	actionFindings := InspectBindings(PageComposition{Actions: []ActionBinding{action}}, registry)
	directAction := ValidateActionBinding(action)
	if !reflect.DeepEqual(actionFindings[0].Reasons, directAction.Reasons) {
		t.Fatalf("inspector reasons %q differ from validator %q", actionFindings[0].Reasons, directAction.Reasons)
	}
	if len(actionFindings[0].Notes) == 0 || actionFindings[0].Notes[0] != "token missing" {
		t.Fatalf("token absence not noted first: %+v", actionFindings[0])
	}
	// Notes keep fixed attribute order across bindings.
	ordered := InspectBindings(PageComposition{Widgets: []WidgetBinding{{
		WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
		Classification: "internal", SourceType: "projection", SourceID: "journeys", Sensitive: true, Editable: true,
	}}}, registry)
	want := []string{`classification "internal"`, `authority "canonical"`, `source-type "projection"`, `source-id "journeys"`, `editable`, `sensitive`}
	if !reflect.DeepEqual(ordered[0].Notes, want) {
		t.Fatalf("note order = %q, want %q", ordered[0].Notes, want)
	}
}
