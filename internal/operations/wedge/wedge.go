package wedge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// WedgeSchemaVersion is the version of the Gate A evidence contracts owned by
// this package. The records are deliberately persistence-neutral: a caller
// may retain their canonical JSON and digest in its own evidence store.
const WedgeSchemaVersion = 1

var (
	ErrInvalidWedgeRecord = errors.New("commercial: invalid wedge record")
	ErrIncompleteExit     = errors.New("commercial: incomplete pilot exit plan")
	ErrUnsignedThresholds = errors.New("commercial: unsigned pilot thresholds")
)

// EvidenceWindow prevents an aggregate from becoming detached from the
// population and source that produced it.
type EvidenceWindow struct {
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	Source     string    `json:"source"`
	Exclusions []string  `json:"exclusions,omitempty"`
}

func (w EvidenceWindow) validate() error {
	if w.From.IsZero() || w.To.IsZero() || !w.To.After(w.From) || strings.TrimSpace(w.Source) == "" {
		return fmt.Errorf("%w: evidence window requires an ordered interval and source", ErrInvalidWedgeRecord)
	}
	return nil
}

// HandoffTopology identifies the independently owned boundary at which a
// pilot result is handed to a downstream system. It is a claim about
// observation and acknowledgement, never a grant of mutation authority.
type HandoffTopology struct {
	SchemaVersion int                 `json:"schema_version"`
	ID            string              `json:"id"`
	Source        SystemIdentity      `json:"source"`
	Destination   SystemIdentity      `json:"destination"`
	Claim         HandoffClaim        `json:"claim"`
	Observation   ObservationContract `json:"observation"`
	Owner         Accountability      `json:"owner"`
}

type SystemIdentity struct {
	ConnectorID       string `json:"connector_id"`
	SystemID          string `json:"system_id"`
	AuthorityBoundary string `json:"authority_boundary"`
	OwnerID           string `json:"owner_id"`
}

type HandoffClaim struct {
	Purpose             string   `json:"purpose"`
	NarrowedCrossSystem bool     `json:"narrowed_cross_system"`
	Fields              []string `json:"fields"`
	NoMutation          bool     `json:"no_mutation"`
}

type ObservationContract struct {
	Mechanism string `json:"mechanism"`
	AckID     string `json:"ack_id"`
	Evidence  string `json:"evidence"`
}

type Accountability struct {
	OwnerID string `json:"owner_id"`
	Role    string `json:"role"`
}

func (t HandoffTopology) Validate() error {
	if t.SchemaVersion != WedgeSchemaVersion || !machineID(t.ID) {
		return fmt.Errorf("%w: topology identity", ErrInvalidWedgeRecord)
	}
	if !validSystem(t.Source) || !validSystem(t.Destination) || t.Source.SystemID == t.Destination.SystemID {
		return fmt.Errorf("%w: source and destination must be distinct systems", ErrInvalidWedgeRecord)
	}
	if t.Source.OwnerID == t.Destination.OwnerID {
		return fmt.Errorf("%w: source and destination require independent owners", ErrInvalidWedgeRecord)
	}
	if t.Source.AuthorityBoundary == t.Destination.AuthorityBoundary && !t.Claim.NarrowedCrossSystem {
		return fmt.Errorf("%w: shared authority boundary needs an explicit narrowed cross-system claim", ErrInvalidWedgeRecord)
	}
	if strings.TrimSpace(t.Claim.Purpose) == "" || len(t.Claim.Fields) == 0 || !t.Claim.NoMutation {
		return fmt.Errorf("%w: narrowed no-mutation claim is required", ErrInvalidWedgeRecord)
	}
	if t.Source.ConnectorID == t.Destination.ConnectorID && !t.Claim.NarrowedCrossSystem {
		return fmt.Errorf("%w: one connector needs an explicit narrowed cross-system claim", ErrInvalidWedgeRecord)
	}
	if strings.TrimSpace(t.Observation.Mechanism) == "" || strings.TrimSpace(t.Observation.AckID) == "" || strings.TrimSpace(t.Observation.Evidence) == "" {
		return fmt.Errorf("%w: acknowledgement observation is incomplete", ErrInvalidWedgeRecord)
	}
	if !machineID(t.Owner.OwnerID) || strings.TrimSpace(t.Owner.Role) == "" {
		return fmt.Errorf("%w: accountable owner", ErrInvalidWedgeRecord)
	}
	return nil
}

func validSystem(s SystemIdentity) bool {
	return machineID(s.ConnectorID) && machineID(s.SystemID) && machineID(s.AuthorityBoundary) && machineID(s.OwnerID)
}

// BypassReason is a versioned, customer-readable reason for an approved path
// outside Human Capital Management Suite. The unexplained path intentionally has no reason code.
type BypassReason struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type BypassTaxonomy struct {
	Version string         `json:"version"`
	Codes   []BypassReason `json:"codes"`
}

func (t BypassTaxonomy) Validate() error {
	if strings.TrimSpace(t.Version) == "" || len(t.Codes) == 0 {
		return fmt.Errorf("%w: bypass taxonomy identity", ErrInvalidWedgeRecord)
	}
	seen := map[string]bool{}
	for _, code := range t.Codes {
		if !machineID(code.Code) || strings.TrimSpace(code.Label) == "" || seen[code.Code] {
			return fmt.Errorf("%w: bypass taxonomy code %q", ErrInvalidWedgeRecord, code.Code)
		}
		seen[code.Code] = true
	}
	return nil
}

type AdoptionPath string

const (
	AdoptionHCMNext        AdoptionPath = "HCM_NEXT"
	AdoptionApprovedBypass AdoptionPath = "APPROVED_BYPASS"
	AdoptionUnexplained    AdoptionPath = "UNEXPLAINED_EXCEPTION"
)

type AdoptionRecord struct {
	TransactionID string       `json:"transaction_id"`
	Eligible      bool         `json:"eligible"`
	Path          AdoptionPath `json:"path"`
	BypassCode    string       `json:"bypass_code,omitempty"`
}

type AdoptionMetric struct {
	SchemaVersion int              `json:"schema_version"`
	MetricID      string           `json:"metric_id"`
	Window        EvidenceWindow   `json:"window"`
	Taxonomy      BypassTaxonomy   `json:"taxonomy"`
	Records       []AdoptionRecord `json:"records"`
}

type AdoptionResult struct {
	MetricDigest     string `json:"metric_digest"`
	Eligible         int    `json:"eligible"`
	HCMNext          int    `json:"hcm_next"`
	ApprovedBypass   int    `json:"approved_bypass"`
	Unexplained      int    `json:"unexplained_exception"`
	AdoptionBasisPts int    `json:"adoption_basis_points"`
}

func (m AdoptionMetric) Validate() error {
	if m.SchemaVersion != WedgeSchemaVersion || !machineID(m.MetricID) {
		return fmt.Errorf("%w: adoption metric identity", ErrInvalidWedgeRecord)
	}
	if err := m.Window.validate(); err != nil {
		return err
	}
	if err := m.Taxonomy.Validate(); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, code := range m.Taxonomy.Codes {
		known[code.Code] = true
	}
	seen := map[string]bool{}
	for _, record := range m.Records {
		if !record.Eligible {
			continue
		}
		if !machineID(record.TransactionID) || seen[record.TransactionID] {
			return fmt.Errorf("%w: eligible transaction identity", ErrInvalidWedgeRecord)
		}
		seen[record.TransactionID] = true
		switch record.Path {
		case AdoptionHCMNext:
			if record.BypassCode != "" {
				return fmt.Errorf("%w: Human Capital Management Suite path cannot carry a bypass code", ErrInvalidWedgeRecord)
			}
		case AdoptionApprovedBypass:
			if !known[record.BypassCode] {
				return fmt.Errorf("%w: approved bypass %q is not in taxonomy", ErrInvalidWedgeRecord, record.BypassCode)
			}
		case AdoptionUnexplained:
			if record.BypassCode != "" {
				return fmt.Errorf("%w: unexplained exception cannot carry a bypass code", ErrInvalidWedgeRecord)
			}
		default:
			return fmt.Errorf("%w: eligible transaction %q is not classified", ErrInvalidWedgeRecord, record.TransactionID)
		}
	}
	return nil
}

func (m AdoptionMetric) Evaluate() (AdoptionResult, error) {
	if err := m.Validate(); err != nil {
		return AdoptionResult{}, err
	}
	var result AdoptionResult
	for _, record := range m.Records {
		if !record.Eligible {
			continue
		}
		result.Eligible++
		switch record.Path {
		case AdoptionHCMNext:
			result.HCMNext++
		case AdoptionApprovedBypass:
			result.ApprovedBypass++
		case AdoptionUnexplained:
			result.Unexplained++
		}
	}
	if result.Eligible == 0 {
		return AdoptionResult{}, fmt.Errorf("%w: adoption denominator is empty", ErrInvalidWedgeRecord)
	}
	result.AdoptionBasisPts = result.HCMNext * 10000 / result.Eligible
	digest, err := wedgeDigest(m)
	if err != nil {
		return AdoptionResult{}, err
	}
	result.MetricDigest = digest
	return result, nil
}

type CostCategory string

const (
	CostCustomerLabor CostCategory = "CUSTOMER_LABOR"
	CostHCMNext       CostCategory = "HCM_NEXT_COST_TO_SERVE"
)

type CostEvidence struct {
	ID               string         `json:"id"`
	Category         CostCategory   `json:"category"`
	Role             string         `json:"role"`
	Activity         string         `json:"activity"`
	Minutes          int64          `json:"minutes"`
	RateCentsPerHour int64          `json:"rate_cents_per_hour"`
	RateSource       string         `json:"rate_source"`
	Window           EvidenceWindow `json:"window"`
	AllocationMethod string         `json:"allocation_method"`
}

type CostReport struct {
	SchemaVersion int            `json:"schema_version"`
	ReportID      string         `json:"report_id"`
	Evidence      []CostEvidence `json:"evidence"`
}

type CostSummary struct {
	ReportDigest       string `json:"report_digest"`
	CustomerLaborCents int64  `json:"customer_labor_cents"`
	HCMNextCostCents   int64  `json:"hcm_next_cost_cents"`
}

func (r CostReport) Validate() error {
	if r.SchemaVersion != WedgeSchemaVersion || !machineID(r.ReportID) || len(r.Evidence) == 0 {
		return fmt.Errorf("%w: cost report identity and evidence", ErrInvalidWedgeRecord)
	}
	seen := map[string]bool{}
	for _, evidence := range r.Evidence {
		if !machineID(evidence.ID) || seen[evidence.ID] || strings.TrimSpace(evidence.Role) == "" || strings.TrimSpace(evidence.Activity) == "" || evidence.Minutes <= 0 || evidence.RateCentsPerHour < 0 || strings.TrimSpace(evidence.RateSource) == "" || strings.TrimSpace(evidence.AllocationMethod) == "" {
			return fmt.Errorf("%w: cost evidence %q is incomplete", ErrInvalidWedgeRecord, evidence.ID)
		}
		if evidence.Category != CostCustomerLabor && evidence.Category != CostHCMNext {
			return fmt.Errorf("%w: unknown cost category %q", ErrInvalidWedgeRecord, evidence.Category)
		}
		if err := evidence.Window.validate(); err != nil {
			return fmt.Errorf("%w: cost evidence %q: %v", ErrInvalidWedgeRecord, evidence.ID, err)
		}
		seen[evidence.ID] = true
	}
	return nil
}

func (r CostReport) Compile() (CostSummary, error) {
	if err := r.Validate(); err != nil {
		return CostSummary{}, err
	}
	var result CostSummary
	for _, evidence := range r.Evidence {
		cents := evidence.Minutes * evidence.RateCentsPerHour / 60
		switch evidence.Category {
		case CostCustomerLabor:
			result.CustomerLaborCents += cents
		case CostHCMNext:
			result.HCMNextCostCents += cents
		}
	}
	digest, err := wedgeDigest(r)
	if err != nil {
		return CostSummary{}, err
	}
	result.ReportDigest = digest
	return result, nil
}

type PilotMeasurement struct {
	SampleSize           int   `json:"sample_size"`
	AdoptionBasisPoints  int   `json:"adoption_basis_points"`
	ExceptionBasisPoints int   `json:"exception_basis_points"`
	CustomerLaborCents   int64 `json:"customer_labor_cents"`
	HCMNextCostCents     int64 `json:"hcm_next_cost_cents"`
}

type ProceedThreshold struct {
	MinAdoptionBasisPoints int   `json:"min_adoption_basis_points"`
	MaxCustomerLaborCents  int64 `json:"max_customer_labor_cents"`
	MaxHCMNextCostCents    int64 `json:"max_hcm_next_cost_cents"`
}

type ReselectThreshold struct {
	MinAdoptionBasisPoints  int `json:"min_adoption_basis_points"`
	MaxExceptionBasisPoints int `json:"max_exception_basis_points"`
}

type StopThreshold struct {
	MaxExceptionBasisPoints int   `json:"max_exception_basis_points"`
	MaxHCMNextCostCents     int64 `json:"max_hcm_next_cost_cents"`
}

// PilotThresholds is a signed, immutable decision record. Signature is a
// placeholder for the human approval system; the digest binds the exact
// numeric values that were approved.
type PilotThresholds struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	MinSample     int               `json:"min_sample"`
	Proceed       ProceedThreshold  `json:"proceed"`
	Reselect      ReselectThreshold `json:"reselect"`
	Stop          StopThreshold     `json:"stop"`
	ApprovedBy    string            `json:"approved_by"`
	Signature     string            `json:"signature"`
	Digest        string            `json:"digest,omitempty"`
}

type PilotDecisionCode string

const (
	DecisionProceed       PilotDecisionCode = "PROCEED"
	DecisionReselectWedge PilotDecisionCode = "RESELECT_WEDGE"
	DecisionStop          PilotDecisionCode = "STOP"
)

type PilotDecision struct {
	Code            PilotDecisionCode `json:"code"`
	Reason          string            `json:"reason"`
	ThresholdDigest string            `json:"threshold_digest"`
	Measurement     PilotMeasurement  `json:"measurement"`
}

func (t PilotThresholds) Validate() error {
	if t.SchemaVersion != WedgeSchemaVersion || strings.TrimSpace(t.Version) == "" || t.MinSample <= 0 || strings.TrimSpace(t.ApprovedBy) == "" || strings.TrimSpace(t.Signature) == "" {
		return ErrUnsignedThresholds
	}
	if t.Proceed.MinAdoptionBasisPoints < 0 || t.Proceed.MinAdoptionBasisPoints > 10000 || t.Reselect.MinAdoptionBasisPoints < 0 || t.Reselect.MinAdoptionBasisPoints > 10000 || t.Reselect.MaxExceptionBasisPoints < 0 || t.Reselect.MaxExceptionBasisPoints > 10000 || t.Stop.MaxExceptionBasisPoints < 0 || t.Stop.MaxExceptionBasisPoints > 10000 || t.Proceed.MaxCustomerLaborCents < 0 || t.Proceed.MaxHCMNextCostCents < 0 || t.Stop.MaxHCMNextCostCents < 0 {
		return fmt.Errorf("%w: threshold range", ErrInvalidWedgeRecord)
	}
	if t.Stop.MaxExceptionBasisPoints <= t.Reselect.MaxExceptionBasisPoints || t.Stop.MaxHCMNextCostCents <= t.Proceed.MaxHCMNextCostCents || t.Reselect.MinAdoptionBasisPoints >= t.Proceed.MinAdoptionBasisPoints {
		return fmt.Errorf("%w: thresholds do not define ordered numeric bands", ErrInvalidWedgeRecord)
	}
	return nil
}

func (t PilotThresholds) SealDigest() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	copy := t
	copy.Digest = ""
	return wedgeDigest(copy)
}

func (t PilotThresholds) Evaluate(m PilotMeasurement) (PilotDecision, error) {
	if err := t.Validate(); err != nil {
		return PilotDecision{}, err
	}
	if m.SampleSize < 0 || m.AdoptionBasisPoints < 0 || m.ExceptionBasisPoints < 0 || m.AdoptionBasisPoints > 10000 || m.ExceptionBasisPoints > 10000 || m.CustomerLaborCents < 0 || m.HCMNextCostCents < 0 {
		return PilotDecision{}, fmt.Errorf("%w: measurement range", ErrInvalidWedgeRecord)
	}
	digest, err := t.SealDigest()
	if err != nil {
		return PilotDecision{}, err
	}
	decision := PilotDecision{ThresholdDigest: digest, Measurement: m}
	switch {
	case m.ExceptionBasisPoints > t.Stop.MaxExceptionBasisPoints || m.HCMNextCostCents > t.Stop.MaxHCMNextCostCents:
		decision.Code, decision.Reason = DecisionStop, "stop threshold exceeded"
	case m.SampleSize < t.MinSample:
		decision.Code, decision.Reason = DecisionReselectWedge, "minimum sample not reached"
	case m.AdoptionBasisPoints >= t.Proceed.MinAdoptionBasisPoints && m.CustomerLaborCents <= t.Proceed.MaxCustomerLaborCents && m.HCMNextCostCents <= t.Proceed.MaxHCMNextCostCents:
		decision.Code, decision.Reason = DecisionProceed, "all proceed thresholds met"
	default:
		decision.Code, decision.Reason = DecisionReselectWedge, "proceed thresholds not met"
	}
	return decision, nil
}

type PendingDisposition string

const (
	PendingExported      PendingDisposition = "EXPORTED"
	PendingCancelled     PendingDisposition = "CANCELLED"
	PendingManualHandoff PendingDisposition = "MANUAL_HANDOFF"
)

type Revocation struct {
	ConnectorID  string    `json:"connector_id"`
	CredentialID string    `json:"credential_id"`
	Revoked      bool      `json:"revoked"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
}

type PendingWork struct {
	WorkID      string             `json:"work_id"`
	Disposition PendingDisposition `json:"disposition"`
}

type ExportItem struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

type EvidenceExport struct {
	TenantID string       `json:"tenant_id"`
	Complete bool         `json:"complete"`
	Items    []ExportItem `json:"items"`
}

type RetentionHold struct {
	HoldID string    `json:"hold_id"`
	Reason string    `json:"reason"`
	Until  time.Time `json:"until"`
}

type DestructionResponsibility struct {
	OwnerID string `json:"owner_id"`
	Method  string `json:"method"`
}

type PilotExitPlan struct {
	SchemaVersion int                       `json:"schema_version"`
	PlanID        string                    `json:"plan_id"`
	TenantID      string                    `json:"tenant_id"`
	Revocations   []Revocation              `json:"revocations"`
	Pending       []PendingWork             `json:"pending"`
	Export        EvidenceExport            `json:"export"`
	RetentionDays int                       `json:"retention_days"`
	Holds         []RetentionHold           `json:"holds,omitempty"`
	Destruction   DestructionResponsibility `json:"destruction"`
}

type ExitReceipt struct {
	PlanDigest                 string `json:"plan_digest"`
	TenantID                   string `json:"tenant_id"`
	ExportDigest               string `json:"export_digest"`
	ActiveConnectorCredentials int    `json:"active_connector_credentials"`
	PendingManualObligations   int    `json:"pending_manual_obligations"`
}

func (p PilotExitPlan) Validate() error {
	if p.SchemaVersion != WedgeSchemaVersion || !machineID(p.PlanID) || !machineID(p.TenantID) || len(p.Revocations) == 0 || len(p.Pending) == 0 {
		return ErrIncompleteExit
	}
	seen := map[string]bool{}
	for _, revocation := range p.Revocations {
		if !machineID(revocation.ConnectorID) || !machineID(revocation.CredentialID) || !revocation.Revoked || revocation.RevokedAt.IsZero() || seen[revocation.CredentialID] {
			return fmt.Errorf("%w: every credential must be revoked exactly once", ErrIncompleteExit)
		}
		seen[revocation.CredentialID] = true
	}
	for _, work := range p.Pending {
		if !machineID(work.WorkID) || (work.Disposition != PendingExported && work.Disposition != PendingCancelled && work.Disposition != PendingManualHandoff) {
			return fmt.Errorf("%w: pending work %q lacks a disposition", ErrIncompleteExit, work.WorkID)
		}
	}
	if !machineID(p.Export.TenantID) || p.Export.TenantID != p.TenantID || !p.Export.Complete || len(p.Export.Items) == 0 {
		return fmt.Errorf("%w: complete tenant-scoped evidence export is required", ErrIncompleteExit)
	}
	for _, item := range p.Export.Items {
		if strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.Digest) == "" {
			return fmt.Errorf("%w: export item is incomplete", ErrIncompleteExit)
		}
	}
	if p.RetentionDays <= 0 || strings.TrimSpace(p.Destruction.OwnerID) == "" || strings.TrimSpace(p.Destruction.Method) == "" {
		return fmt.Errorf("%w: retention and destruction responsibility are required", ErrIncompleteExit)
	}
	for _, hold := range p.Holds {
		if !machineID(hold.HoldID) || strings.TrimSpace(hold.Reason) == "" || hold.Until.IsZero() {
			return fmt.Errorf("%w: retention hold is incomplete", ErrIncompleteExit)
		}
	}
	return nil
}

// DryRunExit compiles an exit receipt without contacting a connector or
// deleting evidence. A successful receipt proves the plan leaves zero active
// connector credentials in the described authority set.
func DryRunExit(p PilotExitPlan) (ExitReceipt, error) {
	if err := p.Validate(); err != nil {
		return ExitReceipt{}, err
	}
	planDigest, err := wedgeDigest(p)
	if err != nil {
		return ExitReceipt{}, err
	}
	exportDigest, err := wedgeDigest(p.Export)
	if err != nil {
		return ExitReceipt{}, err
	}
	receipt := ExitReceipt{PlanDigest: planDigest, TenantID: p.TenantID, ExportDigest: exportDigest}
	for _, work := range p.Pending {
		if work.Disposition == PendingManualHandoff {
			receipt.PendingManualObligations++
		}
	}
	return receipt, nil
}

func machineID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " /\\\t\r\n") {
		return false
	}
	return true
}

func wedgeDigest(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// SortBypassTaxonomy returns a copy ordered by customer-visible code for
// fixture generation and stable evidence presentation.
func SortBypassTaxonomy(t BypassTaxonomy) BypassTaxonomy {
	t.Codes = append([]BypassReason(nil), t.Codes...)
	sort.Slice(t.Codes, func(i, j int) bool { return t.Codes[i].Code < t.Codes[j].Code })
	return t
}
