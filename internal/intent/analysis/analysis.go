// Package analysis contains the pure boundary between governed analysis and
// proposed action. An analysis can explain or recommend; it cannot become an
// executable capability merely by being handed to this package.
package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

// Version is the version of the analysis-to-intent contract.
func Version() int { return contractVersion }

var (
	ErrInvalidRequest        = errors.New("analysis: invalid analytical request")
	ErrInvalidResult         = errors.New("analysis: invalid analytical result")
	ErrUnauthorized          = errors.New("analysis: unauthorized analysis or evidence")
	ErrStale                 = errors.New("analysis: stale analytical result")
	ErrRestrictedEvidence    = errors.New("analysis: restricted evidence cannot enter a proposal")
	ErrGovernanceRequired    = errors.New("analysis: fresh governance is required")
	ErrSimulationRequired    = errors.New("analysis: fresh simulation is required")
	ErrExecutionNotGranted   = errors.New("analysis: analysis never grants execution authority")
	ErrInvalidRecommendation = errors.New("analysis: invalid action recommendation")
)

// Watermark pins one source release used by an analytical result. A version
// without a digest is not reproducible evidence.
type Watermark struct {
	SourceRef string
	Version   string
	Digest    string
	Observed  time.Time
}

// EvidenceRef is a selected, redaction-safe reference to analytical evidence.
// It carries lineage metadata rather than a result value, so a recommendation
// cannot smuggle an unauthorized field into a proposed action.
type EvidenceRef struct {
	ID                   string
	SourceRef            string
	TransformationRef    string
	AuthorityRef         string
	FieldPath            string
	Digest               string
	Observed             time.Time
	RequiredAuthorityRef string
	Restricted           bool
}

// Authorization is a server-derived authorization snapshot. The analysis
// package does not mint it; an authn/governance adapter must populate it from
// the verified principal, tenant scope and composed decision.
type Authorization struct {
	Allowed         bool
	TenantID        string
	OrganizationID  string
	PrincipalRef    string
	DecisionRef     string
	DecisionDigest  string
	Purpose         string
	AllowedFields   []string
	AllowedEvidence []string
	AuthorityRefs   []string
	ValidUntil      time.Time
}

// AnalyticalRequest identifies a governed, read-only analysis. Query, cohort
// and model identities are references/digests; raw query text and raw model
// output are intentionally outside this contract.
type AnalyticalRequest struct {
	RequestID       string
	TenantID        string
	OrganizationID  string
	Purpose         string
	DefinitionRef   string
	QueryRef        string
	QueryVersion    string
	QueryDigest     string
	CohortRef       string
	CohortVersion   string
	CohortDigest    string
	ModelRef        string
	ModelVersion    string
	ModelDigest     string
	RequestedFields []string
	Authorization   Authorization
	RequestedAt     time.Time
}

// Uncertainty makes limitations explicit instead of allowing a result to be
// mistaken for an authoritative fact.
type Uncertainty struct {
	Class       string
	Confidence  string
	Limitations []string
}

// AnalyticalResult is the metadata-and-artifact envelope produced by an
// analysis. It has no mutation or effect field by design.
type AnalyticalResult struct {
	ResultID            string
	RequestID           string
	TenantID            string
	OrganizationID      string
	Purpose             string
	AuthorizationDigest string
	DefinitionRef       string
	QueryRef            string
	QueryVersion        string
	QueryDigest         string
	CohortRef           string
	CohortVersion       string
	CohortDigest        string
	ModelRef            string
	ModelVersion        string
	ModelDigest         string
	ArtifactRef         string
	SourceWatermarks    []Watermark
	Evidence            []EvidenceRef
	Uncertainty         Uncertainty
	GeneratedAt         time.Time
	ValidUntil          time.Time
	Digest              string
}

// SourcedResult is the validated result returned by SourceResult. It is an
// alias so callers can name the hand-off according to the stage they are in.
type SourcedResult = AnalyticalResult

// Population is the immutable, authorized population snapshot for a proposed
// action. A count alone is not an authorization proof; the snapshot digest is
// required.
type Population struct {
	TenantID   string
	Ref        string
	Version    string
	Digest     string
	Authorized bool
}

// CausalLink states how the analysis relates to the proposed action. It never
// upgrades descriptive or predictive evidence into a causal fact.
type CausalLink struct {
	Kind        string
	Basis       string
	EvidenceIDs []string
	Confounders []string
	Limitations []string
}

const (
	LinkDescriptive   = "DESCRIPTIVE"
	LinkAssociational = "ASSOCIATIONAL"
	LinkPredictive    = "PREDICTIVE"
	LinkCausal        = "CAUSAL"
)

// ActionSpec identifies the deterministic action capability that a human or
// workflow may later decide to use. It is not an invocation and has no effect
// payload.
type ActionSpec struct {
	DefinitionRef string
	CapabilityRef string
	InputDigest   string
}

// GovernanceEvidence is the fresh, scope-bound decision needed before a
// recommendation becomes a proposal. ALLOW_WITH_OBLIGATIONS is retained as
// a governed draft state; UNKNOWN and DENY are never promotable.
type GovernanceEvidence struct {
	DecisionRef    string
	DecisionDigest string
	State          string
	ScopeDigest    string
	Purpose        string
	EvaluatedAt    time.Time
	ValidUntil     time.Time
}

// SimulationEvidence is the fresh simulation/revalidation result bound to the
// exact action input digest.
type SimulationEvidence struct {
	SimulationRef     string
	SimulationDigest  string
	ActionInputDigest string
	Status            string
	EvaluatedAt       time.Time
	ValidUntil        time.Time
}

// RecommendationRequest is the complete input to RecommendAction. It carries
// only selected evidence references, never arbitrary analytical values.
type RecommendationRequest struct {
	Analysis         SourcedResult
	Authorization    Authorization
	Action           ActionSpec
	SelectedEvidence []string
	Population       Population
	CausalLink       CausalLink
	Governance       GovernanceEvidence
	Simulation       SimulationEvidence
	Now              time.Time
}

// ActionProposal is a separate DRAFT BusinessIntent-shaped artifact. It is
// deliberately non-executable: ExecutionAuthority is always false and this
// package exposes no execution method or side-effect port.
type ActionProposal struct {
	ProposalID         string
	Status             string
	Family             string
	TenantID           string
	OrganizationID     string
	Action             ActionSpec
	AnalysisID         string
	AnalysisDigest     string
	SelectedEvidence   []EvidenceRef
	Uncertainty        Uncertainty
	Population         Population
	CausalLink         CausalLink
	Governance         GovernanceEvidence
	Simulation         SimulationEvidence
	ExecutionAuthority bool
	Digest             string
}

const (
	ProposalDraft        = "DRAFT"
	ProposalFamilyChange = "CHANGE_REQUEST"
	SimulationPass       = "PASS"
	SimulationReady      = "READY"
)

// SourceResult validates an analytical result against its request and mints
// the result digest. A result with missing source/model/cohort versions or
// incomplete lineage is refused before it can be recommended.
func SourceResult(req AnalyticalRequest, result AnalyticalResult) (SourcedResult, error) {
	if err := req.Validate(req.RequestedAt); err != nil {
		return AnalyticalResult{}, err
	}
	if err := result.validateShape(); err != nil {
		return AnalyticalResult{}, err
	}
	if result.RequestID != req.RequestID || result.TenantID != req.TenantID || result.OrganizationID != req.OrganizationID || result.Purpose != req.Purpose {
		return AnalyticalResult{}, fmt.Errorf("%w: result is outside the request scope", ErrInvalidResult)
	}
	if result.AuthorizationDigest != "" && result.AuthorizationDigest != req.Authorization.DecisionDigest {
		return AnalyticalResult{}, fmt.Errorf("%w: result authorization snapshot does not match the request", ErrUnauthorized)
	}
	if result.DefinitionRef != req.DefinitionRef || result.QueryRef != req.QueryRef || result.QueryVersion != req.QueryVersion || result.QueryDigest != req.QueryDigest ||
		result.CohortRef != req.CohortRef || result.CohortVersion != req.CohortVersion || result.CohortDigest != req.CohortDigest ||
		result.ModelRef != req.ModelRef || result.ModelVersion != req.ModelVersion || result.ModelDigest != req.ModelDigest {
		return AnalyticalResult{}, fmt.Errorf("%w: result does not pin the request query, cohort and model versions", ErrInvalidResult)
	}
	result.AuthorizationDigest = req.Authorization.DecisionDigest
	if result.Digest != "" && result.Digest != resultDigest(result) {
		return AnalyticalResult{}, fmt.Errorf("%w: caller-supplied result digest is incorrect", ErrInvalidResult)
	}
	result.Digest = resultDigest(result)
	result.SourceWatermarks = cloneWatermarks(result.SourceWatermarks)
	result.Evidence = cloneEvidence(result.Evidence)
	result.Uncertainty.Limitations = cloneStrings(result.Uncertainty.Limitations)
	return result, nil
}

// ProduceResult is a descriptive alias for SourceResult.
func ProduceResult(req AnalyticalRequest, result AnalyticalResult) (SourcedResult, error) {
	return SourceResult(req, result)
}

// RecommendAction links a fresh, authorized analytical result to a separate
// draft proposal. It never executes the proposed action and never copies an
// analytical value into the proposal.
func RecommendAction(req RecommendationRequest) (ActionProposal, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := req.Analysis.ValidateFresh(now); err != nil {
		return ActionProposal{}, err
	}
	if err := validateRecommendation(req, now); err != nil {
		return ActionProposal{}, err
	}

	selected := make([]EvidenceRef, 0, len(req.SelectedEvidence))
	byID := make(map[string]EvidenceRef, len(req.Analysis.Evidence))
	for _, evidence := range req.Analysis.Evidence {
		byID[evidence.ID] = evidence
	}
	seen := make(map[string]bool, len(req.SelectedEvidence))
	for _, id := range req.SelectedEvidence {
		if seen[id] {
			return ActionProposal{}, fmt.Errorf("%w: evidence %q selected twice", ErrInvalidRecommendation, id)
		}
		seen[id] = true
		evidence, ok := byID[id]
		if !ok {
			return ActionProposal{}, fmt.Errorf("%w: evidence %q is not part of the result", ErrInvalidRecommendation, id)
		}
		if evidence.Restricted {
			return ActionProposal{}, fmt.Errorf("%w: evidence %q is restricted", ErrRestrictedEvidence, id)
		}
		selected = append(selected, evidence)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })

	proposal := ActionProposal{
		ProposalID:         "proposal:" + shortDigest(req.Analysis.Digest+"\x00"+req.Action.InputDigest+"\x00"+req.Population.Digest),
		Status:             ProposalDraft,
		Family:             ProposalFamilyChange,
		TenantID:           req.Analysis.TenantID,
		OrganizationID:     req.Analysis.OrganizationID,
		Action:             req.Action,
		AnalysisID:         req.Analysis.ResultID,
		AnalysisDigest:     req.Analysis.Digest,
		SelectedEvidence:   selected,
		Uncertainty:        cloneUncertainty(req.Analysis.Uncertainty),
		Population:         req.Population,
		CausalLink:         cloneCausalLink(req.CausalLink),
		Governance:         req.Governance,
		Simulation:         req.Simulation,
		ExecutionAuthority: false,
	}
	proposal.Digest = proposalDigest(proposal)
	if err := proposal.Validate(); err != nil {
		return ActionProposal{}, err
	}
	return proposal, nil
}

// Validate re-checks the non-executable proposal boundary. A proposal loaded
// from an older store or altered by a caller cannot acquire execution
// authority by changing its exported fields.
func (p ActionProposal) Validate() error {
	if p.Status != ProposalDraft || p.Family != ProposalFamilyChange || p.ProposalID == "" || p.Digest == "" {
		return fmt.Errorf("%w: proposal is not a complete draft", ErrInvalidRecommendation)
	}
	if p.ExecutionAuthority {
		return ErrExecutionNotGranted
	}
	if p.Digest != proposalDigest(p) {
		return fmt.Errorf("%w: proposal digest does not match its immutable content", ErrInvalidRecommendation)
	}
	return nil
}

// Explain returns a redaction-safe summary of a proposal. It contains no
// source values, selected field values or authority-bearing payload.
func Explain(proposal ActionProposal) string {
	return fmt.Sprintf("analysis proposal status=%s family=%s analysis=%s evidence=%d governed=%s simulated=%s executable=%t digest=%s",
		proposal.Status, proposal.Family, proposal.AnalysisID, len(proposal.SelectedEvidence), proposal.Governance.State, proposal.Simulation.Status, proposal.ExecutionAuthority, proposal.Digest)
}

// ValidateFresh proves that the result is still current at now.
func (r AnalyticalResult) ValidateFresh(now time.Time) error {
	if err := r.validateShape(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !now.Before(r.ValidUntil) || now.Before(r.GeneratedAt) {
		return fmt.Errorf("%w: result valid [%s,%s), checked at %s", ErrStale, r.GeneratedAt.UTC().Format(time.RFC3339Nano), r.ValidUntil.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	}
	for _, watermark := range r.SourceWatermarks {
		if watermark.Observed.After(now) {
			return fmt.Errorf("%w: source watermark %q is in the future", ErrStale, watermark.SourceRef)
		}
	}
	return nil
}

func (r AnalyticalRequest) Validate(now time.Time) error {
	for field, value := range map[string]string{
		"request_id": r.RequestID, "tenant_id": r.TenantID, "definition_ref": r.DefinitionRef,
		"purpose": r.Purpose, "query_ref": r.QueryRef, "query_version": r.QueryVersion,
		"query_digest": r.QueryDigest, "cohort_ref": r.CohortRef, "cohort_version": r.CohortVersion,
		"cohort_digest": r.CohortDigest, "model_ref": r.ModelRef, "model_version": r.ModelVersion,
		"model_digest": r.ModelDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, field)
		}
	}
	if len(r.RequestedFields) == 0 {
		return fmt.Errorf("%w: requested fields are required", ErrInvalidRequest)
	}
	if r.RequestedAt.IsZero() {
		return fmt.Errorf("%w: requested_at is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.OrganizationID) == "" {
		return fmt.Errorf("%w: organization_id is required", ErrInvalidRequest)
	}
	if err := r.Authorization.validate(now, r.TenantID, r.OrganizationID, r.Purpose); err != nil {
		return err
	}
	allowed := stringSet(r.Authorization.AllowedFields)
	for _, field := range r.RequestedFields {
		if field == "" || !allowed[field] {
			return fmt.Errorf("%w: field %q is outside the authorized field scope", ErrUnauthorized, field)
		}
	}
	return nil
}

func (a Authorization) validate(now time.Time, tenant, organization, purpose string) error {
	if !a.Allowed {
		return ErrUnauthorized
	}
	for field, value := range map[string]string{
		"tenant_id": a.TenantID, "principal_ref": a.PrincipalRef, "decision_ref": a.DecisionRef,
		"decision_digest": a.DecisionDigest, "purpose": a.Purpose,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: authorization %s is required", ErrUnauthorized, field)
		}
	}
	if a.TenantID != tenant || a.Purpose != purpose || (organization != "" && a.OrganizationID != organization) {
		return fmt.Errorf("%w: authorization scope does not match request", ErrUnauthorized)
	}
	if len(a.AllowedFields) == 0 || len(a.AllowedEvidence) == 0 {
		return fmt.Errorf("%w: authorized field and evidence scopes are required", ErrUnauthorized)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if a.ValidUntil.IsZero() || !now.Before(a.ValidUntil) {
		return fmt.Errorf("%w: authorization is expired", ErrUnauthorized)
	}
	return nil
}

func (r AnalyticalResult) validateShape() error {
	for field, value := range map[string]string{
		"result_id": r.ResultID, "request_id": r.RequestID, "tenant_id": r.TenantID,
		"purpose":        r.Purpose,
		"definition_ref": r.DefinitionRef, "query_ref": r.QueryRef, "query_version": r.QueryVersion,
		"query_digest": r.QueryDigest, "cohort_ref": r.CohortRef, "cohort_version": r.CohortVersion,
		"cohort_digest": r.CohortDigest, "model_ref": r.ModelRef, "model_version": r.ModelVersion,
		"model_digest": r.ModelDigest, "artifact_ref": r.ArtifactRef,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidResult, field)
		}
	}
	if r.OrganizationID == "" || r.GeneratedAt.IsZero() || r.ValidUntil.IsZero() || !r.ValidUntil.After(r.GeneratedAt) {
		return fmt.Errorf("%w: result scope and validity interval are required", ErrInvalidResult)
	}
	if len(r.SourceWatermarks) == 0 || len(r.Evidence) == 0 {
		return fmt.Errorf("%w: source watermarks and evidence are required", ErrInvalidResult)
	}
	for _, watermark := range r.SourceWatermarks {
		if watermark.SourceRef == "" || watermark.Version == "" || watermark.Digest == "" || watermark.Observed.IsZero() {
			return fmt.Errorf("%w: every source watermark needs source, version, digest and observed time", ErrInvalidResult)
		}
	}
	seen := map[string]bool{}
	for _, evidence := range r.Evidence {
		if evidence.ID == "" || evidence.SourceRef == "" || evidence.TransformationRef == "" || evidence.AuthorityRef == "" || evidence.Digest == "" || evidence.Observed.IsZero() {
			return fmt.Errorf("%w: evidence %q lacks source, transformation, authority, time or digest", ErrInvalidResult, evidence.ID)
		}
		if seen[evidence.ID] {
			return fmt.Errorf("%w: duplicate evidence %q", ErrInvalidResult, evidence.ID)
		}
		seen[evidence.ID] = true
	}
	if r.Uncertainty.Class == "" || r.Uncertainty.Confidence == "" || len(r.Uncertainty.Limitations) == 0 {
		return fmt.Errorf("%w: uncertainty and limitations are required", ErrInvalidResult)
	}
	return nil
}

func validateRecommendation(req RecommendationRequest, now time.Time) error {
	if err := req.Authorization.validate(now, req.Analysis.TenantID, req.Analysis.OrganizationID, req.Analysis.Purpose); err != nil {
		return err
	}
	if req.Analysis.AuthorizationDigest != req.Authorization.DecisionDigest {
		return fmt.Errorf("%w: recommendation authorization does not match the analytical result", ErrUnauthorized)
	}
	if req.Authorization.DecisionDigest == "" {
		return ErrGovernanceRequired
	}
	if req.Action.DefinitionRef == "" || req.Action.CapabilityRef == "" || req.Action.InputDigest == "" {
		return fmt.Errorf("%w: action definition, capability and input digest are required", ErrInvalidRecommendation)
	}
	if len(req.SelectedEvidence) == 0 {
		return fmt.Errorf("%w: at least one evidence reference must be selected", ErrInvalidRecommendation)
	}
	allowedEvidence := stringSet(req.Authorization.AllowedEvidence)
	for _, id := range req.SelectedEvidence {
		if id == "" || !allowedEvidence[id] {
			return fmt.Errorf("%w: evidence %q is outside the authorization snapshot", ErrUnauthorized, id)
		}
	}
	authorities := stringSet(req.Authorization.AuthorityRefs)
	selectedSet := stringSet(req.SelectedEvidence)
	for _, evidence := range req.Analysis.Evidence {
		if selectedSet[evidence.ID] && evidence.RequiredAuthorityRef != "" && !authorities[evidence.RequiredAuthorityRef] {
			return fmt.Errorf("%w: evidence %q requires authority %q", ErrUnauthorized, evidence.ID, evidence.RequiredAuthorityRef)
		}
	}
	if err := validatePopulation(req.Population, req.Analysis.TenantID); err != nil {
		return err
	}
	if err := validateCausalLink(req.CausalLink, req.SelectedEvidence); err != nil {
		return err
	}
	if req.Governance.State != "ALLOW" && req.Governance.State != "ALLOW_WITH_OBLIGATIONS" {
		return fmt.Errorf("%w: state %q is not proposal-eligible", ErrGovernanceRequired, req.Governance.State)
	}
	if req.Governance.DecisionRef == "" || req.Governance.DecisionDigest == "" || req.Governance.ScopeDigest != req.Population.Digest || req.Governance.Purpose != req.Analysis.Purpose || req.Governance.EvaluatedAt.IsZero() || req.Governance.EvaluatedAt.After(now) || req.Governance.ValidUntil.IsZero() || !now.Before(req.Governance.ValidUntil) {
		return fmt.Errorf("%w: governance is missing a fresh scope-bound decision", ErrGovernanceRequired)
	}
	if req.Simulation.Status != SimulationPass && req.Simulation.Status != SimulationReady {
		return fmt.Errorf("%w: status %q is not proposal-eligible", ErrSimulationRequired, req.Simulation.Status)
	}
	if req.Simulation.SimulationRef == "" || req.Simulation.SimulationDigest == "" || req.Simulation.ActionInputDigest != req.Action.InputDigest || req.Simulation.EvaluatedAt.IsZero() || req.Simulation.EvaluatedAt.After(now) || req.Simulation.ValidUntil.IsZero() || !now.Before(req.Simulation.ValidUntil) {
		return fmt.Errorf("%w: simulation is missing a fresh action-bound result", ErrSimulationRequired)
	}
	return nil
}

func validatePopulation(p Population, tenant string) error {
	if p.TenantID == "" || p.TenantID != tenant || p.Ref == "" || p.Version == "" || p.Digest == "" || !p.Authorized {
		return fmt.Errorf("%w: population must be an authorized immutable snapshot", ErrUnauthorized)
	}
	return nil
}

func validateCausalLink(link CausalLink, selected []string) error {
	switch link.Kind {
	case LinkDescriptive, LinkAssociational, LinkPredictive, LinkCausal:
	default:
		return fmt.Errorf("%w: causal link kind %q is not declared", ErrInvalidRecommendation, link.Kind)
	}
	if link.Basis == "" || len(link.EvidenceIDs) == 0 || len(link.Limitations) == 0 {
		return fmt.Errorf("%w: causal link requires basis, evidence and limitations", ErrInvalidRecommendation)
	}
	selectedSet := stringSet(selected)
	for _, id := range link.EvidenceIDs {
		if !selectedSet[id] {
			return fmt.Errorf("%w: causal link cites unselected evidence %q", ErrInvalidRecommendation, id)
		}
	}
	return nil
}

func resultDigest(r AnalyticalResult) string {
	var b strings.Builder
	field(&b, "schema", "hcmnext.analysis.result/v1")
	for _, v := range []string{r.ResultID, r.RequestID, r.TenantID, r.OrganizationID, r.Purpose, r.AuthorizationDigest, r.DefinitionRef, r.QueryRef, r.QueryVersion, r.QueryDigest, r.CohortRef, r.CohortVersion, r.CohortDigest, r.ModelRef, r.ModelVersion, r.ModelDigest, r.ArtifactRef, r.GeneratedAt.UTC().Format(time.RFC3339Nano), r.ValidUntil.UTC().Format(time.RFC3339Nano), r.Uncertainty.Class, r.Uncertainty.Confidence} {
		field(&b, "value", v)
	}
	watermarks := cloneWatermarks(r.SourceWatermarks)
	sort.Slice(watermarks, func(i, j int) bool { return watermarkKey(watermarks[i]) < watermarkKey(watermarks[j]) })
	for _, w := range watermarks {
		field(&b, "watermark", watermarkKey(w))
	}
	evidence := cloneEvidence(r.Evidence)
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ID < evidence[j].ID })
	for _, e := range evidence {
		field(&b, "evidence", evidenceKey(e))
	}
	for _, limitation := range sortedStrings(r.Uncertainty.Limitations) {
		field(&b, "limitation", limitation)
	}
	return sha256Digest(b.String())
}

func proposalDigest(p ActionProposal) string {
	var b strings.Builder
	field(&b, "schema", "hcmnext.analysis.action-proposal/v1")
	for _, v := range []string{p.Status, p.Family, p.TenantID, p.OrganizationID, p.Action.DefinitionRef, p.Action.CapabilityRef, p.Action.InputDigest, p.AnalysisID, p.AnalysisDigest, p.Population.TenantID, p.Population.Ref, p.Population.Version, p.Population.Digest, p.CausalLink.Kind, p.CausalLink.Basis, p.Governance.DecisionRef, p.Governance.DecisionDigest, p.Governance.State, p.Simulation.SimulationRef, p.Simulation.SimulationDigest, p.Simulation.Status} {
		field(&b, "value", v)
	}
	for _, e := range p.SelectedEvidence {
		field(&b, "evidence", evidenceKey(e))
	}
	for _, id := range sortedStrings(p.CausalLink.EvidenceIDs) {
		field(&b, "causal.evidence", id)
	}
	return sha256Digest(b.String())
}

func evidenceKey(e EvidenceRef) string {
	return strings.Join([]string{e.ID, e.SourceRef, e.TransformationRef, e.AuthorityRef, e.FieldPath, e.Digest, e.Observed.UTC().Format(time.RFC3339Nano), e.RequiredAuthorityRef, fmt.Sprint(e.Restricted)}, "\x00")
}

func watermarkKey(w Watermark) string {
	return strings.Join([]string{w.SourceRef, w.Version, w.Digest, w.Observed.UTC().Format(time.RFC3339Nano)}, "\x00")
}

func field(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%d:%s=%d:%s;", len(label), label, len(value), value)
}

func sha256Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func shortDigest(value string) string {
	digest := sha256Digest(value)
	return digest[len("sha256:") : len("sha256:")+24]
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func sortedStrings(values []string) []string {
	out := cloneStrings(values)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func cloneWatermarks(values []Watermark) []Watermark { return append([]Watermark(nil), values...) }

func cloneEvidence(values []EvidenceRef) []EvidenceRef { return append([]EvidenceRef(nil), values...) }

func cloneUncertainty(value Uncertainty) Uncertainty {
	value.Limitations = cloneStrings(value.Limitations)
	return value
}

func cloneCausalLink(value CausalLink) CausalLink {
	value.EvidenceIDs = cloneStrings(value.EvidenceIDs)
	value.Confounders = cloneStrings(value.Confounders)
	value.Limitations = cloneStrings(value.Limitations)
	return value
}
