package extract

import (
	"fmt"
	"path"
	"strings"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
)

// DraftWindowStart is the effective-window start every extracted draft
// carries. It is a fixture window, not a statutory one: the corpus was
// retrieved on 2026-09-03 and states its own effective dates inconsistently,
// so a draft asserts only that it describes the law as the research recorded
// it. Effective-date boundaries become real when a pack is authored for
// release, per the contract's section 2.3.
const DraftWindowStart = "2026-01-01"

// ExtractorVersion identifies this extractor in a definition's provenance. It
// sits outside the release digest, so bumping it never invalidates a signed
// release.
const ExtractorVersion = "internal/governance/legal/extract v1 (LEGAL-010, LEGAL-011)"

// StateExtraction is one state's generated definition plus the findings the
// extraction produced.
type StateExtraction struct {
	State      State
	Definition legal.PackDefinition
	// KindCounts is how many obligations of each kind the definition carries.
	KindCounts map[legal.ObligationType]int
	// UnsupportedY names the kinds the matrix marks Y for which no research
	// item could be found. The obligation is still emitted, marked VERIFY
	// with an explicit "section not stated" citation, so a Y cell is never
	// silently empty.
	UnsupportedY []legal.ObligationType
	// UncertainEmitted names the `?` cells that did find evidence and were
	// therefore emitted, always at VERIFY.
	UncertainEmitted []legal.ObligationType
	// UncertainDropped names the `?` cells with no evidence, emitted as
	// nothing at all.
	UncertainDropped []legal.ObligationType
}

// ExtractState builds one state's draft definition from its research file and
// the contract matrix.
func ExtractState(matrix *Matrix, file *ResearchFile, state State) (StateExtraction, error) {
	out := StateExtraction{State: state, KindCounts: map[legal.ObligationType]int{}}

	def := legal.PackDefinition{
		SchemaVersion:     1,
		PackID:            draftPackID(state.Code),
		Version:           legal.PackVersion{Major: 1, Minor: 0},
		VocabularyVersion: uint32(legal.VocabularyVersion2),
		Jurisdiction: legal.JurisdictionJSON{
			Country:      "US",
			Subdivision:  state.Code,
			LocalityPath: []string{},
			Level:        "SUBDIVISION",
		},
		Window:       legal.WindowJSON{Start: DraftWindowStart},
		SourceType:   legal.SourceTypeStatute.String(),
		ReviewStatus: legal.ReviewStatusUnreviewed.String(),
		Obligations:  []legal.ObligationJSON{},
		Preemptions:  []legal.PreemptionJSON{},
		Provenance: legal.ProvenanceJSON{
			Generator:   ExtractorVersion,
			SourceFiles: []string{file.Path},
			Notes: "Mechanically extracted from the research file's Implications for P1A/P1B section " +
				"and the rule-pack contract's section 5 matrix. Agent-drafted research, no legal " +
				"review, not legal advice. Every rule is UNREVIEWED and this pack is unusable under " +
				"any nonzero tenant review floor. A field the research does not state is left " +
				"unstated rather than filled in.",
		},
	}

	for _, kind := range legal.AllObligationTypes() {
		cell := matrix.Cell(state.Code, kind)
		switch cell.Value {
		case CellStateRule:
			item, found := findEvidence(file, kind)
			obligation := buildObligation(state, file, kind, cell, item, found)
			def.Obligations = append(def.Obligations, obligation)
			out.KindCounts[kind]++
			if !found {
				out.UnsupportedY = append(out.UnsupportedY, kind)
			}
		case CellUncertain:
			item, found := findEvidence(file, kind)
			if !found {
				out.UncertainDropped = append(out.UncertainDropped, kind)
				continue
			}
			obligation := buildObligation(state, file, kind, cell, item, true)
			// A `?` cell is a blocking finding in the contract's section 5.
			// Whatever the research text reads like, the matrix says the
			// corpus is uncertain, so the marker is VERIFY.
			obligation.Citation.ConfidenceMarker = legal.ConfidenceMarkerVerify.String()
			def.Obligations = append(def.Obligations, obligation)
			out.KindCounts[kind]++
			out.UncertainEmitted = append(out.UncertainEmitted, kind)
		default:
			// F: the federal baseline applies and this pack carries nothing.
			// L: the rule is a locality rule and belongs to a locality-level
			// release, not to this subdivision pack.
			// P: the state preempts locality rules, which is a
			// PreemptionAssertion below, never an obligation.
		}
	}

	for _, row := range matrix.Preemptions[state.Code] {
		def.Preemptions = append(def.Preemptions, legal.PreemptionJSON{
			Kind:  row.Kind.String(),
			Scope: "LOCALITY_ONLY",
			Citation: legal.CitationJSON{
				SourceFile:       file.Path,
				Section:          cleanSection(strings.ReplaceAll(row.Citation, "`", "")),
				Note:             "state preemption of locality rules of this kind, per the rule-pack contract's section 6.4",
				ReviewStatus:     legal.ReviewStatusUnreviewed.String(),
				ConfidenceMarker: legal.ConfidenceMarkerVerify.String(),
			},
		})
	}

	out.Definition = def
	if _, err := def.Candidate(); err != nil {
		return StateExtraction{}, fmt.Errorf("extract: %s draft does not validate: %w", state.Code, err)
	}
	return out, nil
}

func draftPackID(code string) string {
	return "us-" + strings.ToLower(code) + "-promotion-base-pay-change-draft"
}

// findEvidence returns the research item that evidences a kind, searching
// Implications, then Summary, then the topic section that owns the kind.
//
// Among matching items it prefers the first that also names a statutory
// section, and falls back to the first match otherwise. A cited item is
// strictly better evidence than an uncited one: "MCA § 39-3-404" is a rule a
// reviewer can check, while a sentence about what the platform should flag is
// not. Both passes walk the same fixed order, so the choice is deterministic.
func findEvidence(file *ResearchFile, kind legal.ObligationType) (Item, bool) {
	matcher, ok := MatcherFor(kind)
	if !ok {
		return Item{}, false
	}
	var firstMatch Item
	haveMatch := false
	for _, item := range file.ItemsForSearch(matcher.Owning) {
		if !matcher.Pattern.MatchString(item.Text) {
			continue
		}
		if !haveMatch {
			firstMatch, haveMatch = item, true
		}
		if ExtractSection(item.Text) != SectionNotStated {
			return item, true
		}
	}
	return firstMatch, haveMatch
}

// obligationID builds the stable id for one state's obligation of a kind.
func obligationID(code string, kind legal.ObligationType) string {
	slug := strings.ToLower(strings.ReplaceAll(kind.String(), "_", "-"))
	return "us-" + strings.ToLower(code) + "-" + slug
}

// buildObligation renders one obligation entry. When found is false the
// matrix asserts a state rule the research does not evidence; the obligation
// is emitted anyway, with an unmistakable "section not stated" citation and a
// VERIFY marker, because dropping it would make the pack claim the state has
// no such rule.
func buildObligation(
	state State,
	file *ResearchFile,
	kind legal.ObligationType,
	cell MatrixCell,
	item Item,
	found bool,
) legal.ObligationJSON {
	citation := legal.CitationJSON{
		SourceFile:   file.Path,
		ReviewStatus: legal.ReviewStatusUnreviewed.String(),
	}
	standard := legal.RuleStandardRequired
	if !found {
		citation.Section = SectionNotStated
		citation.Note = fmt.Sprintf(
			"the contract's section 5 matrix marks %s for %s, and no item in %s evidences the rule; "+
				"recorded so the gap is visible rather than read as an absence of duty",
			kind, state.Code, path.Base(file.Path))
		citation.ConfidenceMarker = legal.ConfidenceMarkerVerify.String()
	} else {
		citation.Section = ExtractSection(item.Text)
		citation.Note = Summarize(item.Text, 220)
		marker := legal.ConfidenceMarkerConfirmed
		if citation.Section == SectionNotStated || ReadsUncertain(item.Text) {
			marker = legal.ConfidenceMarkerVerify
		}
		if ReadsRecommendation(item.Text) {
			// Contract section 7.1: a recommendation is never promoted into a
			// statutory requirement.
			standard = legal.RuleStandardRecommended
			marker = legal.ConfidenceMarkerVerify
		}
		citation.ConfidenceMarker = marker.String()
	}

	return legal.ObligationJSON{
		Kind:     kind.String(),
		ID:       obligationID(state.Code, kind),
		Citation: citation,
		Body:     buildBody(kind, cell, item, found, standard, citation.Note),
	}
}
