package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Obligation statuses: the closed lifecycle vocabulary.
const (
	ObligationOpen      = "OPEN"
	ObligationSatisfied = "SATISFIED"
	ObligationWaived    = "WAIVED"
	ObligationOverdue   = "OVERDUE"
	ObligationBlocked   = "BLOCKED"
)

// DueRule is the versioned statutory-calendar calculation behind one
// due date: an offset in calendar days from the trigger under a named
// calendar version. Day counts keep the computation kernel-pure.
type DueRule struct {
	OffsetDays int
	Calendar   string
	Version    string
}

// Obligation is one structured legal obligation with its computed due
// date and lifecycle status. Waivers never delete the original: waiving
// returns a new record citing its explicit authority.
type Obligation struct {
	Type            ObligationType
	Authority       string
	Subject         string
	Trigger         string
	TriggerDay      int
	Due             DueRule
	DueDay          int
	Action          string
	Evidence        string
	Owner           string
	Risk            string
	Status          string
	BlockReason     string
	WaiverAuthority string
	Digest          string
}

func obligationDigest(obligation Obligation) string {
	parts := []string{
		"legal-obligation", obligation.Type.String(), obligation.Authority,
		obligation.Subject, obligation.Trigger, fmt.Sprint(obligation.TriggerDay),
		fmt.Sprint(obligation.Due.OffsetDays), obligation.Due.Calendar, obligation.Due.Version,
		fmt.Sprint(obligation.DueDay), obligation.Action, obligation.Evidence,
		obligation.Owner, obligation.Risk, obligation.Status,
		obligation.BlockReason, obligation.WaiverAuthority,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// OpenObligation computes the due date and opens the obligation. Missing
// authority, trigger, due-date calendar, action, evidence path, owner or
// risk refuses: an obligation that cannot name its bindings never opens.
func OpenObligation(obligation Obligation) (Obligation, error) {
	if !validObligationType(obligation.Type) {
		return Obligation{}, fmt.Errorf("legal: obligation type is outside the rule-pack vocabulary")
	}
	if strings.TrimSpace(obligation.Authority) == "" {
		return Obligation{}, fmt.Errorf("legal: obligation authority is required")
	}
	if strings.TrimSpace(obligation.Subject) == "" || strings.TrimSpace(obligation.Trigger) == "" {
		return Obligation{}, fmt.Errorf("legal: obligation subject and trigger are required")
	}
	if obligation.TriggerDay < 0 {
		return Obligation{}, fmt.Errorf("legal: obligation trigger day is required")
	}
	if strings.TrimSpace(obligation.Due.Calendar) == "" || strings.TrimSpace(obligation.Due.Version) == "" {
		return Obligation{}, fmt.Errorf("legal: obligation due-date calendar and version are required")
	}
	if obligation.Due.OffsetDays < 0 {
		return Obligation{}, fmt.Errorf("legal: obligation due offset cannot be negative")
	}
	if strings.TrimSpace(obligation.Action) == "" || strings.TrimSpace(obligation.Evidence) == "" {
		return Obligation{}, fmt.Errorf("legal: obligation action and evidence are required")
	}
	if strings.TrimSpace(obligation.Owner) == "" || strings.TrimSpace(obligation.Risk) == "" {
		return Obligation{}, fmt.Errorf("legal: obligation owner and risk are required")
	}
	obligation.DueDay = obligation.TriggerDay + obligation.Due.OffsetDays
	obligation.Status = ObligationOpen
	obligation.Digest = obligationDigest(obligation)
	return obligation, nil
}

func validObligationType(obligationType ObligationType) bool {
	for _, candidate := range AllObligationTypes() {
		if candidate == obligationType {
			return true
		}
	}
	return false
}

// Satisfy closes an obligation against evidence. A contradictory or empty
// evidence claim never reports satisfied.
func Satisfy(obligation Obligation, evidence string) (Obligation, error) {
	if obligation.Status != ObligationOpen && obligation.Status != ObligationOverdue {
		return Obligation{}, fmt.Errorf("legal: only an open obligation can be satisfied")
	}
	if strings.TrimSpace(evidence) == "" {
		return Obligation{}, fmt.Errorf("legal: satisfaction requires evidence")
	}
	obligation.Evidence = evidence
	obligation.Status = ObligationSatisfied
	obligation.Digest = obligationDigest(obligation)
	return obligation, nil
}

// Waive suspends an obligation under an explicit authority. The original
// record is never deleted: the waiver cites its authority on the returned
// record while the caller's original keeps its prior status.
func Waive(obligation Obligation, authority string) (Obligation, error) {
	if obligation.Status != ObligationOpen && obligation.Status != ObligationOverdue {
		return Obligation{}, fmt.Errorf("legal: only an open obligation can be waived")
	}
	if strings.TrimSpace(authority) == "" {
		return Obligation{}, fmt.Errorf("legal: waiver requires an explicit authority")
	}
	waived := obligation
	waived.Status = ObligationWaived
	waived.WaiverAuthority = authority
	waived.Digest = obligationDigest(waived)
	return waived, nil
}

// Block parks an obligation behind a named reason.
func Block(obligation Obligation, reason string) (Obligation, error) {
	if obligation.Status != ObligationOpen && obligation.Status != ObligationOverdue {
		return Obligation{}, fmt.Errorf("legal: only an open obligation can be blocked")
	}
	if strings.TrimSpace(reason) == "" {
		return Obligation{}, fmt.Errorf("legal: blocking requires a reason")
	}
	obligation.Status = ObligationBlocked
	obligation.BlockReason = reason
	obligation.Digest = obligationDigest(obligation)
	return obligation, nil
}

// Age advances lifecycle time: open obligations past their due date turn
// OVERDUE. All other statuses are stable under time.
func Age(obligation Obligation, today int) Obligation {
	if obligation.Status == ObligationOpen && today > obligation.DueDay {
		obligation.Status = ObligationOverdue
		obligation.Digest = obligationDigest(obligation)
	}
	return obligation
}

// Verify recomputes the obligation seal.
func (obligation Obligation) Verify() error {
	if obligation.Digest == "" || obligationDigest(obligation) != obligation.Digest {
		return fmt.Errorf("legal: obligation seal is broken")
	}
	return nil
}
