// Package confidentialactor defines the explicit disclosure contract for
// confidential actors. It is deliberately policy-only: accepting an intake
// here does not grant identity, authorization, or permission to reveal it.
package confidentialactor

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Mode identifies how an actor's identity is handled.
type Mode string

const (
	ModeKnown        Mode = "KNOWN"
	ModePseudonymous Mode = "PSEUDONYMOUS"
	ModeAnonymous    Mode = "ANONYMOUS"
	ModeEscrowed     Mode = "ESCROWED"
)

// IdentityEvidence describes evidence permitted to establish or recover an
// identity. Values are intentionally opaque to this package and interpreted
// by the owning identity system.
type IdentityEvidence struct {
	Kind string
	Ref  string
}

// AbuseControls must be present on every intake, including anonymous ones.
type AbuseControls struct {
	RateLimitPerHour int
	ReportChannel    string
	BlockOnRisk      bool
}

// RevelationPolicy says whether and under what event identity may be
// revealed. Automatic revelation is never accepted for anonymous actors.
type RevelationPolicy struct {
	Allowed          bool
	Trigger          string
	ApproverRole     string
	RequiresEvidence bool
}

// DisclosureLimit bounds downstream use and disclosure. At least one
// recipient and one field must be named; an empty list is not an allow-all.
type DisclosureLimit struct {
	Recipients []string
	Fields     []string
	Expiry     string
	Notify     bool
}

// Intake is the complete, explicit disclosure declaration for one intake.
type Intake struct {
	Mode             Mode
	IdentityEvidence []IdentityEvidence
	Abuse            AbuseControls
	Revelation       RevelationPolicy
	Downstream       DisclosureLimit
}

var (
	ErrModeRequired       = errors.New("confidentialactor: disclosure mode is required")
	ErrModeInvalid        = errors.New("confidentialactor: disclosure mode is invalid")
	ErrEvidenceRequired   = errors.New("confidentialactor: identity evidence is required")
	ErrEvidenceForbidden  = errors.New("confidentialactor: identity evidence is forbidden")
	ErrAbuseRequired      = errors.New("confidentialactor: abuse controls are required")
	ErrRevelationRequired = errors.New("confidentialactor: revelation policy is required")
	ErrDownstreamRequired = errors.New("confidentialactor: downstream disclosure limits are required")
)

func (m Mode) Valid() bool {
	return m == ModeKnown || m == ModePseudonymous || m == ModeAnonymous || m == ModeEscrowed
}

// Validate rejects incomplete declarations. It returns all independent
// violations in deterministic order so callers can safely surface them.
func (i Intake) Validate() error {
	var errs []error
	if i.Mode == "" {
		errs = append(errs, ErrModeRequired)
	} else if !i.Mode.Valid() {
		errs = append(errs, ErrModeInvalid)
	}
	if i.Mode == ModeKnown && len(i.IdentityEvidence) == 0 {
		errs = append(errs, ErrEvidenceRequired)
	}
	if i.Mode == ModeEscrowed && len(i.IdentityEvidence) == 0 {
		errs = append(errs, ErrEvidenceRequired)
	}
	if i.Mode == ModeAnonymous && len(i.IdentityEvidence) != 0 {
		errs = append(errs, ErrEvidenceForbidden)
	}
	for _, e := range i.IdentityEvidence {
		if strings.TrimSpace(e.Kind) == "" || strings.TrimSpace(e.Ref) == "" {
			errs = append(errs, errors.New("confidentialactor: identity evidence kind and ref are required"))
			break
		}
	}
	if i.Abuse.RateLimitPerHour <= 0 || strings.TrimSpace(i.Abuse.ReportChannel) == "" {
		errs = append(errs, ErrAbuseRequired)
	}
	if strings.TrimSpace(i.Revelation.Trigger) == "" || strings.TrimSpace(i.Revelation.ApproverRole) == "" {
		errs = append(errs, ErrRevelationRequired)
	}
	if i.Mode == ModeAnonymous && i.Revelation.Allowed {
		errs = append(errs, errors.New("confidentialactor: anonymous identity cannot be revealed"))
	}
	if len(i.Downstream.Recipients) == 0 || len(i.Downstream.Fields) == 0 || strings.TrimSpace(i.Downstream.Expiry) == "" {
		errs = append(errs, ErrDownstreamRequired)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Validate returns a stable, human-readable error and never applies defaults.
func (i Intake) String() string { return fmt.Sprintf("%s confidential actor intake", i.Mode) }

// CanonicalRecipients returns a sorted copy suitable for deterministic policy
// serialization by callers.
func (i Intake) CanonicalRecipients() []string {
	out := append([]string(nil), i.Downstream.Recipients...)
	sort.Strings(out)
	return out
}
