package abuse

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrDetectorVersionIdentity   = errors.New("abuse: detector version detector id and semver are required")
	ErrDetectorVersionSemver     = errors.New("abuse: detector version semver must be MAJOR.MINOR.PATCH")
	ErrDetectorVersionInputs     = errors.New("abuse: detector version must declare at least one input signal kind")
	ErrDetectorVersionInputKind  = errors.New("abuse: detector version declares an input signal kind outside the governed vocabulary")
	ErrDetectorVersionOutputs    = errors.New("abuse: detector version must declare at least one output")
	ErrDetectorVersionThreshold  = errors.New("abuse: detector version thresholds must be referenced by id")
	ErrDetectorVersionActivation = errors.New("abuse: detector version activation instant is required")
)

var semverPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ThresholdRef is a reference to a governed threshold declared elsewhere
// (never an inline value): a DetectorVersion names which thresholds it
// depends on by id, so operators can audit and version boundaries
// independently of the detector's own shape.
type ThresholdRef struct{ ID string }

// Valid reports whether t names a concrete threshold by reference.
func (t ThresholdRef) Valid() bool { return strings.TrimSpace(t.ID) != "" }

// DetectorVersion is one immutable, published version of a detector. It
// declares which governed signal kinds it consumes (DeclaredInputs), the
// closed set of output labels it can produce (DeclaredOutputs), the
// thresholds it depends on by reference (Thresholds), and the instant it
// became active (ActivatedAt). A DetectorVersion never carries scoring
// logic, weights, or free-text: it is the governed shape of a detector's
// contract, not its implementation.
type DetectorVersion struct {
	DetectorID      string
	Semver          string
	DeclaredInputs  []SignalKind
	DeclaredOutputs []string
	Thresholds      []ThresholdRef
	ActivatedAt     time.Time
}

// Validate enforces the DetectorVersion contract.
func (v DetectorVersion) Validate() error {
	if strings.TrimSpace(v.DetectorID) == "" || strings.TrimSpace(v.Semver) == "" {
		return ErrDetectorVersionIdentity
	}
	if !semverPattern.MatchString(v.Semver) {
		return ErrDetectorVersionSemver
	}
	if len(v.DeclaredInputs) == 0 {
		return ErrDetectorVersionInputs
	}
	for _, k := range v.DeclaredInputs {
		if !k.Valid() {
			return ErrDetectorVersionInputKind
		}
	}
	if len(v.DeclaredOutputs) == 0 {
		return ErrDetectorVersionOutputs
	}
	for _, o := range v.DeclaredOutputs {
		if strings.TrimSpace(o) == "" {
			return ErrDetectorVersionOutputs
		}
	}
	for _, th := range v.Thresholds {
		if !th.Valid() {
			return ErrDetectorVersionThreshold
		}
	}
	if v.ActivatedAt.IsZero() {
		return ErrDetectorVersionActivation
	}
	return nil
}

// ConsumesUndeclaredKind reports whether kind is not among v's declared
// inputs -- the ABUSE-001 security refusal for "a detector consuming an
// undeclared signal kind." It is independent of validity: even a
// syntactically valid DetectorVersion refuses to be applied to a signal
// kind it never declared.
func (v DetectorVersion) ConsumesUndeclaredKind(kind SignalKind) bool {
	for _, in := range v.DeclaredInputs {
		if in == kind {
			return false
		}
	}
	return true
}

// Digest returns a canonical, deterministic digest over v's declared
// contract: detector id, semver, and the sorted, deduplicated-by-content
// declared inputs/outputs/thresholds plus the activation instant
// (normalized to UTC RFC3339Nano). Two DetectorVersions built with the
// same logical content in different construction order digest identically.
func (v DetectorVersion) Digest() (string, error) {
	inputs := append([]SignalKind(nil), v.DeclaredInputs...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i] < inputs[j] })
	outputs := append([]string(nil), v.DeclaredOutputs...)
	sort.Strings(outputs)
	thresholds := append([]ThresholdRef(nil), v.Thresholds...)
	sort.Slice(thresholds, func(i, j int) bool { return thresholds[i].ID < thresholds[j].ID })

	canon := struct {
		DetectorID  string
		Semver      string
		Inputs      []SignalKind
		Outputs     []string
		Thresholds  []ThresholdRef
		ActivatedAt string
	}{
		DetectorID:  v.DetectorID,
		Semver:      v.Semver,
		Inputs:      inputs,
		Outputs:     outputs,
		Thresholds:  thresholds,
		ActivatedAt: v.ActivatedAt.UTC().Format(time.RFC3339Nano),
	}
	return digest(canon)
}
