package operations

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// OPS008SchemaVersion identifies the runbook and rehearsal evidence shape.
	OPS008SchemaVersion = 1

	StatusRehearsalReady    = "READY"
	StatusRehearsalRejected = "OPS_008_REJECTED"
)

// Runbook is the minimum critical operating procedure required by OPS-008.
// Text values beginning with PLACEHOLDER are mechanics fixtures only; a human
// must replace them with the selected service's real operating decisions.
type Runbook struct {
	ID                string        `json:"id"`
	Version           string        `json:"version"`
	Service           string        `json:"service"`
	Critical          bool          `json:"critical"`
	Owner             string        `json:"owner"`
	BackupOwner       string        `json:"backup_owner"`
	Trigger           string        `json:"trigger"`
	Diagnosis         string        `json:"diagnosis"`
	Containment       string        `json:"containment"`
	RollbackOrDegrade string        `json:"rollback_or_degrade"`
	Verification      string        `json:"verification"`
	Communication     string        `json:"communication"`
	ExpiresAt         time.Time     `json:"expires_at"`
	AcknowledgementBy time.Duration `json:"acknowledgement_by"`
	EscalationBy      time.Duration `json:"escalation_by"`
	HandoffBy         time.Duration `json:"handoff_by"`
}

// RehearsalCase records observed timing for one synthetic page or support
// case. It contains no tenant payload and is safe to evaluate in memory.
type RehearsalCase struct {
	ID             string    `json:"id"`
	RunbookID      string    `json:"runbook_id"`
	TriggeredAt    time.Time `json:"triggered_at"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
	EscalatedAt    time.Time `json:"escalated_at"`
	HandedOffAt    time.Time `json:"handed_off_at"`
	EvidenceRef    string    `json:"evidence_ref"`
}

// RehearsalDiagnostic describes the exact failed operational target.
type RehearsalDiagnostic struct {
	CaseID    string        `json:"case_id"`
	RunbookID string        `json:"runbook_id"`
	Field     string        `json:"field"`
	State     string        `json:"state"`
	Version   string        `json:"version"`
	Observed  time.Duration `json:"observed"`
	Target    time.Duration `json:"target"`
	Reason    string        `json:"reason"`
}

func (d RehearsalDiagnostic) String() string {
	return fmt.Sprintf("case=%s runbook=%s field=%s state=%s version=%s: %s", d.CaseID, d.RunbookID, d.Field, d.State, d.Version, d.Reason)
}

// RehearsalResult is the pure OPS-008 readiness result.
type RehearsalResult struct {
	Status      string                `json:"status"`
	Runbooks    int                   `json:"runbooks"`
	Cases       int                   `json:"cases"`
	Diagnostics []RehearsalDiagnostic `json:"diagnostics"`
	Effects     ZeroEffects           `json:"effects"`
}

// Ready reports whether every critical runbook and synthetic rehearsal met its
// declared acknowledgement, escalation and handoff targets.
func (r RehearsalResult) Ready() bool { return r.Status == StatusRehearsalReady }

// Version reports the OPS-008 evidence contract version.
func Version() int { return OPS008SchemaVersion }

// Explain describes the evidence boundary consumed by operations planning.
func Explain() string {
	return "OPS-008 v1: complete critical runbooks with synthetic acknowledgement, escalation, and support-handoff evidence"
}

// ValidateRunbook checks one runbook without consulting the clock. Expiry is
// checked by Rehearse at its explicit evaluation instant.
func ValidateRunbook(runbook Runbook) []RehearsalDiagnostic {
	var diagnostics []RehearsalDiagnostic
	add := func(field, state, reason string) {
		diagnostics = append(diagnostics, RehearsalDiagnostic{RunbookID: runbook.ID, Field: field, State: state, Version: runbook.Version, Reason: reason})
	}
	if strings.TrimSpace(runbook.ID) == "" {
		add("id", "MISSING", "runbook id is required")
	}
	if strings.TrimSpace(runbook.Version) == "" {
		add("version", "MISSING", "runbook version is required")
	}
	if strings.TrimSpace(runbook.Service) == "" {
		add("service", "MISSING", "service is required")
	}
	if !runbook.Critical {
		add("critical", "INVALID", "OPS-008 requires critical runbooks")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"owner", runbook.Owner}, {"backup_owner", runbook.BackupOwner}, {"trigger", runbook.Trigger},
		{"diagnosis", runbook.Diagnosis}, {"containment", runbook.Containment}, {"rollback_or_degrade", runbook.RollbackOrDegrade},
		{"verification", runbook.Verification}, {"communication", runbook.Communication},
	} {
		if strings.TrimSpace(field.value) == "" {
			add(field.name, "MISSING", field.name+" is required")
		}
	}
	if runbook.Owner != "" && runbook.Owner == runbook.BackupOwner {
		add("backup_owner", "INVALID", "backup owner must be independent")
	}
	if runbook.ExpiresAt.IsZero() {
		add("expires_at", "MISSING", "runbook expiry is required")
	}
	if runbook.AcknowledgementBy <= 0 {
		add("acknowledgement_by", "MISSING", "acknowledgement target is required")
	}
	if runbook.EscalationBy <= 0 {
		add("escalation_by", "MISSING", "escalation target is required")
	}
	if runbook.HandoffBy <= 0 {
		add("handoff_by", "MISSING", "handoff target is required")
	}
	sort.SliceStable(diagnostics, func(i, j int) bool { return diagnostics[i].Field < diagnostics[j].Field })
	return diagnostics
}

// Rehearse evaluates runbooks and observed synthetic cases at now. It never
// persists, pages, sends messages, changes business state, or calls a
// provider, so rejected evidence has explicit zero effects.
func Rehearse(runbooks []Runbook, cases []RehearsalCase, now time.Time) RehearsalResult {
	result := RehearsalResult{Status: StatusRehearsalRejected, Runbooks: len(runbooks), Cases: len(cases), Effects: ZeroEffects{}}
	byID := make(map[string]Runbook, len(runbooks))
	for _, runbook := range runbooks {
		result.Diagnostics = append(result.Diagnostics, ValidateRunbook(runbook)...)
		if prior, exists := byID[runbook.ID]; exists {
			result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{RunbookID: runbook.ID, Field: "id", State: "DUPLICATE", Version: prior.Version, Reason: "runbook id is registered more than once"})
		}
		byID[runbook.ID] = runbook
		if !runbook.ExpiresAt.IsZero() && !runbook.ExpiresAt.After(now) {
			result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{RunbookID: runbook.ID, Field: "expires_at", State: "EXPIRED", Version: runbook.Version, Reason: "runbook evidence has expired"})
		}
	}
	if len(runbooks) == 0 {
		result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{Field: "runbooks", State: "MISSING", Reason: "at least one critical runbook is required"})
	}
	seenCases := map[string]bool{}
	for _, rehearsal := range cases {
		if seenCases[rehearsal.ID] {
			result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{CaseID: rehearsal.ID, RunbookID: rehearsal.RunbookID, Field: "id", State: "DUPLICATE", Reason: "rehearsal case id is registered more than once"})
		}
		seenCases[rehearsal.ID] = true
		runbook, exists := byID[rehearsal.RunbookID]
		if !exists {
			result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{CaseID: rehearsal.ID, RunbookID: rehearsal.RunbookID, Field: "runbook_id", State: "UNKNOWN", Reason: "rehearsal case has no runbook"})
			continue
		}
		validateCase(&result, runbook, rehearsal)
	}
	if len(cases) == 0 {
		result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{Field: "cases", State: "MISSING", Reason: "at least one synthetic rehearsal case is required"})
	}
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].CaseID != result.Diagnostics[j].CaseID {
			return result.Diagnostics[i].CaseID < result.Diagnostics[j].CaseID
		}
		if result.Diagnostics[i].RunbookID != result.Diagnostics[j].RunbookID {
			return result.Diagnostics[i].RunbookID < result.Diagnostics[j].RunbookID
		}
		return result.Diagnostics[i].Field < result.Diagnostics[j].Field
	})
	if len(result.Diagnostics) == 0 {
		result.Status = StatusRehearsalReady
	}
	return result
}

func validateCase(result *RehearsalResult, runbook Runbook, rehearsal RehearsalCase) {
	add := func(field, state string, observed, target time.Duration, reason string) {
		result.Diagnostics = append(result.Diagnostics, RehearsalDiagnostic{CaseID: rehearsal.ID, RunbookID: runbook.ID, Field: field, State: state, Version: runbook.Version, Observed: observed, Target: target, Reason: reason})
	}
	if strings.TrimSpace(rehearsal.ID) == "" || strings.TrimSpace(rehearsal.EvidenceRef) == "" {
		add("identity", "MISSING", 0, 0, "case id and evidence reference are required")
	}
	if rehearsal.TriggeredAt.IsZero() || rehearsal.AcknowledgedAt.IsZero() || rehearsal.EscalatedAt.IsZero() || rehearsal.HandedOffAt.IsZero() {
		add("timestamps", "MISSING", 0, 0, "trigger, acknowledgement, escalation and handoff timestamps are required")
		return
	}
	if rehearsal.AcknowledgedAt.Before(rehearsal.TriggeredAt) || rehearsal.EscalatedAt.Before(rehearsal.AcknowledgedAt) || rehearsal.HandedOffAt.Before(rehearsal.EscalatedAt) {
		add("timestamps", "INVALID", 0, 0, "rehearsal timestamps must be ordered")
		return
	}
	ack := rehearsal.AcknowledgedAt.Sub(rehearsal.TriggeredAt)
	escalation := rehearsal.EscalatedAt.Sub(rehearsal.TriggeredAt)
	handoff := rehearsal.HandedOffAt.Sub(rehearsal.TriggeredAt)
	if ack > runbook.AcknowledgementBy {
		add("acknowledgement", "TARGET_BREACHED", ack, runbook.AcknowledgementBy, "acknowledgement exceeded target")
	}
	if escalation > runbook.EscalationBy {
		add("escalation", "TARGET_BREACHED", escalation, runbook.EscalationBy, "escalation exceeded target")
	}
	if handoff > runbook.HandoffBy {
		add("handoff", "TARGET_BREACHED", handoff, runbook.HandoffBy, "support handoff exceeded target")
	}
}

// PlaceholderRunbooks supplies mechanics fixtures while leaving operating
// ownership and response decisions visibly unresolved for a human.
func PlaceholderRunbooks() []Runbook {
	return []Runbook{{
		ID: "PLACEHOLDER_PROMOTION_READINESS_RUNBOOK", Version: "PLACEHOLDER_v1", Service: "PLACEHOLDER_PROMOTION_SERVICE", Critical: true,
		Owner: "PLACEHOLDER_PRIMARY_ONCALL", BackupOwner: "PLACEHOLDER_BACKUP_ONCALL", Trigger: "PLACEHOLDER_SYNTHETIC_PAGE_TRIGGER",
		Diagnosis: "PLACEHOLDER_DIAGNOSIS_STEPS", Containment: "PLACEHOLDER_READ_ONLY_CONTAINMENT", RollbackOrDegrade: "PLACEHOLDER_ROLLBACK_OR_DEGRADE",
		Verification: "PLACEHOLDER_VERIFICATION_QUERY", Communication: "PLACEHOLDER_CUSTOMER_SAFE_COMMUNICATION", ExpiresAt: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
		AcknowledgementBy: 5 * time.Minute, EscalationBy: 15 * time.Minute, HandoffBy: 30 * time.Minute,
	}}
}

// PlaceholderRehearsals returns non-customer synthetic timing evidence.
func PlaceholderRehearsals() []RehearsalCase {
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	return []RehearsalCase{{ID: "PLACEHOLDER_SYNTHETIC_CASE_001", RunbookID: "PLACEHOLDER_PROMOTION_READINESS_RUNBOOK", TriggeredAt: base, AcknowledgedAt: base.Add(2 * time.Minute), EscalatedAt: base.Add(8 * time.Minute), HandedOffAt: base.Add(20 * time.Minute), EvidenceRef: "PLACEHOLDER_REHEARSAL_EVIDENCE"}}
}
