// Package edges executes the version-pinned adversarial workflow-
// context edge matrix (CONF-024): twelve required cases run through
// public capabilities and return exact outcome dimensions with authority
// bindings and ledger/effect counts. No transport acceptance, stale
// decision or manual continuity path fabricates business completion:
// every verdict carries its evidence chronology and asserts its
// prohibited effects at zero.
package edges

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// MatrixVersion pins the evaluated matrix: verdicts cite it, and unknown
// versions never execute.
const MatrixVersion = "2026-09-10"

// Outcomes: the closed verdict vocabulary.
const (
	OutcomeBlock          = "BLOCK"
	OutcomeReplan         = "REPLAN"
	OutcomeReapprove      = "REAPPROVE"
	OutcomeRouteHuman     = "ROUTE_HUMAN"
	OutcomeDegrade        = "DEGRADE"
	OutcomeRepairRequired = "REPAIR_REQUIRED"
	OutcomeComplete       = "COMPLETE"
)

// Case IDs: the twelve required adversarial cases.
const (
	CaseLateEvent          = "late-out-of-order-event"
	CaseQueuedHandoff      = "queued-authority-handoff"
	CaseLegalChangeWaiting = "legal-change-while-waiting"
	CaseAssigneeChurn      = "assignee-authority-churn"
	CaseDelegationLoop     = "delegation-sod-loop"
	CaseBulkInvalidation   = "one-subject-bulk-invalidation"
	CaseSharedDevice       = "shared-offline-device"
	CaseMaskedApproval     = "materially-masked-approval"
	CaseAcceptedNotApplied = "provider-accepted-not-applied"
	CaseManualOutage       = "manual-outage-continuity"
	CaseAccessibility      = "multi-step-accessibility-localization"
	CaseStaleDecision      = "stale-decision-transport-acceptance"
)

var (
	// ErrUnknownMatrix reports execution under an unpinned version.
	ErrUnknownMatrix = errors.New("edges: unknown matrix version")
	// ErrUnknownCase reports a case outside the required twelve.
	ErrUnknownCase = errors.New("edges: unknown edge case")
	// ErrProhibitedEffect reports a recorded effect the verdict forbids.
	ErrProhibitedEffect = errors.New("edges: prohibited effect recorded")
)

// Capability is the public seam every case executes through: fakes stand
// in for transports while the verdict counts real invocations.
type Capability interface {
	Name() string
	Invoke(input string) (Effect, error)
}

// Effect is one recorded capability outcome.
type Effect struct {
	Capability string
	Input      string
	Output     string
	Business   bool
}

// Recorder tallies capability invocations for ledger/effect counts.
type Recorder struct {
	mu      sync.Mutex
	effects []Effect
}

// Record appends one effect.
func (r *Recorder) Record(effect Effect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effects = append(r.effects, effect)
}

// Effects returns the recorded effects in order.
func (r *Recorder) Effects() []Effect {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Effect(nil), r.effects...)
}

// EdgeCase is one adversarial context. Only the fields its case reads
// matter; the rest stay zero.
type EdgeCase struct {
	ID               string
	MatrixVersion    string
	EventLate        bool
	EventOutOfOrder  bool
	HandoffClaimed   bool
	LegalChanged     bool
	AssigneeRevoked  bool
	DelegationCycles bool
	BulkInvalid      bool
	DeviceShared     bool
	DeviceOffline    bool
	DeviceAttested   bool
	ApprovalMasked   bool
	ProviderAccepted bool
	ProviderApplied  bool
	ManualRunbook    bool
	NeedsAltFormat   bool
	MandatoryLocale  bool
	LocaleReady      bool
	DecisionStale    bool
	TransportOnly    bool
	Capabilities     []Capability
	CapabilityInput  string
}

// Verdict is one deterministic outcome with its bindings.
type Verdict struct {
	CaseID           string
	MatrixVersion    string
	Outcome          string
	Evidence         []string
	AuthorityBinding string
	LedgerCount      int
	EffectCount      int
	BusinessEffects  int
	Digest           string
}

func verdictDigest(caseID, outcome string, evidence []string, ledger, effects int) string {
	parts := append([]string{"edge-verdict", MatrixVersion, caseID, outcome}, evidence...)
	parts = append(parts, fmt.Sprintf("ledger=%d", ledger), fmt.Sprintf("effects=%d", effects))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

var prohibitedByOutcome = map[string][]string{
	OutcomeBlock:          {"business-commit", "silent-advance"},
	OutcomeReplan:         {"business-commit"},
	OutcomeReapprove:      {"execute-under-old-approval"},
	OutcomeRouteHuman:     {"auto-advance"},
	OutcomeDegrade:        {"full-fidelity-effect"},
	OutcomeRepairRequired: {"business-commit", "mark-complete"},
	OutcomeComplete:       {},
}

func knownCase(id string) bool {
	switch id {
	case CaseLateEvent, CaseQueuedHandoff, CaseLegalChangeWaiting, CaseAssigneeChurn,
		CaseDelegationLoop, CaseBulkInvalidation, CaseSharedDevice, CaseMaskedApproval,
		CaseAcceptedNotApplied, CaseManualOutage, CaseAccessibility, CaseStaleDecision:
		return true
	default:
		return false
	}
}

// Execute runs one case through its capabilities and returns the exact
// outcome with evidence chronology. Capability invocations record the
// ledger; any recorded prohibited effect fails the verdict instead of
// completing it.
func Execute(c EdgeCase, recorder *Recorder) (Verdict, error) {
	if c.MatrixVersion != MatrixVersion {
		return Verdict{}, fmt.Errorf("%w: %q", ErrUnknownMatrix, c.MatrixVersion)
	}
	if !knownCase(c.ID) {
		return Verdict{}, fmt.Errorf("%w: %q", ErrUnknownCase, c.ID)
	}
	verdict := Verdict{CaseID: c.ID, MatrixVersion: MatrixVersion}
	chronicle := func(format string, args ...any) {
		verdict.Evidence = append(verdict.Evidence, fmt.Sprintf("t%d: "+format, append([]any{len(verdict.Evidence)}, args...)...))
	}
	invoke := func() {
		for _, capability := range c.Capabilities {
			effect, err := capability.Invoke(c.CapabilityInput)
			if err != nil {
				chronicle("capability %s refused: %v", capability.Name(), err)
				continue
			}
			effect.Capability = capability.Name()
			recorder.Record(effect)
			chronicle("capability %s returned %q", capability.Name(), effect.Output)
		}
	}
	verdict.AuthorityBinding = "authority/" + c.ID
	switch c.ID {
	case CaseStaleDecision:
		// Stale decisions and transport-only acceptance never complete.
		invoke()
		verdict.Outcome = OutcomeBlock
		chronicle("stale decision or transport-only path blocked")
	case CaseMaskedApproval:
		invoke()
		if !c.ApprovalMasked {
			verdict.Outcome = OutcomeComplete
			chronicle("approval transparent: complete")
			break
		}
		verdict.Outcome = OutcomeBlock
		chronicle("materially masked approval blocked")
	case CaseDelegationLoop:
		invoke()
		if !c.DelegationCycles {
			verdict.Outcome = OutcomeComplete
			chronicle("delegation chain acyclic: complete")
			break
		}
		verdict.Outcome = OutcomeBlock
		chronicle("delegation/SoD loop blocked")
	case CaseLegalChangeWaiting:
		invoke()
		if !c.LegalChanged {
			verdict.Outcome = OutcomeComplete
			chronicle("law unchanged: complete")
			break
		}
		verdict.Outcome = OutcomeReapprove
		chronicle("legal change while waiting: re-approve")
	case CaseAcceptedNotApplied:
		invoke()
		if c.ProviderAccepted && !c.ProviderApplied {
			verdict.Outcome = OutcomeRepairRequired
			chronicle("provider accepted without applying: repair required")
			break
		}
		verdict.Outcome = OutcomeComplete
		chronicle("provider applied: complete")
	case CaseLateEvent:
		invoke()
		if c.EventLate || c.EventOutOfOrder {
			verdict.Outcome = OutcomeReplan
			chronicle("late/out-of-order event: replan frontier")
			break
		}
		verdict.Outcome = OutcomeComplete
		chronicle("ordered event: complete")
	case CaseBulkInvalidation:
		invoke()
		if c.BulkInvalid {
			verdict.Outcome = OutcomeReplan
			chronicle("one-subject bulk invalidation: replan")
			break
		}
		verdict.Outcome = OutcomeComplete
		chronicle("bulk intact: complete")
	case CaseQueuedHandoff:
		invoke()
		if !c.HandoffClaimed {
			verdict.Outcome = OutcomeRouteHuman
			chronicle("queued handoff unclaimed: route human")
			break
		}
		verdict.Outcome = OutcomeComplete
		chronicle("handoff claimed: complete")
	case CaseAssigneeChurn:
		invoke()
		if c.AssigneeRevoked {
			verdict.Outcome = OutcomeRouteHuman
			chronicle("assignee authority churned: route human")
			break
		}
		verdict.Outcome = OutcomeComplete
		chronicle("assignee stable: complete")
	case CaseManualOutage:
		invoke()
		if !c.ManualRunbook {
			verdict.Outcome = OutcomeRouteHuman
			chronicle("outage without runbook: route human")
			break
		}
		verdict.Outcome = OutcomeDegrade
		chronicle("runbook continuity: degrade with evidence")
	case CaseSharedDevice:
		invoke()
		switch {
		case c.DeviceShared && !c.DeviceAttested:
			verdict.Outcome = OutcomeBlock
			chronicle("unattested shared device blocked")
		case c.DeviceOffline:
			verdict.Outcome = OutcomeDegrade
			chronicle("offline device: degrade with queued effects")
		default:
			verdict.Outcome = OutcomeComplete
			chronicle("device trusted: complete")
		}
	case CaseAccessibility:
		invoke()
		switch {
		case c.MandatoryLocale && !c.LocaleReady:
			verdict.Outcome = OutcomeBlock
			chronicle("mandatory locale missing blocked")
		case c.NeedsAltFormat:
			verdict.Outcome = OutcomeDegrade
			chronicle("alternate-format path with evidence")
		default:
			verdict.Outcome = OutcomeComplete
			chronicle("no accessibility barrier: complete")
		}
	}
	verdict.LedgerCount = len(verdict.Evidence)
	for _, effect := range recorder.Effects() {
		verdict.EffectCount++
		if effect.Business {
			verdict.BusinessEffects++
		}
		for _, prohibited := range prohibitedByOutcome[verdict.Outcome] {
			if effect.Output == prohibited {
				return Verdict{}, fmt.Errorf("%w: %q under %s", ErrProhibitedEffect, effect.Output, verdict.Outcome)
			}
		}
	}
	verdict.Digest = verdictDigest(verdict.CaseID, verdict.Outcome, verdict.Evidence, verdict.LedgerCount, verdict.EffectCount)
	return verdict, nil
}

// CaseIDs lists the required twelve in order.
func CaseIDs() []string {
	return []string{
		CaseLateEvent, CaseQueuedHandoff, CaseLegalChangeWaiting, CaseAssigneeChurn,
		CaseDelegationLoop, CaseBulkInvalidation, CaseSharedDevice, CaseMaskedApproval,
		CaseAcceptedNotApplied, CaseManualOutage, CaseAccessibility, CaseStaleDecision,
	}
}

// Verify recomputes the verdict seal.
func (v Verdict) Verify() error {
	if v.MatrixVersion != MatrixVersion {
		return fmt.Errorf("%w: %q", ErrUnknownMatrix, v.MatrixVersion)
	}
	if verdictDigest(v.CaseID, v.Outcome, v.Evidence, v.LedgerCount, v.EffectCount) != v.Digest {
		return errors.New("edges: verdict seal broken")
	}
	return nil
}
