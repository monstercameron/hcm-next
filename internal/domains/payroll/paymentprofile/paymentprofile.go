// Package paymentprofile owns the pure, versioned payment-data applicability
// contract for one tenant and one integration. It records decisions and
// boundaries; it never stores payment account data or performs I/O.
package paymentprofile

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

const (
	// BoundaryTestRef is the checked-in integration boundary reference used by
	// the default profile fixtures. A profile cannot cite an unrecognized test.
	BoundaryTestRef         = "internal/domains/payroll/paymentprofile/paymentprofile_test.go#TestTodo_SECARCH_012_Integration"
	SecurityBoundaryTestRef = "internal/domains/payroll/paymentprofile/paymentprofile_test.go#TestTodo_SECARCH_012_Security"
)

var (
	ErrInvalidPaymentDataProfile = errors.New("paymentprofile: invalid payment data profile")
	ErrInvalidDecision           = errors.New("paymentprofile: invalid applicability decision")
	ErrInvalidDataFlow           = errors.New("paymentprofile: invalid data-flow map")
	ErrInvalidReview             = errors.New("paymentprofile: invalid reviewer sign-off")
	ErrMissingDecision           = errors.New("paymentprofile: required applicability decision is missing")
	ErrUnsignedProfile           = errors.New("paymentprofile: profile is unsigned")
	ErrUnknownBoundaryTest       = errors.New("paymentprofile: boundary test reference is unknown")
	ErrNoEffectiveProfile        = errors.New("paymentprofile: no effective profile for tenant and instant")
	ErrAmbiguousProfile          = errors.New("paymentprofile: multiple effective profiles for tenant and instant")
	ErrTenantMismatch            = errors.New("paymentprofile: tenant does not match resolution request")
)

// FieldError is a typed refusal that names the field which made a record
// unusable. Its cause remains matchable with errors.Is.
type FieldError struct {
	Field string
	Cause error
}

func (e *FieldError) Error() string { return fmt.Sprintf("%s: field=%s", e.Cause, e.Field) }
func (e *FieldError) Unwrap() error { return e.Cause }

func fieldError(cause error, field string) error {
	return &FieldError{Field: field, Cause: cause}
}

// DecisionKind is the closed set of payment applicability questions.
type DecisionKind string

const (
	PCIScopeDecision               DecisionKind = "PCI_SCOPE"
	NachaThresholdDecision         DecisionKind = "NACHA_THRESHOLD"
	GLBACoveredInstitutionDecision DecisionKind = "GLBA_COVERED_INSTITUTION"
	FTIDecision                    DecisionKind = "FTI"

	// Descriptive aliases keep callers readable without creating a second wire
	// vocabulary.
	DecisionPCIScope       = PCIScopeDecision
	DecisionNachaThreshold = NachaThresholdDecision
	DecisionGLBA           = GLBACoveredInstitutionDecision
	DecisionFTI            = FTIDecision
)

func (k DecisionKind) valid() bool {
	switch k {
	case PCIScopeDecision, NachaThresholdDecision, GLBACoveredInstitutionDecision, FTIDecision:
		return true
	default:
		return false
	}
}

// Valid reports whether k is one of the four required questions.
func (k DecisionKind) Valid() bool { return k.valid() }

// DecisionOutcome is deliberately binary: unresolved applicability is a
// missing decision and cannot be represented by a permissive zero value.
type DecisionOutcome string

const (
	Applicable    DecisionOutcome = "APPLICABLE"
	NotApplicable DecisionOutcome = "NOT_APPLICABLE"

	PCIInScope                 = Applicable
	PCIOutOfScope              = NotApplicable
	NachaThresholdApplies      = Applicable
	NachaThresholdDoesNotApply = NotApplicable
	GLBACovered                = Applicable
	GLBANotCovered             = NotApplicable
	FTIPresent                 = Applicable
	FTIAbsent                  = NotApplicable
)

func (o DecisionOutcome) valid() bool { return o == Applicable || o == NotApplicable }

// PaymentDataClass names the sensitive payment/government data represented by
// a flow. It is a classification token, never the data itself.
type PaymentDataClass string

const (
	ClassCardholder PaymentDataClass = "CARDHOLDER_DATA"
	ClassACHAccount PaymentDataClass = "ACH_ACCOUNT_DATA"
	ClassFTI        PaymentDataClass = "FEDERAL_TAX_INFORMATION"

	CardholderData = ClassCardholder
	ACHAccountData = ClassACHAccount
	FederalTaxInfo = ClassFTI
)

func (c PaymentDataClass) valid() bool {
	switch c {
	case ClassCardholder, ClassACHAccount, ClassFTI:
		return true
	default:
		return false
	}
}

// FlowNodeKind is the closed vocabulary for typed data-flow nodes.
type FlowNodeKind string

const (
	NodeTenant    FlowNodeKind = "TENANT"
	NodePayroll   FlowNodeKind = "PAYROLL"
	NodePaymethod FlowNodeKind = "PAYMETHOD"
	NodeDLP       FlowNodeKind = "DLP"
	NodeConnector FlowNodeKind = "CONNECTOR"
	NodeExternal  FlowNodeKind = "EXTERNAL_BOUNDARY"
)

func (k FlowNodeKind) valid() bool {
	switch k {
	case NodeTenant, NodePayroll, NodePaymethod, NodeDLP, NodeConnector, NodeExternal:
		return true
	default:
		return false
	}
}

// FlowAction states how a data-flow edge handles classified data.
type FlowAction string

const (
	Stores    FlowAction = "STORES"
	Processes FlowAction = "PROCESSES"
	Transmits FlowAction = "TRANSMITS"
	CanAffect FlowAction = "CAN_AFFECT"
)

func (a FlowAction) valid() bool {
	switch a {
	case Stores, Processes, Transmits, CanAffect:
		return true
	default:
		return false
	}
}

// DataFlowNode is a typed node. PackagePath and ConnectorID make the trust
// boundary auditable without retaining payloads or account identifiers.
type DataFlowNode struct {
	ID          string
	Kind        FlowNodeKind
	PackagePath string
	ConnectorID string
}

func (n DataFlowNode) Validate() error {
	if strings.TrimSpace(n.ID) == "" {
		return fieldError(ErrInvalidDataFlow, "node.id")
	}
	if !n.Kind.valid() {
		return fieldError(ErrInvalidDataFlow, "node.kind")
	}
	if strings.TrimSpace(n.PackagePath) == "" {
		return fieldError(ErrInvalidDataFlow, "node.package_path")
	}
	if n.Kind == NodeConnector && strings.TrimSpace(n.ConnectorID) == "" {
		return fieldError(ErrInvalidDataFlow, "node.connector_id")
	}
	return nil
}

func (n DataFlowNode) Canonical() []byte {
	if n.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.paymentprofile.DataFlowNode", schemaVersion).
		String("id", n.ID).String("kind", string(n.Kind)).String("package_path", n.PackagePath).String("connector_id", n.ConnectorID)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// DataFlowEdge is a typed, classified edge between two node IDs. Every edge
// names the package that owns the decision and the connector it crosses.
type DataFlowEdge struct {
	From        string
	To          string
	PackagePath string
	ConnectorID string
	Action      FlowAction
	Classes     []PaymentDataClass
}

func (e DataFlowEdge) Validate() error {
	if strings.TrimSpace(e.From) == "" {
		return fieldError(ErrInvalidDataFlow, "edge.from")
	}
	if strings.TrimSpace(e.To) == "" {
		return fieldError(ErrInvalidDataFlow, "edge.to")
	}
	if strings.TrimSpace(e.PackagePath) == "" {
		return fieldError(ErrInvalidDataFlow, "edge.package_path")
	}
	if strings.TrimSpace(e.ConnectorID) == "" {
		return fieldError(ErrInvalidDataFlow, "edge.connector_id")
	}
	if !e.Action.valid() {
		return fieldError(ErrInvalidDataFlow, "edge.action")
	}
	if len(e.Classes) == 0 {
		return fieldError(ErrInvalidDataFlow, "edge.classes")
	}
	seen := make(map[PaymentDataClass]struct{}, len(e.Classes))
	for _, class := range e.Classes {
		if !class.valid() {
			return fieldError(ErrInvalidDataFlow, "edge.classes")
		}
		if _, ok := seen[class]; ok {
			return fieldError(ErrInvalidDataFlow, "edge.classes")
		}
		seen[class] = struct{}{}
	}
	return nil
}

func (e DataFlowEdge) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	classes := make([]string, len(e.Classes))
	for i, class := range e.Classes {
		classes[i] = string(class)
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.paymentprofile.DataFlowEdge", schemaVersion).
		String("from", e.From).String("to", e.To).String("package_path", e.PackagePath).
		String("connector_id", e.ConnectorID).String("action", string(e.Action)).SortedStrings("class", classes)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// DataFlowMap is a complete typed map for one applicability decision.
type DataFlowMap struct {
	Nodes           []DataFlowNode
	Edges           []DataFlowEdge
	CanonicalDigest string
}

func (m DataFlowMap) validateShape() error {
	if len(m.Nodes) < 2 {
		return fieldError(ErrInvalidDataFlow, "nodes")
	}
	seen := make(map[string]struct{}, len(m.Nodes))
	for _, node := range m.Nodes {
		if err := node.Validate(); err != nil {
			return err
		}
		if _, ok := seen[node.ID]; ok {
			return fieldError(ErrInvalidDataFlow, "nodes.id")
		}
		seen[node.ID] = struct{}{}
	}
	if len(m.Edges) == 0 {
		return fieldError(ErrInvalidDataFlow, "edges")
	}
	for _, edge := range m.Edges {
		if err := edge.Validate(); err != nil {
			return err
		}
		if _, ok := seen[edge.From]; !ok {
			return fieldError(ErrInvalidDataFlow, "edges.from")
		}
		if _, ok := seen[edge.To]; !ok {
			return fieldError(ErrInvalidDataFlow, "edges.to")
		}
	}
	return nil
}

// Validate checks the graph and, when present, its digest.
func (m DataFlowMap) Validate() error {
	if err := m.validateShape(); err != nil {
		return err
	}
	if m.CanonicalDigest != "" && m.CanonicalDigest != m.computedDigest() {
		return fieldError(ErrInvalidDataFlow, "canonical_digest")
	}
	return nil
}

func (m DataFlowMap) body() []byte {
	nodes := append([]DataFlowNode(nil), m.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	edges := append([]DataFlowEdge(nil), m.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		return edgeKey(edges[i]) < edgeKey(edges[j])
	})
	w := canonicalbytes.New("hcmnext.domains.payroll.paymentprofile.DataFlowMap", schemaVersion).
		Count("nodes", len(nodes)).Count("edges", len(edges))
	for _, node := range nodes {
		w.Value("node", node)
	}
	for _, edge := range edges {
		w.Value("edge", edge)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func edgeKey(e DataFlowEdge) string {
	classes := make([]string, len(e.Classes))
	for i, class := range e.Classes {
		classes[i] = string(class)
	}
	sort.Strings(classes)
	return strings.Join([]string{e.From, e.To, e.PackagePath, e.ConnectorID, string(e.Action), strings.Join(classes, ",")}, "\x00")
}

func (m DataFlowMap) computedDigest() string { return canonicalbytes.Digest(m.body()) }

// Canonical returns the validated graph encoding used by parent records.
func (m DataFlowMap) Canonical() []byte {
	if m.validateShape() != nil {
		return nil
	}
	return m.body()
}

// NewDataFlowMap validates and digests a graph, copying all caller slices.
func NewDataFlowMap(m DataFlowMap) (DataFlowMap, error) {
	m.Nodes = append([]DataFlowNode(nil), m.Nodes...)
	edges := append([]DataFlowEdge(nil), m.Edges...)
	m.Edges = edges
	for i := range m.Edges {
		m.Edges[i].Classes = append([]PaymentDataClass(nil), m.Edges[i].Classes...)
	}
	m.CanonicalDigest = ""
	if err := m.validateShape(); err != nil {
		return DataFlowMap{}, err
	}
	m.CanonicalDigest = m.computedDigest()
	return m, nil
}

// ReviewerSignOff is the signed review fact attached to a profile. The
// signature is represented by an evidence reference; raw credentials never
// enter this package.
type ReviewerSignOff struct {
	PreparedBy      string
	Reviewer        string
	SignatureRef    string
	SignedAt        values.Instant
	CanonicalDigest string
}

func (s ReviewerSignOff) validateShape(preparedBy string) error {
	actor := preparedBy
	if strings.TrimSpace(actor) == "" {
		actor = s.PreparedBy
	}
	if strings.TrimSpace(actor) == "" {
		return fieldError(ErrInvalidReview, "prepared_by")
	}
	if strings.TrimSpace(s.Reviewer) == "" {
		return fieldError(ErrInvalidReview, "reviewer")
	}
	if actor == s.Reviewer {
		return fieldError(ErrInvalidReview, "reviewer")
	}
	if strings.TrimSpace(s.SignatureRef) == "" {
		return fieldError(ErrInvalidReview, "signature_ref")
	}
	if err := s.SignedAt.Validate(); err != nil {
		return fieldError(ErrInvalidReview, "signed_at")
	}
	return nil
}

func (s ReviewerSignOff) Validate() error { return s.validateShape("") }

func (s ReviewerSignOff) body() []byte {
	if s.validateShape(s.PreparedBy) != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.paymentprofile.ReviewerSignOff", schemaVersion).
		String("prepared_by", s.PreparedBy).String("reviewer", s.Reviewer).
		String("signature_ref", s.SignatureRef).Value("signed_at", s.SignedAt)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s ReviewerSignOff) computedDigest() string { return canonicalbytes.Digest(s.body()) }

// Canonical returns the signed review encoding used by the profile digest.
func (s ReviewerSignOff) Canonical() []byte {
	if s.validateShape(s.PreparedBy) != nil {
		return nil
	}
	return s.body()
}

// NewReviewerSignOff creates an immutable signed review fact.
func NewReviewerSignOff(s ReviewerSignOff) (ReviewerSignOff, error) {
	s.CanonicalDigest = ""
	if err := s.validateShape(s.PreparedBy); err != nil {
		return ReviewerSignOff{}, err
	}
	s.CanonicalDigest = s.computedDigest()
	return s, nil
}

// ApplicabilityDecision is one of the four required decisions. Its graph,
// boundary reference, evidence, review window and digest travel together.
type ApplicabilityDecision struct {
	ID                 string
	Kind               DecisionKind
	Outcome            DecisionOutcome
	DataFlow           DataFlowMap
	BoundaryTestRef    string
	EvidenceRef        string
	Effective          values.EffectiveInterval
	Revision           uint64
	SupersedesRevision uint64
	SupersedesDigest   string
	CanonicalDigest    string
}

type Decision = ApplicabilityDecision

func (d ApplicabilityDecision) validateShape() error {
	if strings.TrimSpace(d.ID) == "" {
		return fieldError(ErrInvalidDecision, "decision.id")
	}
	if !d.Kind.valid() {
		return fieldError(ErrInvalidDecision, "decision.kind")
	}
	if !d.Outcome.valid() {
		return fieldError(ErrInvalidDecision, "decision.outcome")
	}
	if err := d.DataFlow.Validate(); err != nil {
		return err
	}
	if !knownBoundaryTest(d.BoundaryTestRef) {
		return fieldError(ErrUnknownBoundaryTest, "decision.boundary_test_ref")
	}
	if strings.TrimSpace(d.EvidenceRef) == "" {
		return fieldError(ErrInvalidDecision, "decision.evidence_ref")
	}
	if d.Revision == 0 {
		return fieldError(ErrInvalidDecision, "decision.revision")
	}
	if d.Revision == 1 && (d.SupersedesRevision != 0 || d.SupersedesDigest != "") {
		return fieldError(ErrInvalidDecision, "decision.supersedes_revision")
	}
	if d.Revision > 1 && (d.SupersedesRevision == 0 || d.SupersedesRevision >= d.Revision || strings.TrimSpace(d.SupersedesDigest) == "") {
		return fieldError(ErrInvalidDecision, "decision.supersedes_digest")
	}
	if err := d.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidDecision, "decision.effective")
	}
	return nil
}

func (d ApplicabilityDecision) Validate() error {
	if err := d.validateShape(); err != nil {
		return err
	}
	if d.CanonicalDigest != "" && d.CanonicalDigest != d.computedDigest() {
		return fieldError(ErrInvalidDecision, "decision.canonical_digest")
	}
	return nil
}

func (d ApplicabilityDecision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.payroll.paymentprofile.ApplicabilityDecision", schemaVersion).
		String("id", d.ID).String("kind", string(d.Kind)).String("outcome", string(d.Outcome)).
		Value("data_flow", d.DataFlow).String("boundary_test_ref", d.BoundaryTestRef).
		String("evidence_ref", d.EvidenceRef).Value("effective", d.Effective).
		Int("revision", int64(d.Revision)).Int("supersedes_revision", int64(d.SupersedesRevision)).String("supersedes_digest", d.SupersedesDigest)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (d ApplicabilityDecision) computedDigest() string { return canonicalbytes.Digest(d.body()) }

// Canonical returns the validated decision encoding used by the profile.
func (d ApplicabilityDecision) Canonical() []byte {
	if d.validateShape() != nil {
		return nil
	}
	return d.body()
}

// NewApplicabilityDecision validates, deep-copies and digests one decision.
func NewApplicabilityDecision(d ApplicabilityDecision) (ApplicabilityDecision, error) {
	flow, err := NewDataFlowMap(d.DataFlow)
	if err != nil {
		return ApplicabilityDecision{}, err
	}
	d.DataFlow = flow
	d.CanonicalDigest = ""
	if err := d.validateShape(); err != nil {
		return ApplicabilityDecision{}, err
	}
	d.CanonicalDigest = d.computedDigest()
	return d, nil
}

// PaymentDataProfile is one immutable, effective-dated tenant/integration
// revision. A valid value contains exactly one decision for each required
// applicability question and an independent reviewer sign-off.
type PaymentDataProfile struct {
	TenantRef          string
	IntegrationRef     string
	ConnectorVersion   connectivity.Version
	Revision           uint64
	SupersedesRevision uint64
	SupersedesDigest   string
	Decisions          []ApplicabilityDecision
	Effective          values.EffectiveInterval
	PreparedBy         string
	Review             ReviewerSignOff
	CanonicalDigest    string
}

type Profile = PaymentDataProfile

func (p PaymentDataProfile) validateShape() error {
	if strings.TrimSpace(p.TenantRef) == "" {
		return fieldError(ErrInvalidPaymentDataProfile, "tenant_ref")
	}
	if strings.TrimSpace(p.IntegrationRef) == "" {
		return fieldError(ErrInvalidPaymentDataProfile, "integration_ref")
	}
	if p.ConnectorVersion.IsZero() {
		return fieldError(ErrInvalidPaymentDataProfile, "connector_version")
	}
	if p.Revision == 0 {
		return fieldError(ErrInvalidPaymentDataProfile, "revision")
	}
	if p.Revision == 1 && (p.SupersedesRevision != 0 || p.SupersedesDigest != "") {
		return fieldError(ErrInvalidPaymentDataProfile, "supersedes_revision")
	}
	if p.Revision > 1 && (p.SupersedesRevision == 0 || p.SupersedesRevision >= p.Revision || strings.TrimSpace(p.SupersedesDigest) == "") {
		return fieldError(ErrInvalidPaymentDataProfile, "supersedes_digest")
	}
	if err := p.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidPaymentDataProfile, "effective")
	}
	seen := make(map[DecisionKind]struct{}, len(p.Decisions))
	for _, decision := range p.Decisions {
		if err := decision.Validate(); err != nil {
			return err
		}
		if decision.CanonicalDigest == "" {
			return fieldError(ErrUnsignedProfile, "decisions."+string(decision.Kind)+".canonical_digest")
		}
		if decision.DataFlow.CanonicalDigest == "" {
			return fieldError(ErrUnsignedProfile, "decisions."+string(decision.Kind)+".data_flow.canonical_digest")
		}
		if _, ok := seen[decision.Kind]; ok {
			return fieldError(ErrInvalidDecision, "decisions."+string(decision.Kind))
		}
		seen[decision.Kind] = struct{}{}
	}
	for _, kind := range []DecisionKind{PCIScopeDecision, NachaThresholdDecision, GLBACoveredInstitutionDecision, FTIDecision} {
		if _, ok := seen[kind]; !ok {
			return fieldError(ErrMissingDecision, "decisions."+string(kind))
		}
	}
	if err := p.Review.validateShape(p.PreparedBy); err != nil {
		return err
	}
	if p.Review.CanonicalDigest == "" {
		return fieldError(ErrUnsignedProfile, "review.canonical_digest")
	}
	if p.Review.CanonicalDigest != p.Review.computedDigest() {
		return fieldError(ErrInvalidReview, "review.canonical_digest")
	}
	return nil
}

// Validate checks the profile and refuses an unsigned or tampered revision.
func (p PaymentDataProfile) Validate() error {
	if err := p.validateShape(); err != nil {
		return err
	}
	if strings.TrimSpace(p.CanonicalDigest) == "" {
		return fieldError(ErrUnsignedProfile, "canonical_digest")
	}
	if p.CanonicalDigest != p.computedDigest() {
		return fieldError(ErrInvalidPaymentDataProfile, "canonical_digest")
	}
	return nil
}

func (p PaymentDataProfile) body() []byte {
	decisions := append([]ApplicabilityDecision(nil), p.Decisions...)
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Kind < decisions[j].Kind })
	w := canonicalbytes.New("hcmnext.domains.payroll.paymentprofile.PaymentDataProfile", schemaVersion).
		String("tenant_ref", p.TenantRef).String("integration_ref", p.IntegrationRef).
		String("connector_version", p.ConnectorVersion.String()).Int("revision", int64(p.Revision)).
		Int("supersedes_revision", int64(p.SupersedesRevision)).String("supersedes_digest", p.SupersedesDigest).
		Value("effective", p.Effective).String("prepared_by", p.PreparedBy).Value("review", p.Review).
		Count("decisions", len(decisions))
	for _, decision := range decisions {
		w.Value("decision", decision)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p PaymentDataProfile) computedDigest() string { return canonicalbytes.Digest(p.body()) }

// NewPaymentDataProfile validates, deep-copies and digests a profile.
func NewPaymentDataProfile(p PaymentDataProfile) (PaymentDataProfile, error) {
	p.Decisions = append([]ApplicabilityDecision(nil), p.Decisions...)
	for i := range p.Decisions {
		decision, err := NewApplicabilityDecision(p.Decisions[i])
		if err != nil {
			return PaymentDataProfile{}, err
		}
		p.Decisions[i] = decision
	}
	review, err := NewReviewerSignOff(ReviewerSignOff{
		PreparedBy: p.PreparedBy, Reviewer: p.Review.Reviewer,
		SignatureRef: p.Review.SignatureRef, SignedAt: p.Review.SignedAt,
	})
	if err != nil {
		return PaymentDataProfile{}, err
	}
	p.Review = review
	p.CanonicalDigest = ""
	if err := p.validateShape(); err != nil {
		return PaymentDataProfile{}, err
	}
	p.CanonicalDigest = p.computedDigest()
	return p, nil
}

// NewProfile is a descriptive constructor alias.
func NewProfile(p PaymentDataProfile) (PaymentDataProfile, error) { return NewPaymentDataProfile(p) }

// ProfileExplanation is deliberately content-free: it identifies only the
// revision shape and digest, never tenant IDs, connector IDs or reviewer IDs.
type ProfileExplanation struct {
	Revision      uint64
	DecisionCount int
	FlowCount     int
	Digest        string
}

// Explain returns an audit-safe summary of a signed profile.
func (p PaymentDataProfile) Explain() (ProfileExplanation, error) {
	if err := p.Validate(); err != nil {
		return ProfileExplanation{}, err
	}
	flowCount := 0
	for _, decision := range p.Decisions {
		flowCount += len(decision.DataFlow.Edges)
	}
	return ProfileExplanation{Revision: p.Revision, DecisionCount: len(p.Decisions), FlowCount: flowCount, Digest: p.CanonicalDigest}, nil
}

// Explain is the package-level audit-safe entry point.
func Explain(p PaymentDataProfile) (ProfileExplanation, error) { return p.Explain() }

// Resolve selects the one signed profile for tenant that contains at. It is
// intentionally ambiguous when multiple integrations are effective; callers
// must resolve a connector explicitly rather than silently widening scope.
func Resolve(tenant string, at values.Instant, profiles []PaymentDataProfile) (PaymentDataProfile, error) {
	if strings.TrimSpace(tenant) == "" {
		return PaymentDataProfile{}, fieldError(ErrTenantMismatch, "tenant")
	}
	if err := at.Validate(); err != nil {
		return PaymentDataProfile{}, fieldError(ErrNoEffectiveProfile, "instant")
	}
	var matches []PaymentDataProfile
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return PaymentDataProfile{}, err
		}
		if profile.TenantRef != tenant {
			continue
		}
		active, err := containsInstant(profile.Effective, at)
		if err != nil {
			return PaymentDataProfile{}, fieldError(ErrNoEffectiveProfile, "effective")
		}
		if !active {
			continue
		}
		for _, decision := range profile.Decisions {
			active, err := containsInstant(decision.Effective, at)
			if err != nil || !active {
				return PaymentDataProfile{}, fieldError(ErrMissingDecision, "decisions."+string(decision.Kind)+".effective")
			}
		}
		matches = append(matches, profile)
	}
	switch len(matches) {
	case 0:
		return PaymentDataProfile{}, fieldError(ErrNoEffectiveProfile, "profile")
	case 1:
		return matches[0], nil
	default:
		return PaymentDataProfile{}, fieldError(ErrAmbiguousProfile, "profiles")
	}
}

// ResolveForIntegration resolves an explicitly named connector when a tenant
// has more than one integration profile.
func ResolveForIntegration(tenant, integration string, at values.Instant, profiles []PaymentDataProfile) (PaymentDataProfile, error) {
	if strings.TrimSpace(integration) == "" {
		return PaymentDataProfile{}, fieldError(ErrTenantMismatch, "integration")
	}
	filtered := make([]PaymentDataProfile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.IntegrationRef == integration {
			filtered = append(filtered, profile)
		}
	}
	return Resolve(tenant, at, filtered)
}

func containsInstant(interval values.EffectiveInterval, at values.Instant) (bool, error) {
	if interval.Kind() == values.IntervalKindInstant {
		return interval.ContainsInstant(at)
	}
	if interval.Kind() == values.IntervalKindLocalDate {
		date, err := values.NewLocalDate(at.Time().Year(), at.Time().Month(), at.Time().Day())
		if err != nil {
			return false, err
		}
		return interval.ContainsDate(date)
	}
	return false, errors.New("paymentprofile: effective interval is unset")
}

func knownBoundaryTest(ref string) bool {
	return ref == BoundaryTestRef || ref == SecurityBoundaryTestRef
}
