package abuse

// Explanation reports, for one ActivitySignal evaluated against one
// DetectorVersion, whether the detector version is applicable and why.
// It never re-derives a verdict about the activity itself (that is
// ABUSE-002's job): it only reports the applicability of a governed
// detector contract to a governed signal, in closed vocabulary.
type Explanation struct {
	DetectorID string
	Semver     string
	SignalID   string
	SignalKind SignalKind
	// Applicable is true only when SignalKind is one of the
	// DetectorVersion's DeclaredInputs and both inputs are individually
	// valid.
	Applicable bool
	// MatchedInput is the declared input SignalKind matched, when
	// Applicable is true. It is the zero value otherwise.
	MatchedInput SignalKind
	// Reason is a closed-vocabulary code naming why Applicable came out
	// the way it did: SIGNAL_INVALID, DETECTOR_VERSION_INVALID,
	// SIGNAL_KIND_NOT_DECLARED_INPUT, or SIGNAL_KIND_DECLARED_INPUT.
	Reason string
}

const (
	ExplainReasonSignalInvalid          = "SIGNAL_INVALID"
	ExplainReasonDetectorVersionInvalid = "DETECTOR_VERSION_INVALID"
	ExplainReasonNotDeclaredInput       = "SIGNAL_KIND_NOT_DECLARED_INPUT"
	ExplainReasonDeclaredInput          = "SIGNAL_KIND_DECLARED_INPUT"
)

// Explain reports why v is, or is not, applicable to s: which declared
// input matched (if any) and the closed-vocabulary reason. A signal
// carrying raw content, or any other invalid signal, is never matched --
// Explain refuses to name a match for ungoverned input, mirroring the
// same refusal Validate and Registry.Publish enforce elsewhere in this
// package.
func Explain(s ActivitySignal, v DetectorVersion) Explanation {
	exp := Explanation{DetectorID: v.DetectorID, Semver: v.Semver, SignalID: s.ID, SignalKind: s.Kind}

	if err := s.Validate(); err != nil {
		exp.Reason = ExplainReasonSignalInvalid
		return exp
	}
	if err := v.Validate(); err != nil {
		exp.Reason = ExplainReasonDetectorVersionInvalid
		return exp
	}
	if v.ConsumesUndeclaredKind(s.Kind) {
		exp.Reason = ExplainReasonNotDeclaredInput
		return exp
	}
	exp.Applicable = true
	exp.MatchedInput = s.Kind
	exp.Reason = ExplainReasonDeclaredInput
	return exp
}
