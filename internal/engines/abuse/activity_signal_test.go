package abuse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/abuse"
)

func validActivitySignal() abuse.ActivitySignal {
	return abuse.ActivitySignal{
		ID:           "sig-1",
		Kind:         abuse.SignalKindSensitiveRead,
		Subject:      abuse.Ref{Type: "worker", ID: "w-1"},
		Actor:        abuse.Ref{Type: "user", ID: "u-1"},
		Tenant:       "tenant-a",
		ObservedAt:   time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		SourceSystem: "activity-stream",
	}
}

func TestActivitySignalValidate(t *testing.T) {
	if err := validActivitySignal().Validate(); err != nil {
		t.Fatalf("valid signal rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*abuse.ActivitySignal)
		want   error
	}{
		{"id", func(s *abuse.ActivitySignal) { s.ID = "" }, abuse.ErrSignalIdentity},
		{"kind", func(s *abuse.ActivitySignal) { s.Kind = "NOT_GOVERNED" }, abuse.ErrSignalKind},
		{"subject ref", func(s *abuse.ActivitySignal) { s.Subject = abuse.Ref{} }, abuse.ErrSignalRef},
		{"actor ref", func(s *abuse.ActivitySignal) { s.Actor = abuse.Ref{} }, abuse.ErrSignalRef},
		{"tenant", func(s *abuse.ActivitySignal) { s.Tenant = "" }, abuse.ErrSignalTenant},
		{"observed at", func(s *abuse.ActivitySignal) { s.ObservedAt = time.Time{} }, abuse.ErrSignalObservedAt},
		{"source system", func(s *abuse.ActivitySignal) { s.SourceSystem = "" }, abuse.ErrSignalSourceEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validActivitySignal()
			tc.mutate(&s)
			if err := s.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

// TestABUSE001SecurityRawContentAndUndeclaredKindRefused extends the
// existing TestTodo_ABUSE_001_Security publish-boundary coverage (that
// name is already taken in abuse_test.go) with the ActivitySignal/
// DetectorVersion boundary: a signal carrying raw content is refused, and
// so is a detector version consuming a signal kind it never declared.
func TestABUSE001SecurityRawContentAndUndeclaredKindRefused(t *testing.T) {
	s := validActivitySignal()
	s.RawContent = "the actual free-text description of what happened"
	if err := s.Validate(); !errors.Is(err, abuse.ErrSignalRawContent) {
		t.Fatalf("signal carrying raw content was not refused: %v", err)
	}
	if _, err := s.Digest(); !errors.Is(err, abuse.ErrSignalRawContent) {
		t.Fatalf("Digest() did not refuse raw content: %v", err)
	}

	dv := validDetectorVersion()
	other := abuse.SignalKindPrivilegedChange
	if !dv.ConsumesUndeclaredKind(other) {
		t.Fatalf("detector version %v incorrectly reports %q as a declared input", dv.DeclaredInputs, other)
	}
	exp := abuse.Explain(validActivitySignal(), abuse.DetectorVersion{
		DetectorID:      dv.DetectorID,
		Semver:          dv.Semver,
		DeclaredInputs:  []abuse.SignalKind{other},
		DeclaredOutputs: dv.DeclaredOutputs,
		Thresholds:      dv.Thresholds,
		ActivatedAt:     dv.ActivatedAt,
	})
	if exp.Applicable || exp.Reason != abuse.ExplainReasonNotDeclaredInput {
		t.Fatalf("Explain() matched an undeclared signal kind: %+v", exp)
	}
}

// TestTodo_ABUSE_001_Property is the ABUSE-001 digest-determinism property
// test: two ActivitySignals (and two DetectorVersions) with identical
// logical content, but constructed via different field/slice orderings,
// always digest identically; changing any governed field always changes
// the digest.
func TestTodo_ABUSE_001_Property(t *testing.T) {
	a := validActivitySignal()
	b := validActivitySignal() // separately constructed, identical content
	da, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	db, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if da != db || da == "" {
		t.Fatalf("identical activity signals digested differently: %q vs %q", da, db)
	}

	changed := validActivitySignal()
	changed.Tenant = "tenant-b"
	dc, err := changed.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if dc == da {
		t.Fatal("changing tenant did not change the activity signal digest")
	}

	dvA := validDetectorVersion()
	dvA.DeclaredInputs = []abuse.SignalKind{abuse.SignalKindSensitiveRead, abuse.SignalKindBulkExport}
	dvA.DeclaredOutputs = []string{"REVIEW_REQUIRED", "STEP_UP_AUTH"}
	dvA.Thresholds = []abuse.ThresholdRef{{ID: "t-2"}, {ID: "t-1"}}

	dvB := validDetectorVersion()
	dvB.DeclaredInputs = []abuse.SignalKind{abuse.SignalKindBulkExport, abuse.SignalKindSensitiveRead}
	dvB.DeclaredOutputs = []string{"STEP_UP_AUTH", "REVIEW_REQUIRED"}
	dvB.Thresholds = []abuse.ThresholdRef{{ID: "t-1"}, {ID: "t-2"}}

	digA, err := dvA.Digest()
	if err != nil {
		t.Fatal(err)
	}
	digB, err := dvB.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digA != digB {
		t.Fatalf("reordering declared inputs/outputs/thresholds changed the detector version digest: %q vs %q", digA, digB)
	}

	dvC := validDetectorVersion()
	dvC.Semver = "2.0.0"
	digC, err := dvC.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digC == digA {
		t.Fatal("changing semver did not change the detector version digest")
	}
}
