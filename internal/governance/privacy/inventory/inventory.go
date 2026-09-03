// Package inventory models and validates the executable processing registry
// required by PRIV-001. It intentionally has no dependency on runtime,
// workflow, generated, intent, or domain packages: callers provide governed
// configuration and observed receipts, and this package only validates it.
package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft    Status = "DRAFT"
	StatusApproved Status = "APPROVED"
	StatusRetired  Status = "RETIRED"
)

type ObligationKind string

const (
	MonitoringConsent  ObligationKind = "MONITORING_CONSENT"
	BreachNotification ObligationKind = "BREACH_NOTIFICATION"
)

// ObligationDeadline is scoped to a rule-pack release and jurisdiction. A
// deadline is deliberately not a package-wide/global timer.
type ObligationDeadline struct {
	Kind            ObligationKind `json:"kind"`
	Jurisdiction    string         `json:"jurisdiction"`
	RulePackRelease string         `json:"rule_pack_release"`
	DeadlineHours   int            `json:"deadline_hours"`
	EffectiveFrom   time.Time      `json:"effective_from"`
	EffectiveTo     time.Time      `json:"effective_to,omitempty"`
}

type ProcessingActivity struct {
	ID                     string               `json:"id"`
	Version                string               `json:"version"`
	Status                 Status               `json:"status"`
	Controller             string               `json:"controller"`
	Processor              string               `json:"processor"`
	Subprocessors          []string             `json:"subprocessors"`
	Purpose                string               `json:"purpose"`
	DataSubjects           []string             `json:"data_subjects"`
	DataCategories         []string             `json:"data_categories"`
	Systems                []string             `json:"systems"`
	Recipients             []string             `json:"recipients"`
	Regions                []string             `json:"regions"`
	LawfulBasis            string               `json:"lawful_basis"`
	Retention              string               `json:"retention"`
	SecurityControls       []string             `json:"security_controls"`
	DPIARef                string               `json:"dpia_ref"`
	TransferAssessmentRefs []string             `json:"transfer_assessment_refs"`
	Obligations            []ObligationDeadline `json:"obligations"`
}

type ProcessingDataFlow struct {
	ID                string    `json:"id"`
	ActivityID        string    `json:"activity_id"`
	Version           string    `json:"version"`
	SourceSystem      string    `json:"source_system"`
	DestinationSystem string    `json:"destination_system"`
	Recipient         string    `json:"recipient"`
	Controller        string    `json:"controller"`
	Processor         string    `json:"processor"`
	Subprocessor      string    `json:"subprocessor"`
	DataCategories    []string  `json:"data_categories"`
	Purpose           string    `json:"purpose"`
	Operations        []string  `json:"operations"`
	TransferRegions   []string  `json:"transfer_regions"`
	ContractRefs      []string  `json:"contract_refs"`
	Safeguards        []string  `json:"safeguards"`
	SecurityControls  []string  `json:"security_controls"`
	RetentionRef      string    `json:"retention_ref"`
	EffectiveFrom     time.Time `json:"effective_from"`
	EffectiveTo       time.Time `json:"effective_to,omitempty"`
}

// DataFlowOccurrence is an observed receipt, proving who/where/category was
// actually received for a flow rather than merely being configured.
type DataFlowOccurrence struct {
	ID             string    `json:"id"`
	FlowID         string    `json:"flow_id"`
	Recipient      string    `json:"recipient"`
	Region         string    `json:"region"`
	DataCategories []string  `json:"data_categories"`
	ReceivedAt     time.Time `json:"received_at"`
}

type Inventory struct {
	Activities  []ProcessingActivity `json:"activities"`
	Flows       []ProcessingDataFlow `json:"flows"`
	Occurrences []DataFlowOccurrence `json:"occurrences"`
}

var ErrInvalid = errors.New("privacy inventory: invalid")

func required(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalid, label)
	}
	return nil
}

func nonEmpty(label string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: %s must not be empty", ErrInvalid, label)
	}
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s[%d] is empty", ErrInvalid, label, i)
		}
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (a ProcessingActivity) Validate() error {
	for label, value := range map[string]string{"id": a.ID, "version": a.Version, "controller": a.Controller, "processor": a.Processor, "purpose": a.Purpose, "lawful_basis": a.LawfulBasis, "retention": a.Retention} {
		if err := required(label, value); err != nil {
			return err
		}
	}
	for label, values := range map[string][]string{"data_subjects": a.DataSubjects, "data_categories": a.DataCategories, "systems": a.Systems, "recipients": a.Recipients, "regions": a.Regions, "security_controls": a.SecurityControls} {
		if err := nonEmpty(label, values); err != nil {
			return err
		}
	}
	if a.Status != StatusDraft && a.Status != StatusApproved && a.Status != StatusRetired {
		return fmt.Errorf("%w: unsupported status %q", ErrInvalid, a.Status)
	}
	if a.Status == StatusApproved && a.DPIARef == "" {
		return fmt.Errorf("%w: approved activity %q requires dpia_ref", ErrInvalid, a.ID)
	}
	seen := map[ObligationKind]bool{}
	for _, obligation := range a.Obligations {
		if obligation.Kind != MonitoringConsent && obligation.Kind != BreachNotification {
			return fmt.Errorf("%w: activity %q has unsupported obligation %q", ErrInvalid, a.ID, obligation.Kind)
		}
		if seen[obligation.Kind] {
			return fmt.Errorf("%w: activity %q duplicates obligation %q", ErrInvalid, a.ID, obligation.Kind)
		}
		seen[obligation.Kind] = true
		if err := required("obligation.jurisdiction", obligation.Jurisdiction); err != nil {
			return err
		}
		if err := required("obligation.rule_pack_release", obligation.RulePackRelease); err != nil {
			return err
		}
		if obligation.DeadlineHours <= 0 {
			return fmt.Errorf("%w: activity %q obligation %q has no positive deadline", ErrInvalid, a.ID, obligation.Kind)
		}
		if obligation.EffectiveFrom.IsZero() || (!obligation.EffectiveTo.IsZero() && !obligation.EffectiveFrom.Before(obligation.EffectiveTo)) {
			return fmt.Errorf("%w: activity %q obligation %q has invalid effective interval", ErrInvalid, a.ID, obligation.Kind)
		}
	}
	if a.Status == StatusApproved && (!seen[MonitoringConsent] || !seen[BreachNotification]) {
		return fmt.Errorf("%w: approved activity %q must record monitoring and breach obligations", ErrInvalid, a.ID)
	}
	return nil
}

func (f ProcessingDataFlow) Validate(a ProcessingActivity) error {
	for label, value := range map[string]string{"id": f.ID, "activity_id": f.ActivityID, "version": f.Version, "source_system": f.SourceSystem, "destination_system": f.DestinationSystem, "recipient": f.Recipient, "controller": f.Controller, "processor": f.Processor, "purpose": f.Purpose, "retention_ref": f.RetentionRef} {
		if err := required(label, value); err != nil {
			return err
		}
	}
	for label, values := range map[string][]string{"data_categories": f.DataCategories, "operations": f.Operations, "transfer_regions": f.TransferRegions, "contract_refs": f.ContractRefs, "safeguards": f.Safeguards, "security_controls": f.SecurityControls} {
		if err := nonEmpty(label, values); err != nil {
			return err
		}
	}
	if f.EffectiveFrom.IsZero() || (!f.EffectiveTo.IsZero() && !f.EffectiveFrom.Before(f.EffectiveTo)) {
		return fmt.Errorf("%w: flow %q has invalid effective interval", ErrInvalid, f.ID)
	}
	if !contains(a.Systems, f.SourceSystem) || !contains(a.Systems, f.DestinationSystem) {
		return fmt.Errorf("%w: flow %q systems are absent from activity %q", ErrInvalid, f.ID, a.ID)
	}
	if !contains(a.Recipients, f.Recipient) {
		return fmt.Errorf("%w: flow %q recipient is absent from activity %q", ErrInvalid, f.ID, a.ID)
	}
	if !contains(a.Regions, f.TransferRegions[0]) || !contains(a.DataCategories, f.DataCategories[0]) || f.Purpose != a.Purpose {
		return fmt.Errorf("%w: flow %q exceeds activity scope", ErrInvalid, f.ID)
	}
	return nil
}

func (o DataFlowOccurrence) Validate(f ProcessingDataFlow) error {
	for label, value := range map[string]string{"id": o.ID, "flow_id": o.FlowID, "recipient": o.Recipient, "region": o.Region} {
		if err := required(label, value); err != nil {
			return err
		}
	}
	if o.ReceivedAt.IsZero() {
		return fmt.Errorf("%w: occurrence %q received_at is required", ErrInvalid, o.ID)
	}
	if o.FlowID != f.ID || o.Recipient != f.Recipient || !contains(f.TransferRegions, o.Region) {
		return fmt.Errorf("%w: occurrence %q receipt is outside flow scope", ErrInvalid, o.ID)
	}
	if err := nonEmpty("occurrence.data_categories", o.DataCategories); err != nil {
		return err
	}
	for _, category := range o.DataCategories {
		if !contains(f.DataCategories, category) {
			return fmt.Errorf("%w: occurrence %q category %q is outside flow scope", ErrInvalid, o.ID, category)
		}
	}
	return nil
}

func (i Inventory) Validate() error {
	activities := map[string]ProcessingActivity{}
	for _, a := range i.Activities {
		if err := a.Validate(); err != nil {
			return err
		}
		key := a.ID + "@" + a.Version
		if _, ok := activities[key]; ok {
			return fmt.Errorf("%w: duplicate activity %s", ErrInvalid, key)
		}
		activities[key] = a
	}
	flows := map[string]ProcessingDataFlow{}
	for _, f := range i.Flows {
		a, ok := activities[f.ActivityID+"@"+f.Version]
		if !ok {
			return fmt.Errorf("%w: flow %q has no versioned activity", ErrInvalid, f.ID)
		}
		if err := f.Validate(a); err != nil {
			return err
		}
		if _, ok := flows[f.ID]; ok {
			return fmt.Errorf("%w: duplicate flow %q", ErrInvalid, f.ID)
		}
		flows[f.ID] = f
	}
	for _, o := range i.Occurrences {
		f, ok := flows[o.FlowID]
		if !ok {
			return fmt.Errorf("%w: occurrence %q has no flow", ErrInvalid, o.ID)
		}
		if err := o.Validate(f); err != nil {
			return err
		}
	}
	return nil
}

// Digest provides a stable content address for a validated inventory.
func (i Inventory) Digest() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
