package abuse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

func validDetectorVersion() abuse.DetectorVersion {
	return abuse.DetectorVersion{
		DetectorID:      "sensitive-read-detector",
		Semver:          "1.0.0",
		DeclaredInputs:  []abuse.SignalKind{abuse.SignalKindSensitiveRead},
		DeclaredOutputs: []string{"REVIEW_REQUIRED"},
		Thresholds:      []abuse.ThresholdRef{{ID: "threshold-1"}},
		ActivatedAt:     time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
}

func TestDetectorVersionValidate(t *testing.T) {
	if err := validDetectorVersion().Validate(); err != nil {
		t.Fatalf("valid detector version rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*abuse.DetectorVersion)
		want   error
	}{
		{"detector id", func(v *abuse.DetectorVersion) { v.DetectorID = "" }, abuse.ErrDetectorVersionIdentity},
		{"semver empty", func(v *abuse.DetectorVersion) { v.Semver = "" }, abuse.ErrDetectorVersionIdentity},
		{"semver malformed", func(v *abuse.DetectorVersion) { v.Semver = "v1" }, abuse.ErrDetectorVersionSemver},
		{"no inputs", func(v *abuse.DetectorVersion) { v.DeclaredInputs = nil }, abuse.ErrDetectorVersionInputs},
		{"ungoverned input", func(v *abuse.DetectorVersion) { v.DeclaredInputs = []abuse.SignalKind{"NOT_GOVERNED"} }, abuse.ErrDetectorVersionInputKind},
		{"no outputs", func(v *abuse.DetectorVersion) { v.DeclaredOutputs = nil }, abuse.ErrDetectorVersionOutputs},
		{"blank output", func(v *abuse.DetectorVersion) { v.DeclaredOutputs = []string{" "} }, abuse.ErrDetectorVersionOutputs},
		{"unreferenced threshold", func(v *abuse.DetectorVersion) { v.Thresholds = []abuse.ThresholdRef{{}} }, abuse.ErrDetectorVersionThreshold},
		{"activation instant", func(v *abuse.DetectorVersion) { v.ActivatedAt = time.Time{} }, abuse.ErrDetectorVersionActivation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validDetectorVersion()
			tc.mutate(&v)
			if err := v.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestDetectorVersionThresholdsAreReferencesNotValues(t *testing.T) {
	// A ThresholdRef only ever carries an id: there is no field on it for
	// an inline numeric value, so a detector version cannot smuggle a
	// threshold value into its own declared shape.
	ref := abuse.ThresholdRef{ID: "threshold-1"}
	if !ref.Valid() {
		t.Fatal("a populated threshold reference reports itself invalid")
	}
	if (abuse.ThresholdRef{}).Valid() {
		t.Fatal("an empty threshold reference reports itself valid")
	}
}
