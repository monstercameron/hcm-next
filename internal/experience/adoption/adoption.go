// Package adoption owns the versioned, evidence-backed readiness gate for the
// CUSTOMER-004 participant journeys. It has no transport or persistence
// dependencies: callers provide observations and retain the resulting digest.
package adoption

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

type Role string

const (
	RoleAdministrator Role = "administrator"
	RoleApprover      Role = "approver"
	RoleEmployee      Role = "employee"
	RoleSupport       Role = "support"
)

type Journey string

const (
	JourneyAdministratorSetup Journey = "administrator_setup"
	JourneyApproverDecision   Journey = "approver_decision"
	JourneyEmployeeRequest    Journey = "employee_request"
	JourneySupportTriage      Journey = "support_triage"
)

// Flow is retained as an alias for callers that use accessibility terminology.
type Flow = Journey

const (
	FlowAdministratorSetup = JourneyAdministratorSetup
	FlowApproverDecision   = JourneyApproverDecision
	FlowEmployeeRequest    = JourneyEmployeeRequest
	FlowSupportTriage      = JourneySupportTriage
)

type Dimension string

const (
	DimensionAccessibility Dimension = "accessibility"
	DimensionTraining      Dimension = "training"
	DimensionRollback      Dimension = "rollback"
	DimensionHelp          Dimension = "help"
)

type JourneySpec struct {
	ID       Journey
	Role     Role
	Name     string
	Required []Dimension
}

var journeyRegistry = [...]JourneySpec{
	{JourneyAdministratorSetup, RoleAdministrator, "Administrator setup", []Dimension{DimensionAccessibility, DimensionTraining, DimensionRollback, DimensionHelp}},
	{JourneyApproverDecision, RoleApprover, "Approver decision", []Dimension{DimensionAccessibility, DimensionTraining, DimensionRollback, DimensionHelp}},
	{JourneyEmployeeRequest, RoleEmployee, "Employee request", []Dimension{DimensionAccessibility, DimensionTraining, DimensionRollback, DimensionHelp}},
	{JourneySupportTriage, RoleSupport, "Support triage", []Dimension{DimensionAccessibility, DimensionTraining, DimensionRollback, DimensionHelp}},
}

func (j JourneySpec) Clone() JourneySpec {
	j.Required = append([]Dimension(nil), j.Required...)
	return j
}
func RegistryMatrix() []JourneySpec {
	out := make([]JourneySpec, len(journeyRegistry))
	for i := range journeyRegistry {
		out[i] = journeyRegistry[i].Clone()
	}
	return out
}
func Lookup(j Journey) (JourneySpec, bool) {
	for _, spec := range journeyRegistry {
		if spec.ID == j {
			return spec.Clone(), true
		}
	}
	return JourneySpec{}, false
}

var (
	ErrInvalidMatrix     = errors.New("adoption: invalid readiness matrix")
	ErrDuplicateEvidence = errors.New("adoption: duplicate journey/dimension evidence")
	ErrMissingEvidence   = errors.New("adoption: missing readiness evidence")
)

// Evidence binds an observation to one critical journey and readiness
// dimension. TaskDigest and ResultDigest must agree: a result from a different
// build cannot qualify the task that was exercised.
type Evidence struct {
	Journey         Journey   `json:"journey"`
	Dimension       Dimension `json:"dimension"`
	TaskDigest      string    `json:"task_digest"`
	ResultDigest    string    `json:"result_digest"`
	RecordedAt      time.Time `json:"recorded_at"`
	Accessible      bool      `json:"accessible"`
	Trained         bool      `json:"trained"`
	RollbackPath    bool      `json:"rollback_path"`
	HelpPath        bool      `json:"help_path"`
	Waiver          string    `json:"waiver,omitempty"`
	WaiverExpiresAt time.Time `json:"waiver_expires_at,omitempty"`
}

type Matrix struct {
	ID       string     `json:"id"`
	Version  string     `json:"version"`
	Evidence []Evidence `json:"evidence"`
}
type Report struct {
	MatrixDigest string
	Passed       bool
	Missing      []string
}

func (m Matrix) Validate(now time.Time) error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidMatrix)
	}
	seen := map[string]bool{}
	for _, e := range m.Evidence {
		if _, ok := Lookup(e.Journey); !ok || !validDimension(e.Dimension) || e.TaskDigest == "" || e.ResultDigest == "" || e.RecordedAt.IsZero() {
			return ErrMissingEvidence
		}
		key := string(e.Journey) + "\x00" + string(e.Dimension)
		if seen[key] {
			return ErrDuplicateEvidence
		}
		seen[key] = true
		if e.Waiver != "" && e.WaiverExpiresAt.IsZero() {
			return fmt.Errorf("%w: waiver expiry is required", ErrInvalidMatrix)
		}
		if !e.WaiverExpiresAt.IsZero() && !e.WaiverExpiresAt.After(now) {
			return fmt.Errorf("%w: expired waiver", ErrInvalidMatrix)
		}
	}
	return nil
}
func validDimension(d Dimension) bool {
	return d == DimensionAccessibility || d == DimensionTraining || d == DimensionRollback || d == DimensionHelp
}
func (m Matrix) Digest() (string, error) { return m.digestAt(time.Now().UTC()) }
func (m Matrix) digestAt(now time.Time) (string, error) {
	if err := m.Validate(now); err != nil {
		return "", err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:]), nil
}

func (m Matrix) Evaluate(now time.Time) (Report, error) {
	if err := m.Validate(now); err != nil {
		return Report{}, err
	}
	digest, err := m.digestAt(now)
	if err != nil {
		return Report{}, err
	}
	have := map[string]Evidence{}
	for _, e := range m.Evidence {
		have[string(e.Journey)+"\x00"+string(e.Dimension)] = e
	}
	var missing []string
	for _, spec := range journeyRegistry {
		for _, d := range spec.Required {
			e, ok := have[string(spec.ID)+"\x00"+string(d)]
			valid := ok && e.TaskDigest == e.ResultDigest && e.Accessible && e.Trained && e.RollbackPath && e.HelpPath
			if !valid {
				missing = append(missing, string(spec.ID)+"/"+string(d))
			}
		}
	}
	sort.Strings(missing)
	return Report{MatrixDigest: digest, Passed: len(missing) == 0, Missing: missing}, nil
}

// Check is the concise checker form for callers that already have a matrix.
func Check(m Matrix, now time.Time) (Report, error) { return m.Evaluate(now) }
