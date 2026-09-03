package legal

import (
	"errors"
	"fmt"
)

// Vocabulary, source-type and review-pipeline errors. All are matchable with
// errors.Is.
var (
	// ErrVocabularyVersionUnsupported is the VOCABULARY_VERSION_UNSUPPORTED
	// typed failure from the contract's section 9. A release typed against a
	// vocabulary this build does not know cannot be evaluated as "these
	// kinds do not apply", because the engine cannot tell which kinds the
	// author considered.
	ErrVocabularyVersionUnsupported = errors.New("legal: VOCABULARY_VERSION_UNSUPPORTED")
	// ErrConfidenceMarker is returned when a citation carries no confidence
	// marker.
	ErrConfidenceMarker = errors.New("legal: citation confidence marker is unspecified")
	// ErrSourceType is returned when a pack declares no source type.
	ErrSourceType = errors.New("legal: rule pack source type is unspecified")
)

// VocabularyVersion identifies which [ObligationType] vocabulary a release
// was typed against. It is part of the release digest because adding a kind
// changes what an empty obligation list means: a release typed against v1 and
// read by a v2 engine says "these kinds were not considered", never "these
// kinds do not apply".
type VocabularyVersion uint32

// Obligation vocabularies.
const (
	// VocabularyVersionUnspecified is the zero value. A [RulePack] built in
	// Go before LEGAL-011 carries it, and [RulePack.EffectiveVocabulary]
	// reads it as [VocabularyVersion1] so LEGAL-001's hand-built fixtures
	// keep evaluating unchanged.
	VocabularyVersionUnspecified VocabularyVersion = 0
	// VocabularyVersion1 is LEGAL-001's ten kinds.
	VocabularyVersion1 VocabularyVersion = 1
	// VocabularyVersion2 is LEGAL-011's twenty-two kinds: the original ten
	// plus the twelve in the contract's section 4.2.
	VocabularyVersion2 VocabularyVersion = 2
	// SupportedVocabularyVersion is the newest vocabulary this build can
	// evaluate. Anything above it is [ErrVocabularyVersionUnsupported].
	SupportedVocabularyVersion = VocabularyVersion2
)

// ConfidenceMarker records how sure the authoring pipeline is that a citation
// says what the rule claims it says. It is per-rule, not per-pack: one
// unverified rule does not taint an otherwise checked release, and deleting
// the uncertain rule is never the way to raise a pack's confidence.
type ConfidenceMarker uint8

// Confidence markers, per the contract's section 7.3.
const (
	// ConfidenceMarkerUnspecified is the zero value and is never legal on a
	// registered citation once the pack declares vocabulary 2 or later.
	ConfidenceMarkerUnspecified ConfidenceMarker = iota
	// ConfidenceMarkerConfirmed means the citation was checked against the
	// primary source.
	ConfidenceMarkerConfirmed
	// ConfidenceMarkerVerify means the research marked the point uncertain,
	// or a reviewer could not confirm it. A VERIFY rule may not reach
	// COUNSEL_APPROVED.
	ConfidenceMarkerVerify
	// ConfidenceMarkerDisputed means two sources in the corpus disagree and
	// both are recorded. A DISPUTED rule may not reach COUNSEL_APPROVED.
	ConfidenceMarkerDisputed
)

var confidenceMarkerWire = map[ConfidenceMarker]string{
	ConfidenceMarkerConfirmed: "CONFIRMED",
	ConfidenceMarkerVerify:    "VERIFY",
	ConfidenceMarkerDisputed:  "DISPUTED",
}

// String returns the stable wire token.
func (m ConfidenceMarker) String() string {
	if w, ok := confidenceMarkerWire[m]; ok {
		return w
	}
	return "CONFIDENCE_MARKER_UNSPECIFIED"
}

// ParseConfidenceMarker maps a wire token back to a marker. It rejects the
// unspecified token so a definition file can never silently drop a marker.
func ParseConfidenceMarker(token string) (ConfidenceMarker, error) {
	for m, w := range confidenceMarkerWire {
		if w == token {
			return m, nil
		}
	}
	return ConfidenceMarkerUnspecified, fmt.Errorf("%w: %q", ErrConfidenceMarker, token)
}

// BlocksCounselApproval reports whether a rule carrying this marker prevents
// its pack from reaching [ReviewStatusCounselApproved].
func (m ConfidenceMarker) BlocksCounselApproval() bool {
	return m == ConfidenceMarkerVerify || m == ConfidenceMarkerDisputed || m == ConfidenceMarkerUnspecified
}

// Review statuses added by the contract's section 7.2. The three declared in
// obligation.go keep their ordinals and wire tokens.
const (
	// ReviewStatusVendorBaseline means vendor legal review only; the
	// customer has not approved the interpretation.
	ReviewStatusVendorBaseline ReviewStatus = iota + 3
	// ReviewStatusCustomerDefined means the customer replaced the vendor
	// interpretation with its own.
	ReviewStatusCustomerDefined
	// ReviewStatusRequiresCustomerCounselConfiguration means the release was
	// published deliberately unresolved and blocks evaluation until the
	// tenant configures an interpretation.
	ReviewStatusRequiresCustomerCounselConfiguration
)

// ParseReviewStatus maps a wire token back to a status. It rejects the
// unspecified token: a definition file must state a status explicitly.
func ParseReviewStatus(token string) (ReviewStatus, error) {
	for s, w := range reviewStatusWire {
		if w == token {
			return s, nil
		}
	}
	return ReviewStatusUnspecified, fmt.Errorf("%w: %q", ErrCitationStatus, token)
}

// Releasable reports whether a pack at this review status may be published
// for production evaluation. [ReviewStatusUnreviewed] is fixture-only: every
// pack this repository checks in sits there, and the conformance oracle uses
// this to keep a `?` matrix cell from ever shipping.
func (s ReviewStatus) Releasable() bool {
	return s == ReviewStatusCounselApproved || s == ReviewStatusVendorBaseline || s == ReviewStatusCustomerDefined
}

// SourceType names what kind of authority a release encodes. It is inside the
// digest, because a customer policy and a statute with identical text are not
// the same obligation.
type SourceType uint8

// Source types, per the contract's section 3.1.
const (
	SourceTypeUnspecified SourceType = iota
	SourceTypeStatute
	SourceTypeRegulation
	SourceTypeAgencyGuidance
	SourceTypeCBA
	SourceTypeContract
	SourceTypeCustomerPolicy
)

var sourceTypeWire = map[SourceType]string{
	SourceTypeStatute:        "STATUTE",
	SourceTypeRegulation:     "REGULATION",
	SourceTypeAgencyGuidance: "AGENCY_GUIDANCE",
	SourceTypeCBA:            "CBA",
	SourceTypeContract:       "CONTRACT",
	SourceTypeCustomerPolicy: "CUSTOMER_POLICY",
}

// String returns the stable wire token.
func (t SourceType) String() string {
	if w, ok := sourceTypeWire[t]; ok {
		return w
	}
	return "SOURCE_TYPE_UNSPECIFIED"
}

// ParseSourceType maps a wire token back to a source type.
func ParseSourceType(token string) (SourceType, error) {
	for t, w := range sourceTypeWire {
		if w == token {
			return t, nil
		}
	}
	return SourceTypeUnspecified, fmt.Errorf("%w: %q", ErrSourceType, token)
}

// LifecycleStep names the transaction-lifecycle step an obligation binds to.
// The contract's sections 4.1 and 4.2 assign each kind its steps, and
// [ObligationKindSpec] is the single table that records the assignment: an
// obligation may not bind to a step the contract does not give it.
type LifecycleStep string

// Lifecycle steps, spelled exactly as the contract's sections 4.1 and 4.2
// spell them.
const (
	LifecycleStepDraft      LifecycleStep = "DRAFT"
	LifecycleStepSimulate   LifecycleStep = "SIMULATE"
	LifecycleStepPreflight  LifecycleStep = "PREFLIGHT"
	LifecycleStepApproval   LifecycleStep = "APPROVAL"
	LifecycleStepExecute    LifecycleStep = "EXECUTE"
	LifecycleStepPostCommit LifecycleStep = "POST-COMMIT"
)

// validLifecycleSteps is the closed set of steps an obligation may name.
var validLifecycleSteps = map[LifecycleStep]bool{
	LifecycleStepDraft:      true,
	LifecycleStepSimulate:   true,
	LifecycleStepPreflight:  true,
	LifecycleStepApproval:   true,
	LifecycleStepExecute:    true,
	LifecycleStepPostCommit: true,
}
