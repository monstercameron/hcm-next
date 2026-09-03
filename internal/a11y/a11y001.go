// Package a11y owns the versioned accessibility compatibility matrix.
// It deliberately records qualification evidence rather than pretending that
// an automated markup check proves assistive-technology equivalence.
package a11y

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

var (
	ErrInvalidMatrix        = errors.New("a11y: invalid support matrix")
	ErrDuplicateCombination = errors.New("a11y: duplicate environment combination")
	ErrMissingEvidence      = errors.New("a11y: missing equivalence evidence")
)

// Environment is a supported AT/browser/OS/input combination.
type Environment struct {
	AssistiveTechnology string `json:"assistive_technology"`
	Browser             string `json:"browser"`
	OS                  string `json:"os"`
	Locale              string `json:"locale"`
	Direction           string `json:"direction"`
	ZoomPercent         int    `json:"zoom_percent"`
	HighContrast        bool   `json:"high_contrast"`
	InputMode           string `json:"input_mode"`
}

func (e Environment) Key() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// Flow names the critical user outcomes that must remain equivalent.
type Flow string

const (
	FlowEvidenceUpload Flow = "evidence_upload"
	FlowApproval       Flow = "approval"
	FlowSemanticResult Flow = "semantic_result"
	FlowErrorRecovery  Flow = "error_recovery"
	FlowTimeoutWarning Flow = "timeout_warning"
)

// Evidence binds the observed task and result to the exact flow and build.
type Evidence struct {
	EnvironmentKey  string    `json:"environment_key"`
	Flow            Flow      `json:"flow"`
	TaskDigest      string    `json:"task_digest"`
	ResultDigest    string    `json:"result_digest"`
	FocusPreserved  bool      `json:"focus_preserved"`
	StateAnnounced  bool      `json:"state_announced"`
	ErrorAssociated bool      `json:"error_associated"`
	RecordedAt      time.Time `json:"recorded_at"`
	Defect          string    `json:"defect,omitempty"`
	Waiver          string    `json:"waiver,omitempty"`
	WaiverExpiresAt time.Time `json:"waiver_expires_at,omitempty"`
}

// Matrix is immutable by convention after validation. Callers should retain
// the returned digest as the qualification artifact identifier.
type Matrix struct {
	ID              string        `json:"id"`
	Version         string        `json:"version"`
	Environments    []Environment `json:"environments"`
	Flows           []Flow        `json:"flows"`
	Evidence        []Evidence    `json:"evidence"`
	ContinuityRoute string        `json:"continuity_route"`
}

type Report struct {
	MatrixDigest     string
	Passed           bool
	Missing          []string
	UnsupportedRoute bool
}

func (m Matrix) Validate(now time.Time) error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Version) == "" || len(m.Environments) == 0 || len(m.Flows) == 0 {
		return fmt.Errorf("%w: id, version, environments and flows are required", ErrInvalidMatrix)
	}
	seen := map[string]bool{}
	for _, e := range m.Environments {
		if e.AssistiveTechnology == "" || e.Browser == "" || e.OS == "" || e.Locale == "" || e.InputMode == "" {
			return fmt.Errorf("%w: environment fields are required", ErrInvalidMatrix)
		}
		if e.Direction != "ltr" && e.Direction != "rtl" {
			return fmt.Errorf("%w: direction must be ltr or rtl", ErrInvalidMatrix)
		}
		if e.ZoomPercent < 100 || e.ZoomPercent > 400 {
			return fmt.Errorf("%w: zoom must be 100..400", ErrInvalidMatrix)
		}
		if seen[e.Key()] {
			return ErrDuplicateCombination
		}
		seen[e.Key()] = true
	}
	flowSet := map[Flow]bool{}
	for _, f := range m.Flows {
		if f == "" || flowSet[f] {
			return fmt.Errorf("%w: duplicate or empty flow", ErrInvalidMatrix)
		}
		flowSet[f] = true
	}
	if strings.TrimSpace(m.ContinuityRoute) == "" {
		return fmt.Errorf("%w: continuity route is required", ErrInvalidMatrix)
	}
	for _, e := range m.Evidence {
		if !seen[e.EnvironmentKey] || !flowSet[e.Flow] || e.TaskDigest == "" || e.ResultDigest == "" || e.RecordedAt.IsZero() {
			return ErrMissingEvidence
		}
		if e.Waiver != "" && e.WaiverExpiresAt.IsZero() {
			return fmt.Errorf("%w: waiver expiry is required", ErrInvalidMatrix)
		}
		if !e.WaiverExpiresAt.IsZero() && !e.WaiverExpiresAt.After(now) {
			return fmt.Errorf("%w: expired waiver", ErrInvalidMatrix)
		}
	}
	return nil
}

func (m Matrix) Digest() (string, error) {
	if err := m.Validate(time.Now().UTC()); err != nil {
		return "", err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:]), nil
}

// Evaluate requires every environment/flow pair to have equivalent evidence.
func (m Matrix) Evaluate(now time.Time) (Report, error) {
	if err := m.Validate(now); err != nil {
		return Report{}, err
	}
	d, _ := m.Digest()
	have := map[string]Evidence{}
	for _, e := range m.Evidence {
		have[e.EnvironmentKey+"\x00"+string(e.Flow)] = e
	}
	var missing []string
	for _, env := range m.Environments {
		for _, flow := range m.Flows {
			e, ok := have[env.Key()+"\x00"+string(flow)]
			if !ok || e.TaskDigest != e.ResultDigest || !e.FocusPreserved || !e.StateAnnounced || !e.ErrorAssociated {
				missing = append(missing, env.Key()+"/"+string(flow))
			}
		}
	}
	sort.Strings(missing)
	return Report{MatrixDigest: d, Passed: len(missing) == 0, Missing: missing, UnsupportedRoute: m.ContinuityRoute != ""}, nil
}
