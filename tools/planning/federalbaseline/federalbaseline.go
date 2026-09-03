// Package federalbaseline scans planning/research/state-employment-law for
// places where a state research file restates a federal-law figure
// (LEGAL-017), and cross-checks every restatement against the single
// canonical statement in us-federal.md: the WARN Act's employer-size
// threshold and notice-day period, the FLSA minimum wage, the FLSA
// record-retention periods, and the FMLA employer-size/mile-radius
// threshold.
//
// The corpus is fifty independently-authored research files using
// inconsistent prose to restate the same handful of federal figures (e.g.
// "employers with 100+ employees", "covered employers (100+ employees)",
// "federal WARN's 100+ company size"). Extraction is therefore tolerant by
// design: ExtractRestatements finds a federal-law mention (e.g. "federal
// WARN", "federal FMLA", "federal FLSA minimum wage") and then searches a
// bounded window of trailing text for the number(s) that mention states,
// rather than requiring one fixed sentence shape. It is expected to miss
// some phrasings and should be extended with new alternatives as they
// surface, not rewritten as a strict grammar.
//
// us-federal.md itself is held to a stricter, non-tolerant shape:
// ParseBaseline depends on Section 1's summary bullets reading in the
// specific sentence forms authored there. That asymmetry is deliberate -
// the baseline document is the one place the figures must be
// unambiguous, since it is what every tolerant restatement elsewhere is
// compared against. A missing or reworded summary bullet is reported as a
// Violation rather than silently parsed as zero.
//
// This package does no file I/O of its own: callers (tools/planning's
// plancheck subcommand) read us-federal.md and the other research files
// and pass their content in, matching legalmatrix's separation of parsing
// from directory-walking.
package federalbaseline

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Violation is one concrete disagreement between a state file's federal-law
// restatement and the canonical figure in us-federal.md, or one defect in
// us-federal.md itself. Kind is a stable, machine-readable category;
// Detail names the offending file/line/values in prose.
type Violation struct {
	Kind   string
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.Kind, v.Detail)
}

// Baseline is the canonical set of federal figures extracted from
// us-federal.md's Section 1 summary.
type Baseline struct {
	WARNNoticeDays                  int
	WARNEmployerThreshold           int
	FLSAMinWage                     string
	FLSARetentionPayrollYears       int
	FLSARetentionSupplementaryYears int
	FMLAWeeks                       int
	FMLAHours                       string
	FMLAEmployerThreshold           int
	FMLAMileRadius                  int
}

// baselineFilename is the one file this package treats as the canonical
// statement; callers must exclude it from the state-file set passed to
// Check (it is not a restatement of itself).
const baselineFilename = "us-federal.md"

var (
	// baselineWARNRe matches the Section 1 summary's WARN sentence:
	// "WARN Act (60 days' notice) applies to employers 100+ employees".
	baselineWARNRe = regexp.MustCompile(`WARN Act \((\d{1,3}) days.? notice\) applies to employers (\d{1,4})\+ employees`)

	// baselineWageRe matches "FLSA minimum wage $7.25/hour".
	baselineWageRe = regexp.MustCompile(`FLSA minimum wage \$(\d{1,3}\.\d{2})/hour`)

	// baselineRetentionRe matches "FLSA: retain payroll 3 years,
	// supplementary time records 2 years."
	baselineRetentionRe = regexp.MustCompile(`FLSA: retain payroll (\d{1,2}) years?,? supplementary time records (\d{1,2}) years?`)

	// baselineFMLARe matches "FMLA: 12 weeks/year unpaid leave, 1,250
	// hours in 12 months, for employers 50+ within 75 miles".
	baselineFMLARe = regexp.MustCompile(`FMLA: (\d{1,2}) weeks/year unpaid leave, ([\d,]{1,7}) hours in \d{1,2} months, for employers (\d{1,3})\+ within (\d{1,3}) miles`)
)

// ParseBaseline extracts the Baseline from us-federal.md's content. Every
// missing field is reported as a "missing_baseline_field" Violation with
// the expected sentence shape named, rather than silently leaving a zero
// value that would make every state-file comparison spuriously pass or
// fail.
func ParseBaseline(content string) (Baseline, []Violation) {
	var b Baseline
	var violations []Violation

	if m := baselineWARNRe.FindStringSubmatch(content); m != nil {
		b.WARNNoticeDays = atoi(m[1])
		b.WARNEmployerThreshold = atoi(m[2])
	} else {
		violations = append(violations, Violation{"missing_baseline_field", `us-federal.md has no "WARN Act (N days' notice) applies to employers N+ employees" summary sentence`})
	}

	if m := baselineWageRe.FindStringSubmatch(content); m != nil {
		b.FLSAMinWage = m[1]
	} else {
		violations = append(violations, Violation{"missing_baseline_field", `us-federal.md has no "FLSA minimum wage $N.NN/hour" summary sentence`})
	}

	if m := baselineRetentionRe.FindStringSubmatch(content); m != nil {
		b.FLSARetentionPayrollYears = atoi(m[1])
		b.FLSARetentionSupplementaryYears = atoi(m[2])
	} else {
		violations = append(violations, Violation{"missing_baseline_field", `us-federal.md has no "FLSA: retain payroll N years, supplementary time records N years" summary sentence`})
	}

	if m := baselineFMLARe.FindStringSubmatch(content); m != nil {
		b.FMLAWeeks = atoi(m[1])
		b.FMLAHours = m[2]
		b.FMLAEmployerThreshold = atoi(m[3])
		b.FMLAMileRadius = atoi(m[4])
	} else {
		violations = append(violations, Violation{"missing_baseline_field", `us-federal.md has no "FMLA: N weeks/year unpaid leave, N hours in N months, for employers N+ within N miles" summary sentence`})
	}

	return b, violations
}

// Restatement is one federal-law figure a state file states in its own
// words, with the line it was found on.
type Restatement struct {
	File  string
	Line  int
	Kind  string
	Value string
}

// Restatement kinds.
const (
	KindWARNEmployerThreshold = "WARN_EMPLOYER_THRESHOLD"
	KindWARNNoticeDays        = "WARN_NOTICE_DAYS"
	KindFLSAMinWage           = "FLSA_MIN_WAGE"
	KindFLSARetentionPayroll  = "FLSA_RETENTION_PAYROLL_YEARS"
	KindFLSARetentionSupp     = "FLSA_RETENTION_SUPPLEMENTARY_YEARS"
	KindFMLAEmployerThreshold = "FMLA_EMPLOYER_THRESHOLD"
	KindFMLAMileRadius        = "FMLA_MILE_RADIUS"
	KindFMLAHours             = "FMLA_HOURS"
)

var (
	// federalWARNMentionRe finds a mention of the federal WARN Act, as
	// distinct from a state's own "mini-WARN" law.
	federalWARNMentionRe = regexp.MustCompile(`(?i)federal\s+(?:worker adjustment and retraining notification\s*)?\(?\s*warn\s*\)?(?:\s*act)?`)

	// warnEmployerThresholdRe finds the employer-size (coverage)
	// threshold near a federal-WARN mention. Every alternative is a
	// phrasing actually observed in the corpus; the first non-empty
	// capture group across alternatives is the extracted number.
	warnEmployerThresholdRe = regexp.MustCompile(`(?i)employers?\s+(?:with|of)\s+(\d{1,4})\+?\s*(?:full-time\s+)?employees|covered employers?\s*\(\s*(\d{1,4})\+?|\(\s*(\d{1,4})\+?\s*(?:full-time\s+)?employees\)|(\d{1,4})\+?\s*(?:full-time\s+)?(?:company size|employer employees)`)

	// warnNoticeDaysRe finds the notice-day figure near a federal-WARN
	// mention.
	warnNoticeDaysRe = regexp.MustCompile(`(?i)(\d{1,3})[\s-](?:calendar\s+)?days['’]?\s*(?:written\s+|advance\s+)*notice`)

	// flsaWageRe finds a federal/FLSA minimum-wage dollar restatement in
	// either number-after-label or label-after-number order. The
	// connector between the label and the dollar figure is deliberately
	// restricted to a bare "of"/"is"/":"/"(" (optionally none at all):
	// looser gaps like "higher than the" (a differential, not a
	// restatement) or "% of" (a fraction of the federal figure, e.g. a
	// tipped sub-minimum) must not match, since those describe a
	// different number, not the federal minimum wage itself.
	flsaWageRe = regexp.MustCompile(`(?i)federal(?:\s+FLSA)?\s+(?:minimum(?:\s+wage)?|floor)\s*(?:of|is|:|\()?\s*\$(\d{1,3}\.\d{2})|\$(\d{1,3}\.\d{2})(?:/hour)?\s*(?:\(\s*)?(?:is\s+)?(?:the\s+)?federal(?:\s+FLSA)?\s+(?:minimum(?:\s+wage)?|floor)`)

	// federalFLSAMentionForRetentionRe finds a mention of the federal
	// FLSA record-retention rule anchored to "requires"/"require"
	// (allowing one short citation parenthetical in between, e.g.
	// "FLSA (29 U.S.C. § 211(b)) requires"). Anchoring to the verb -
	// rather than matching a bare "federal FLSA" anywhere - matters
	// because this corpus also uses "federal FLSA" in sentences that
	// only mention it as a general compliance standard with no
	// retention figure of its own nearby (e.g. "digital records must be
	// retained... in compliance with federal FLSA and IRS standards"),
	// where the nearest trailing number belongs to the state's own rule,
	// not a restatement of the federal one.
	federalFLSAMentionForRetentionRe = regexp.MustCompile(`(?i)federal\s+FLSA(?:\s*\([^)]{0,40}\))?\s+requires?|FLSA\s+(?:requires?|require)`)

	// flsaRetentionBackwardRe matches the corpus's other common idiom:
	// the years figure stated first, with "federal FLSA" cited
	// immediately afterward as the source ("3 years per federal FLSA",
	// "3 years (federal FLSA)"). The connector is restricted to
	// per/under/a bare parenthesis so an unrelated preceding number (a
	// state's own, different, retention period) cannot match just
	// because "federal FLSA" appears somewhere later in the sentence.
	flsaRetentionBackwardRe = regexp.MustCompile(`(?i)(\d{1,2})\s*years?\s*(?:per|under|\()\s*federal\s+FLSA`)

	// yearsRe finds a bare "N year(s)" figure, used only within a
	// bounded forward window of a federalFLSAMentionForRetentionRe
	// match.
	yearsRe = regexp.MustCompile(`(\d{1,2})[\s-]years?`)

	// federalFMLAMentionRe finds a mention of the federal FMLA.
	federalFMLAMentionRe = regexp.MustCompile(`(?i)federal\s+FMLA`)

	// fmlaEmployerMileRe finds the compound "N+ employees within N
	// miles" threshold near a federal-FMLA mention.
	fmlaEmployerMileRe = regexp.MustCompile(`(?i)(\d{1,3})\+?\s*employees?\s+within\s+(\d{1,3})[\s-]mile`)

	// fmlaHoursRe finds the comma-formatted hours figure (e.g. "1,250
	// hours") near a federal-FMLA mention. The comma-thousands format is
	// distinctive enough that it is not gated on window proximity beyond
	// the caller's window slice.
	fmlaHoursRe = regexp.MustCompile(`(\d{1,3},\d{3})\+?\s*hours`)
)

// windowAfter returns the slice of s starting at idx, up to n runes long
// (bytes, for the ASCII-heavy research corpus), for bounding a
// secondary-regex search near a mention without matching across
// unrelated sentences far down the file.
func windowAfter(s string, idx, n int) string {
	end := idx + n
	if end > len(s) {
		end = len(s)
	}
	if idx > len(s) {
		return ""
	}
	return s[idx:end]
}

// windowUntilBoundary is windowAfter, additionally truncated at the first
// following Markdown bullet ("\n- ") or paragraph break ("\n\n"), if one
// falls inside the window. The research corpus states one obligation per
// bullet or paragraph; a federal-law mention's own figures are stated in
// the same bullet, so a subsequent bullet's unrelated number (typically
// the state's own, different, threshold or notice period) must not be
// captured just because it falls within a flat byte-count window.
func windowUntilBoundary(s string, idx, n int) string {
	w := windowAfter(s, idx, n)
	cut := len(w)
	if i := strings.Index(w, "\n- "); i != -1 && i < cut {
		cut = i
	}
	if i := strings.Index(w, "\n\n"); i != -1 && i < cut {
		cut = i
	}
	return w[:cut]
}

// firstNonEmpty returns the first non-empty capture group in a
// FindStringSubmatch result (excluding group 0, the whole match).
func firstNonEmpty(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// lineAt returns the 1-based line number of byte offset pos in content.
func lineAt(content string, pos int) int {
	if pos > len(content) {
		pos = len(content)
	}
	return 1 + strings.Count(content[:pos], "\n")
}

// ExtractRestatements scans one state research file's content for
// tolerant restatements of the federal WARN, FLSA and FMLA figures this
// package tracks. filename is used only to attribute Restatement.File.
func ExtractRestatements(filename, content string) []Restatement {
	var out []Restatement

	for _, loc := range federalWARNMentionRe.FindAllStringIndex(content, -1) {
		window := windowUntilBoundary(content, loc[0], 220)
		if m := warnEmployerThresholdRe.FindStringSubmatch(window); m != nil {
			if v := firstNonEmpty(m); v != "" {
				out = append(out, Restatement{File: filename, Line: lineAt(content, loc[0]), Kind: KindWARNEmployerThreshold, Value: v})
			}
		}
		if m := warnNoticeDaysRe.FindStringSubmatch(window); m != nil {
			out = append(out, Restatement{File: filename, Line: lineAt(content, loc[0]), Kind: KindWARNNoticeDays, Value: m[1]})
		}
	}

	for _, loc := range flsaWageRe.FindAllStringSubmatchIndex(content, -1) {
		m := flsaWageRe.FindStringSubmatch(content[loc[0]:loc[1]])
		if v := firstNonEmpty(m); v != "" {
			out = append(out, Restatement{File: filename, Line: lineAt(content, loc[0]), Kind: KindFLSAMinWage, Value: v})
		}
	}

	for _, loc := range federalFLSAMentionForRetentionRe.FindAllStringIndex(content, -1) {
		window := windowUntilBoundary(content, loc[1], 130)
		m := yearsRe.FindStringSubmatch(window)
		if m == nil {
			continue
		}
		out = append(out, Restatement{File: filename, Line: lineAt(content, loc[0]), Kind: retentionKind(content, loc[0]), Value: m[1]})
	}
	for _, loc := range flsaRetentionBackwardRe.FindAllStringSubmatchIndex(content, -1) {
		m := flsaRetentionBackwardRe.FindStringSubmatch(content[loc[0]:loc[1]])
		out = append(out, Restatement{File: filename, Line: lineAt(content, loc[0]), Kind: retentionKind(content, loc[0]), Value: m[1]})
	}

	for _, loc := range federalFMLAMentionRe.FindAllStringIndex(content, -1) {
		window := windowUntilBoundary(content, loc[0], 200)
		if m := fmlaEmployerMileRe.FindStringSubmatch(window); m != nil {
			line := lineAt(content, loc[0])
			out = append(out, Restatement{File: filename, Line: line, Kind: KindFMLAEmployerThreshold, Value: m[1]})
			out = append(out, Restatement{File: filename, Line: line, Kind: KindFMLAMileRadius, Value: m[2]})
		}
		if m := fmlaHoursRe.FindStringSubmatch(window); m != nil {
			out = append(out, Restatement{File: filename, Line: lineAt(content, loc[0]), Kind: KindFMLAHours, Value: m[1]})
		}
	}

	return out
}

// retentionKind classifies a retention-figure mention at byte offset idx
// as the supplementary-records figure (2 years) rather than the default
// payroll-records figure (3 years), based on whether "supplementary"
// appears in the surrounding text.
func retentionKind(content string, idx int) string {
	context := windowAfter(content, max(0, idx-80), 240)
	if strings.Contains(strings.ToLower(context), "supplementary") {
		return KindFLSARetentionSupp
	}
	return KindFLSARetentionPayroll
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func atoi(s string) int {
	s = strings.ReplaceAll(s, ",", "")
	n, _ := strconv.Atoi(s)
	return n
}

// Check parses baselineContent (us-federal.md) and compares every
// restatement extracted from stateFiles (basename -> content, excluding
// us-federal.md) against it. Returned violations are sorted by (Kind,
// Detail) for deterministic output; a caller passing us-federal.md's own
// content in stateFiles has it silently skipped rather than compared
// against itself.
func Check(baselineContent string, stateFiles map[string]string) []Violation {
	baseline, violations := ParseBaseline(baselineContent)

	names := make([]string, 0, len(stateFiles))
	for name := range stateFiles {
		if name == baselineFilename {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, r := range ExtractRestatements(name, stateFiles[name]) {
			if v, ok := disagreement(r, baseline); ok {
				violations = append(violations, v)
			}
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Kind != violations[j].Kind {
			return violations[i].Kind < violations[j].Kind
		}
		return violations[i].Detail < violations[j].Detail
	})
	return violations
}

// disagreement compares one Restatement against the baseline figure of
// the same kind, returning a Violation and true when they disagree.
func disagreement(r Restatement, b Baseline) (Violation, bool) {
	mismatch := func(want string) (Violation, bool) {
		if r.Value == want {
			return Violation{}, false
		}
		return Violation{
			Kind:   "federal_baseline_disagreement",
			Detail: fmt.Sprintf("%s:%d: states %s=%s, us-federal.md states %s", r.File, r.Line, r.Kind, r.Value, want),
		}, true
	}

	switch r.Kind {
	case KindWARNEmployerThreshold:
		return mismatch(strconv.Itoa(b.WARNEmployerThreshold))
	case KindWARNNoticeDays:
		return mismatch(strconv.Itoa(b.WARNNoticeDays))
	case KindFLSAMinWage:
		return mismatch(b.FLSAMinWage)
	case KindFLSARetentionPayroll:
		return mismatch(strconv.Itoa(b.FLSARetentionPayrollYears))
	case KindFLSARetentionSupp:
		return mismatch(strconv.Itoa(b.FLSARetentionSupplementaryYears))
	case KindFMLAEmployerThreshold:
		return mismatch(strconv.Itoa(b.FMLAEmployerThreshold))
	case KindFMLAMileRadius:
		return mismatch(strconv.Itoa(b.FMLAMileRadius))
	case KindFMLAHours:
		return mismatch(b.FMLAHours)
	default:
		return Violation{}, false
	}
}
