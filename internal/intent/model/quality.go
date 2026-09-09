package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const qualityPatternCacheCap = 512

// QualityEvaluator owns the bounded compiled-pattern cache used by quality
// evaluations. Keeping this state on the evaluator makes reuse explicit and
// prevents independent compositions from sharing mutable package state.
type QualityEvaluator struct {
	mu       sync.Mutex
	patterns map[string]*regexp.Regexp
}

// NewQualityEvaluator creates an evaluator with its own compiled-pattern
// cache.
func NewQualityEvaluator() *QualityEvaluator {
	return &QualityEvaluator{patterns: make(map[string]*regexp.Regexp)}
}

func (e *QualityEvaluator) compileQualityPattern(pattern string) (*regexp.Regexp, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.patterns == nil {
		e.patterns = make(map[string]*regexp.Regexp)
	}
	if cached, ok := e.patterns[pattern]; ok {
		return cached, nil
	}
	re, err := regexp.Compile(pattern) // regexhoist:dynamic
	if err != nil {
		return nil, err
	}
	if len(e.patterns) < qualityPatternCacheCap {
		e.patterns[pattern] = re
	}
	return re, nil
}

func compileQualityPattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern) // regexhoist:dynamic
}

// Sentinel causes for data-quality evaluation envelopes (MODEL-024).
// Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidQualityRule reports a QualityRule that cannot be evaluated:
	// no rule reference, an undeclared kind or severity, no checked path, or
	// a kind missing the parameters its check family requires.
	ErrInvalidQualityRule = errors.New("model: invalid data-quality rule")
)

// QualityStatus is the typed outcome of one evaluation. UNKNOWN is never
// coerced to PASS: missing, stale or unparseable evidence is reported as
// exactly what it is (MODEL-024 RED).
type QualityStatus string

// Quality statuses.
const (
	QualityPass          QualityStatus = "PASS"
	QualityFail          QualityStatus = "FAIL"
	QualityUnknown       QualityStatus = "UNKNOWN"
	QualityNotApplicable QualityStatus = "NOT_APPLICABLE"
)

// Valid reports whether s is one of the four declared statuses.
func (s QualityStatus) Valid() bool {
	switch s {
	case QualityPass, QualityFail, QualityUnknown, QualityNotApplicable:
		return true
	default:
		return false
	}
}

// qualityStatusRank orders statuses from best (PASS) to worst (FAIL) so a
// composed envelope can take the worst status without special-casing: higher
// rank always wins, and UNKNOWN outranks NOT_APPLICABLE and PASS so it is
// never silently dropped by composition.
func qualityStatusRank(s QualityStatus) int {
	switch s {
	case QualityPass:
		return 0
	case QualityNotApplicable:
		return 1
	case QualityUnknown:
		return 2
	case QualityFail:
		return 3
	default:
		return -1
	}
}

// QualitySeverity declares how much an individual finding matters.
type QualitySeverity string

// Quality severities.
const (
	SeverityBlocking QualitySeverity = "BLOCKING"
	SeverityWarning  QualitySeverity = "WARNING"
	SeverityInfo     QualitySeverity = "INFO"
)

// Valid reports whether s is one of the three declared severities.
func (s QualitySeverity) Valid() bool {
	switch s {
	case SeverityBlocking, SeverityWarning, SeverityInfo:
		return true
	default:
		return false
	}
}

// CheckKind is a typed data-quality check family. MODEL-024's REFACTOR
// clause requires quality checks to remain a distinct evaluator family from
// validation, hard invariants and reconciliation: this package evaluates
// exactly these five kinds and no others.
type CheckKind string

// Check kinds.
const (
	CheckPresence    CheckKind = "PRESENCE"
	CheckFormat      CheckKind = "FORMAT"
	CheckRange       CheckKind = "RANGE"
	CheckReferential CheckKind = "REFERENTIAL"
	CheckCrossField  CheckKind = "CROSS_FIELD"
)

// Valid reports whether k is one of the five declared check kinds.
func (k CheckKind) Valid() bool {
	switch k {
	case CheckPresence, CheckFormat, CheckRange, CheckReferential, CheckCrossField:
		return true
	default:
		return false
	}
}

// CrossFieldOperator declares the relation a CROSS_FIELD check requires
// between its two paths' values.
type CrossFieldOperator string

// Cross-field operators.
const (
	CrossFieldEqual    CrossFieldOperator = "EQUAL"
	CrossFieldNotEqual CrossFieldOperator = "NOT_EQUAL"
)

// Valid reports whether o is one of the two declared operators.
func (o CrossFieldOperator) Valid() bool {
	switch o {
	case CrossFieldEqual, CrossFieldNotEqual:
		return true
	default:
		return false
	}
}

// QualityRule declares one typed check over one or more field paths. Only
// the fields its Kind actually uses are meaningful; [QualityRule.Validate]
// enforces that every kind carries what it needs to be evaluable, so a rule
// that reaches [EvaluateRule] can never be a no-op.
type QualityRule struct {
	RuleRef  string
	Kind     CheckKind
	Severity QualitySeverity

	// Paths are the field paths this rule checks. PRESENCE, FORMAT, RANGE
	// and REFERENTIAL each check exactly one path; CROSS_FIELD checks
	// exactly two, compared to each other.
	Paths []string

	// Required is used by CheckPresence: when true, an observed absence
	// (Present=false) fails the check rather than merely noting it.
	Required bool

	// Pattern is used by CheckFormat: a value must match this regular
	// expression.
	Pattern string

	// Min and Max are used by CheckRange. At least one must be set.
	Min, Max *float64

	// AllowedValues is used by CheckReferential: the closed set of values
	// considered valid.
	AllowedValues []string

	// Operator is used by CheckCrossField.
	Operator CrossFieldOperator
}

// Validate rejects a quality rule missing an identity, an undeclared kind or
// severity, no checked path, or parameters its check family requires to be
// evaluable.
func (r QualityRule) Validate() error {
	return validateQualityRule(r, compileQualityPattern)
}

func validateQualityRule(r QualityRule, compile func(string) (*regexp.Regexp, error)) error {
	if r.RuleRef == "" {
		return newError("QualityRule.Validate", "rule_ref", ErrInvalidQualityRule,
			"rule carries no reference")
	}
	if !r.Kind.Valid() {
		return newError("QualityRule.Validate", "kind", ErrInvalidQualityRule,
			"%s has kind %q, outside the five declared check kinds", r.RuleRef, r.Kind)
	}
	if !r.Severity.Valid() {
		return newError("QualityRule.Validate", "severity", ErrInvalidQualityRule,
			"%s has severity %q, outside the three declared severities", r.RuleRef, r.Severity)
	}
	if r.Kind == CheckCrossField {
		if len(r.Paths) != 2 {
			return newError("QualityRule.Validate", "paths", ErrInvalidQualityRule,
				"%s is CROSS_FIELD but declares %d paths, not exactly 2", r.RuleRef, len(r.Paths))
		}
	} else if len(r.Paths) != 1 {
		return newError("QualityRule.Validate", "paths", ErrInvalidQualityRule,
			"%s is %s but declares %d paths, not exactly 1", r.RuleRef, r.Kind, len(r.Paths))
	}
	for _, p := range r.Paths {
		if p == "" {
			return newError("QualityRule.Validate", "paths", ErrInvalidQualityRule,
				"%s declares an empty checked path", r.RuleRef)
		}
	}
	switch r.Kind {
	case CheckFormat:
		if r.Pattern == "" {
			return newError("QualityRule.Validate", "pattern", ErrInvalidQualityRule,
				"%s is FORMAT but declares no pattern", r.RuleRef)
		}
		if _, err := compile(r.Pattern); err != nil {
			return newError("QualityRule.Validate", "pattern", ErrInvalidQualityRule,
				"%s pattern %q does not compile: %v", r.RuleRef, r.Pattern, err)
		}
	case CheckRange:
		if r.Min == nil && r.Max == nil {
			return newError("QualityRule.Validate", "range", ErrInvalidQualityRule,
				"%s is RANGE but declares neither a minimum nor a maximum", r.RuleRef)
		}
		if r.Min != nil && r.Max != nil && *r.Min > *r.Max {
			return newError("QualityRule.Validate", "range", ErrInvalidQualityRule,
				"%s has minimum %v greater than maximum %v", r.RuleRef, *r.Min, *r.Max)
		}
	case CheckReferential:
		if len(r.AllowedValues) == 0 {
			return newError("QualityRule.Validate", "allowed_values", ErrInvalidQualityRule,
				"%s is REFERENTIAL but declares no allowed values", r.RuleRef)
		}
	case CheckCrossField:
		if !r.Operator.Valid() {
			return newError("QualityRule.Validate", "operator", ErrInvalidQualityRule,
				"%s has operator %q, outside the two declared operators", r.RuleRef, r.Operator)
		}
	}
	return nil
}

// QualityFact is one observed field value, carrying its own presence flag and
// source watermark: an evaluator can only ever be as fresh and complete as
// the facts it is given.
type QualityFact struct {
	// Present distinguishes "the source told us this is absent" (a known
	// fact) from "the source never reported this path at all" (missing from
	// the facts map, evaluated as UNKNOWN).
	Present bool

	Value string

	// Watermark is when this fact was last observed. An unset watermark
	// makes the fact's freshness unknowable.
	Watermark values.Instant
}

// Finding is one path's evaluated outcome within a rule.
type Finding struct {
	RuleRef          string
	Path             string
	Status           QualityStatus
	Severity         QualitySeverity
	Detail           string
	RemediationOwner string
}

// QualityResult is the byte-stable, composable envelope [EvaluateRule] and
// [ComposeQualityResults] both return: a status, the findings behind it, the
// oldest source watermark among the facts it drew on, and a digest over its
// own content (MODEL-024 GREEN).
type QualityResult struct {
	RuleRef         string
	Status          QualityStatus
	Findings        []Finding
	SourceWatermark values.Instant
	Digest          string
}

// EvaluateRule evaluates rule against facts as of asOf.
//
// A fact absent from facts, or present with no watermark, or present with a
// watermark older than staleAfterSeconds relative to asOf (when
// staleAfterSeconds is nonzero), evaluates to UNKNOWN: it is never coerced to
// PASS. A fact that is malformed for its check (unparseable RANGE value) or
// that fails its check (absent REQUIRED presence, non-matching FORMAT,
// out-of-bounds RANGE, unlisted REFERENTIAL value, disagreeing CROSS_FIELD
// pair) evaluates to FAIL. Every finding carries remediationOwner
// (MODEL-024 RED/GREEN).
func EvaluateRule(rule QualityRule, facts map[string]QualityFact, asOf values.Instant, staleAfterSeconds uint32, remediationOwner string) (QualityResult, error) {
	return evaluateRule(rule, facts, asOf, staleAfterSeconds, remediationOwner, compileQualityPattern)
}

// EvaluateRule evaluates a rule using this evaluator's explicitly owned
// compiled-pattern cache. The zero value is ready for use. A nil receiver is
// also valid, but does not retain compiled patterns between calls; callers
// evaluating repeatedly should reuse an evaluator returned by
// [NewQualityEvaluator].
func (e *QualityEvaluator) EvaluateRule(rule QualityRule, facts map[string]QualityFact, asOf values.Instant, staleAfterSeconds uint32, remediationOwner string) (QualityResult, error) {
	if e == nil {
		e = NewQualityEvaluator()
	}
	return evaluateRule(rule, facts, asOf, staleAfterSeconds, remediationOwner, e.compileQualityPattern)
}

func evaluateRule(rule QualityRule, facts map[string]QualityFact, asOf values.Instant, staleAfterSeconds uint32, remediationOwner string, compile func(string) (*regexp.Regexp, error)) (QualityResult, error) {
	// Validation must compile FORMAT patterns even when the fact is missing or
	// stale. Retain that result for the actual check so one evaluation never
	// compiles the same pattern twice, including through the package API.
	var compiledPattern *regexp.Regexp
	compileOnce := func(pattern string) (*regexp.Regexp, error) {
		if compiledPattern != nil {
			return compiledPattern, nil
		}
		re, err := compile(pattern)
		if err == nil {
			compiledPattern = re
		}
		return re, err
	}
	if err := validateQualityRule(rule, compileOnce); err != nil {
		return QualityResult{}, err
	}

	var findings []Finding
	var watermark values.Instant
	haveWatermark := false
	trackWatermark := func(f QualityFact) {
		if !f.Watermark.IsSet() {
			return
		}
		if !haveWatermark || f.Watermark.Before(watermark) {
			watermark = f.Watermark
			haveWatermark = true
		}
	}

	finding := func(path string, status QualityStatus, detail string) Finding {
		return Finding{
			RuleRef:          rule.RuleRef,
			Path:             path,
			Status:           status,
			Severity:         rule.Severity,
			Detail:           detail,
			RemediationOwner: remediationOwner,
		}
	}

	// unknownOrNil resolves a single path's fact against presence/freshness
	// rules shared by every non-CROSS_FIELD check. It returns a non-nil
	// finding when the fact itself is unusable (missing or stale), leaving
	// the kind-specific check to run only on usable evidence.
	unknownOrNil := func(path string) (QualityFact, *Finding) {
		f, ok := facts[path]
		if !ok {
			return QualityFact{}, ptr(finding(path, QualityUnknown, "no evidence reported for this path"))
		}
		trackWatermark(f)
		if !f.Watermark.IsSet() {
			return f, ptr(finding(path, QualityUnknown, "evidence carries no source watermark"))
		}
		if staleAfterSeconds > 0 {
			age := asOf.Time().Sub(f.Watermark.Time())
			if age.Seconds() > float64(staleAfterSeconds) {
				return f, ptr(finding(path, QualityUnknown, "evidence watermark is stale"))
			}
		}
		return f, nil
	}

	switch rule.Kind {
	case CheckPresence:
		path := rule.Paths[0]
		f, unk := unknownOrNil(path)
		if unk != nil {
			findings = append(findings, *unk)
		} else if rule.Required && !f.Present {
			findings = append(findings, finding(path, QualityFail, "required value is absent"))
		} else {
			findings = append(findings, finding(path, QualityPass, "present"))
		}

	case CheckFormat:
		path := rule.Paths[0]
		f, unk := unknownOrNil(path)
		if unk != nil {
			findings = append(findings, *unk)
		} else {
			re, err := compileOnce(rule.Pattern)
			if err != nil {
				return QualityResult{}, newError("EvaluateRule", "pattern", ErrInvalidQualityRule,
					"%s pattern %q does not compile: %v", rule.RuleRef, rule.Pattern, err)
			}
			if !re.MatchString(f.Value) {
				findings = append(findings, finding(path, QualityFail,
					fmt.Sprintf("value %q does not match pattern %q", f.Value, rule.Pattern)))
			} else {
				findings = append(findings, finding(path, QualityPass, "matches pattern"))
			}
		}

	case CheckRange:
		path := rule.Paths[0]
		f, unk := unknownOrNil(path)
		if unk != nil {
			findings = append(findings, *unk)
		} else {
			n, err := strconv.ParseFloat(f.Value, 64)
			switch {
			case err != nil:
				findings = append(findings, finding(path, QualityFail,
					fmt.Sprintf("value %q is not numeric", f.Value)))
			case rule.Min != nil && n < *rule.Min:
				findings = append(findings, finding(path, QualityFail,
					fmt.Sprintf("value %v is below minimum %v", n, *rule.Min)))
			case rule.Max != nil && n > *rule.Max:
				findings = append(findings, finding(path, QualityFail,
					fmt.Sprintf("value %v is above maximum %v", n, *rule.Max)))
			default:
				findings = append(findings, finding(path, QualityPass, "within range"))
			}
		}

	case CheckReferential:
		path := rule.Paths[0]
		f, unk := unknownOrNil(path)
		if unk != nil {
			findings = append(findings, *unk)
		} else {
			allowed := false
			for _, v := range rule.AllowedValues {
				if v == f.Value {
					allowed = true
					break
				}
			}
			if !allowed {
				findings = append(findings, finding(path, QualityFail,
					fmt.Sprintf("value %q is not a known reference code", f.Value)))
			} else {
				findings = append(findings, finding(path, QualityPass, "resolves to a known reference code"))
			}
		}

	case CheckCrossField:
		combined := rule.Paths[0] + "," + rule.Paths[1]
		left, unkL := unknownOrNil(rule.Paths[0])
		right, unkR := unknownOrNil(rule.Paths[1])
		switch {
		case unkL != nil:
			findings = append(findings, finding(combined, unkL.Status, unkL.Detail))
		case unkR != nil:
			findings = append(findings, finding(combined, unkR.Status, unkR.Detail))
		default:
			equal := left.Value == right.Value
			var ok bool
			switch rule.Operator {
			case CrossFieldEqual:
				ok = equal
			case CrossFieldNotEqual:
				ok = !equal
			}
			if !ok {
				findings = append(findings, finding(combined, QualityFail,
					fmt.Sprintf("%s %s %s does not hold (%q vs %q)", rule.Paths[0], rule.Operator, rule.Paths[1], left.Value, right.Value)))
			} else {
				findings = append(findings, finding(combined, QualityPass, "cross-field relation holds"))
			}
		}
	}

	overall := QualityStatus(QualityPass)
	for _, f := range findings {
		if qualityStatusRank(f.Status) > qualityStatusRank(overall) {
			overall = f.Status
		}
	}

	result := QualityResult{
		RuleRef:         rule.RuleRef,
		Status:          overall,
		Findings:        findings,
		SourceWatermark: watermark,
	}
	result.Digest = digestQualityResult(result)
	return result, nil
}

func ptr[T any](v T) *T { return &v }

// ComposeQualityResults combines multiple rule results into one envelope:
// the worst status wins (FAIL beats UNKNOWN beats NOT_APPLICABLE beats
// PASS — UNKNOWN is never dropped by composition), findings are concatenated
// and sorted for determinism, and the oldest source watermark among the
// inputs is kept. Composing zero results yields NOT_APPLICABLE: there is
// nothing to have passed or failed. The result's own Digest is a pure
// function of its composed content, so composition is itself byte-stable
// (MODEL-024 GREEN — "an envelope that is byte-stable and composable").
func ComposeQualityResults(results []QualityResult) QualityResult {
	if len(results) == 0 {
		out := QualityResult{RuleRef: "COMPOSED", Status: QualityNotApplicable}
		out.Digest = digestQualityResult(out)
		return out
	}
	status := QualityStatus(QualityPass)
	var findings []Finding
	var watermark values.Instant
	haveWatermark := false
	for _, r := range results {
		if qualityStatusRank(r.Status) > qualityStatusRank(status) {
			status = r.Status
		}
		findings = append(findings, r.Findings...)
		if r.SourceWatermark.IsSet() && (!haveWatermark || r.SourceWatermark.Before(watermark)) {
			watermark = r.SourceWatermark
			haveWatermark = true
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].RuleRef != findings[j].RuleRef {
			return findings[i].RuleRef < findings[j].RuleRef
		}
		return findings[i].Path < findings[j].Path
	})
	out := QualityResult{
		RuleRef:         "COMPOSED",
		Status:          status,
		Findings:        findings,
		SourceWatermark: watermark,
	}
	out.Digest = digestQualityResult(out)
	return out
}

// digestQualityResult computes a stable sha256 digest over a result's
// content, following the exact hand-rolled convention [Registry.Digest]
// establishes in coverage.go: a type-tag prefix, a NUL byte after every
// field, and a "sha256:"-prefixed hex result. Two results built from the
// same findings in the same order always digest identically, and composing
// results is therefore itself deterministic.
func digestQualityResult(r QualityResult) string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	w("QUALITY_RESULT", r.RuleRef, string(r.Status), r.SourceWatermark.String())
	for _, f := range r.Findings {
		w("FINDING", f.RuleRef, f.Path, string(f.Status), string(f.Severity), f.Detail, f.RemediationOwner)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
