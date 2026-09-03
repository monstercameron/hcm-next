// Package model defines the typed representation that CONF-001's parser
// extracts from a reference workflow document under
// planning/reference-workflows. It intentionally has no dependency on any
// execution engine: the conformance runner works on these documents (and
// the scenarios they declare) as data. See runner.Runner for the seam a
// future compiled workflow engine plugs into.
package model

// Kind classifies how much structural depth a Workflow was parsed at.
const (
	// KindDocument marks a workflow parsed from a standalone reference
	// workflow document that has its own execution graph, capability
	// list, and "Required conformance scenarios" section (e.g.
	// manager-change.md, promote-into-management.md).
	KindDocument = "STANDALONE_WORKFLOW_DOCUMENT"

	// KindSuiteSection marks a workflow parsed from one "Reference
	// Workflow N" (or the sixth stress-test) subsection of
	// reference-suite.md. These are conceptual/narrative descriptions,
	// not standalone contract documents, so section-heading-dependent
	// extraction (Sections, Preconditions) is not attempted for them;
	// checks that depend on that depth report UNKNOWN rather than a
	// false PASS or FAIL.
	KindSuiteSection = "SUITE_CONCEPTUAL_SECTION"
)

// Document is everything extracted from one *.md file under
// planning/reference-workflows.
type Document struct {
	// SourcePath is the file path relative to the repository root, using
	// forward slashes regardless of host OS.
	SourcePath string

	// Title is the document's level-1 (# ) heading text.
	Title string

	// Workflows holds one entry per modeled workflow in the document. A
	// standalone document (Kind: KindDocument) contributes exactly one
	// entry for itself. A suite index document (reference-suite.md, Kind:
	// KindSuiteSection per entry) contributes one entry per "Reference
	// Workflow N" / stress-test subsection.
	Workflows []*Workflow

	// FoundationalQuestions holds the suite's numbered "Foundational
	// Questions the Suite Must Close" list. Empty for a document that is
	// not the suite index.
	FoundationalQuestions []Scenario

	// LegacyFixtures holds the "Preserved Legacy Conformance Fixtures"
	// table. Empty for a document that is not the suite index.
	LegacyFixtures []LegacyFixture
}

// Workflow is the typed model of one reference workflow: its steps, the
// primitives it uses, the actors/resolvers it names, the preconditions it
// revalidates, the outcomes it expects, and the adversarial/required
// conformance scenarios it declares.
type Workflow struct {
	// ID is a short, stable, deterministic slug used to key this workflow
	// across runs (the report's stable ordering sorts by ID).
	ID string

	// SourcePath is the originating document's path (see Document).
	SourcePath string

	// Kind is KindDocument or KindSuiteSection.
	Kind string

	// Title is the workflow's heading text (the document title for
	// KindDocument, or the subsection heading for KindSuiteSection).
	Title string

	// RawText is the exact source slice this workflow was parsed from
	// (the whole file for KindDocument, the one subsection for
	// KindSuiteSection). Checks that need line-oriented, spec-citable
	// evidence re-scan RawText rather than re-deriving it from the
	// structured fields below.
	RawText string

	// Sections holds every level-N heading and its body, in document
	// order. Populated only for KindDocument; see KindSuiteSection.
	Sections []Section

	// Diagrams holds every fenced code block found in the document (or
	// subsection), in document order.
	Diagrams []Diagram

	// Steps flattens every diagram's content lines, in document order,
	// tagging each with the section it came from.
	Steps []Step

	// Capabilities holds every distinct dotted governed-capability-style
	// reference found inside a diagram (e.g. "people.worker.read"),
	// sorted.
	Capabilities []string

	// Actors holds every distinct resolver-expression reference found
	// inside a diagram (e.g. "HRBPFor(worker.organization)"), sorted.
	Actors []string

	// Cardinalities holds every distinct approval cardinality keyword
	// found inside a diagram (ONE_OF, ANY_OF, ALL_OF, QUORUM), sorted.
	Cardinalities []string

	// PrimitiveHits holds every literal occurrence of a canonical kernel
	// vocabulary primitive name or a retired name, in document order.
	PrimitiveHits []PrimitiveHit

	// Preconditions holds the content lines of any diagram found under a
	// heading that discusses revalidation (e.g. "Execution-time
	// revalidation"). Populated only for KindDocument.
	Preconditions []string

	// ExpectedOutcomes holds the key/value lines of the first diagram
	// that declares at least three of the five intent completion
	// dimensions (RequestState, ExecutionState, BusinessState,
	// ConsistencyState, ObligationState).
	ExpectedOutcomes []KeyValue

	// Scenarios holds the workflow's declared conformance/adversarial
	// scenarios: the numbered "Required conformance scenarios" list for
	// KindDocument, or the bulleted "This reference workflow must prove:"
	// list for KindSuiteSection.
	Scenarios []Scenario
}

// Section is one heading and its body text, before the next heading of any
// level.
type Section struct {
	Heading string
	Level   int
	Body    string
}

// Diagram is one fenced code block (``` ... ```) found in the document.
type Diagram struct {
	// SectionHeading names the enclosing section, for citation.
	SectionHeading string

	// Lines holds every raw line inside the fence, including pure
	// connector/arrow lines.
	Lines []string

	// Steps holds the subset of Lines that carry actual content, i.e.
	// lines that are not composed entirely of ASCII/box-drawing connector
	// tokens (|, v, ^, +, -, etc).
	Steps []string
}

// Step is one content line from a Diagram, with document-order position.
type Step struct {
	Text           string
	SectionHeading string
	Order          int
}

// PrimitiveHit is one literal occurrence of a kernel vocabulary primitive
// name (core, structural, or retired) in the document text.
type PrimitiveHit struct {
	Primitive string
	Context   string
	Line      int
}

// KeyValue is one "Key   Value" line extracted from a diagram, used for the
// five-dimension completion-state blocks.
type KeyValue struct {
	Key   string
	Value string
}

// Scenario is one numbered or bulleted required-conformance-scenario item.
type Scenario struct {
	Index       int
	Description string
}

// LegacyFixture is one row of the suite's "Preserved Legacy Conformance
// Fixtures" table.
type LegacyFixture struct {
	Name             string
	ContractPressure string
}
