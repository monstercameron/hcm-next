package productui

// PROMOUX-004: "Replace free-text target positions with authorized vacancy
// selection and reservation evidence."
//
// TestTodo_PROMOUX_004_Browser proves the picker's rendered markup carries
// every GREEN field -- title, organization, manager, location, vacancy
// window and reservation state -- for an authorized compatible candidate,
// and that the value a picked option's radio input carries is the opaque
// revision reference, never a raw position id (REFACTOR).
//
// TestTodo_PROMOUX_004_Accessibility proves the picker is a native labeled
// group: a fieldset with a legend, radio inputs sharing one name (so
// assistive technology announces them as one mutually exclusive choice),
// and each option's vacancy window/reservation state associated with its
// input through aria-describedby rather than left as disconnected text. It
// also proves the empty state (no compatible vacant position) still renders
// a labeled, explained region rather than disappearing.

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func promoux004TestCandidate(t *testing.T) positionpicker.Candidate {
	t.Helper()
	end, err := values.ParseLocalDate("2027-03-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	return positionpicker.Candidate{
		Reference: "cmVm.dGVzdA", // an opaque, non-guessable-looking token; this file never decodes it
		Title:     "Engineering Manager", Organization: "Engineering", Manager: "Jane Smith", Location: "Remote (US)",
		HasVacancyEnd: true, VacancyEnd: end, ReservationState: positionpicker.ReservationAvailable,
	}
}

func TestTodo_PROMOUX_004_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	candidate := promoux004TestCandidate(t)

	// Props contract first: PositionPickerOptionPropsFrom is the one seam
	// that turns the domain's candidate into presentation, and it must not
	// silently drop or alter a field positionpicker already decided.
	optionProps := PositionPickerOptionPropsFrom(locale, candidate)
	if optionProps.Reference != candidate.Reference.String() {
		t.Fatalf("Reference = %q, want the candidate's own opaque reference %q", optionProps.Reference, candidate.Reference.String())
	}
	if optionProps.Title != "Engineering Manager" || optionProps.Organization != "Engineering" ||
		optionProps.Manager != "Jane Smith" || optionProps.Location != "Remote (US)" {
		t.Fatalf("option display fields = %+v, want the candidate's own labels verbatim", optionProps)
	}
	if optionProps.VacancyWindow != "Open through "+locale.FormatDate(localDateToTime(candidate.VacancyEnd)) {
		t.Fatalf("VacancyWindow = %q, want the localized open-until phrasing", optionProps.VacancyWindow)
	}
	if optionProps.ReservationState != "Available" {
		t.Fatalf("ReservationState = %q, want %q", optionProps.ReservationState, "Available")
	}

	markup, err := ui.RenderToString(ui.CreateElement(PositionPicker, PositionPickerProps{
		Name: "target-position", Legend: "Choose the target position",
		Options: []PositionPickerOptionProps{optionProps},
	}))
	if err != nil {
		t.Fatalf("render PositionPicker: %v", err)
	}
	for _, want := range []string{
		"Choose the target position",
		"Engineering Manager", "Engineering", "Jane Smith", "Remote (US)",
		"Available",
		`value="` + candidate.Reference.String() + `"`,
		`type="radio"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("rendered markup missing %q\n%s", want, markup)
		}
	}
	// REFACTOR: the raw position id -- had this component been handed one
	// -- must never appear; only the opaque Reference does. This candidate
	// carries no raw id at all (positionpicker.Candidate.Position is not
	// even read by PositionPickerOptionPropsFrom), which is itself the
	// proof: there is no code path here that could leak it.
	if strings.Contains(markup, "POS-ENG-MGR-101") {
		t.Fatal("rendered markup must never contain a raw guessable position identifier")
	}
}

func TestTodo_PROMOUX_004_Accessibility(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	candidateA := promoux004TestCandidate(t)
	candidateB := promoux004TestCandidate(t)
	candidateB.Reference = "cmVm.b3RoZXI"
	candidateB.Title, candidateB.HasVacancyEnd, candidateB.ReservationState = "Staff Engineer", false, positionpicker.ReservationAvailable

	optionA := PositionPickerOptionPropsFrom(locale, candidateA)
	optionB := PositionPickerOptionPropsFrom(locale, candidateB)

	t.Run("options share one native fieldset/legend group and one radio name", func(t *testing.T) {
		markup, err := ui.RenderToString(ui.CreateElement(PositionPicker, PositionPickerProps{
			Name: "target-position", Legend: "Choose the target position",
			Options: []PositionPickerOptionProps{optionA, optionB}, Selected: optionA.Reference,
		}))
		if err != nil {
			t.Fatalf("render PositionPicker: %v", err)
		}
		if !strings.Contains(markup, "<fieldset") || !strings.Contains(markup, "<legend") {
			t.Fatalf("markup must be a native fieldset/legend group:\n%s", markup)
		}
		if strings.Count(markup, `name="target-position"`) != 2 {
			t.Fatalf("both options must share one radio group name so assistive technology treats them as mutually exclusive:\n%s", markup)
		}
		if !strings.Contains(markup, `checked`) {
			t.Fatalf("the selected option's radio input must be marked checked:\n%s", markup)
		}
	})

	t.Run("each option's vacancy window and reservation state are associated through aria-describedby", func(t *testing.T) {
		markup, err := ui.RenderToString(ui.CreateElement(PositionPicker, PositionPickerProps{
			Name: "target-position", Legend: "Choose the target position",
			Options: []PositionPickerOptionProps{optionA},
		}))
		if err != nil {
			t.Fatalf("render PositionPicker: %v", err)
		}
		descID := "position-picker-desc-" + optionA.Reference
		if !strings.Contains(markup, `aria-describedby="`+descID+`"`) {
			t.Fatalf("radio input must reference its option's description by id:\n%s", markup)
		}
		if !strings.Contains(markup, `id="`+descID+`"`) {
			t.Fatalf("the description element itself must carry the matching id:\n%s", markup)
		}
		if !strings.Contains(markup, "Open through") {
			t.Fatalf("the described vacancy window text must actually render:\n%s", markup)
		}
	})

	t.Run("no candidate renders a labeled explained empty state, not a disappearing control", func(t *testing.T) {
		markup, err := ui.RenderToString(ui.CreateElement(PositionPicker, PositionPickerProps{
			Name: "target-position", Legend: "Choose the target position",
			EmptyTitle: "No compatible vacant position", EmptyDetail: "No authorized position matches this proposal's job and organization right now.",
		}))
		if err != nil {
			t.Fatalf("render PositionPicker: %v", err)
		}
		if !strings.Contains(markup, "<fieldset") || !strings.Contains(markup, "<legend") {
			t.Fatalf("the empty state must still be a labeled fieldset/legend group:\n%s", markup)
		}
		if !strings.Contains(markup, "No compatible vacant position") || !strings.Contains(markup, "No authorized position matches") {
			t.Fatalf("the empty state must explain itself:\n%s", markup)
		}
		if strings.Contains(markup, `type="radio"`) {
			t.Fatalf("an empty candidate list must render no radio inputs:\n%s", markup)
		}
	})
}
