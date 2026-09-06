package adversarial

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDenyErrorAndIsNonDisclosingDeny_RejectCodesAndLeakTokens(t *testing.T) {
	denied := &DenyError{Code: CodeThreatDenied, Message: "denied"}
	if denied.Error() != "THREAT_002_DENIED: denied" || !IsNonDisclosingDeny(denied) {
		t.Fatalf("deny error = %q or was not non-disclosing", denied.Error())
	}
	for _, err := range []error{
		nil,
		&DenyError{Code: CodeThreatLeaked, Message: "denied"},
		&DenyError{Code: CodeThreatDenied, Message: "resource exists"},
		&DenyError{Code: CodeThreatDenied, Message: "resource found"},
		&DenyError{Code: CodeThreatDenied, Message: "worker: secret"},
		&DenyError{Code: CodeThreatDenied, Message: "tenant-a"},
	} {
		if IsNonDisclosingDeny(err) {
			t.Fatalf("leaking/non-deny error accepted: %v", err)
		}
	}
	if !errors.Is(ErrDenied, ErrDenied) {
		t.Fatal("ErrDenied is not stable as a sentinel")
	}
}

func TestEngine_RunBranchesEvidenceNilAndLeakDetection(t *testing.T) {
	j := Journey{ID: "j-1", Kind: KindPrivacy, Channel: ChannelHTTP, Tenant: "tenant-a", Target: "worker:secret", Action: "read"}
	engine := NewEngine(func(context.Context, Journey) error { return nil })
	result := engine.Run(context.Background(), j)
	if result.Denied || result.UnauthorizedEffects != 1 || !result.EvidenceRedacted || result.EvidenceID == "" {
		t.Fatalf("allowed result = %+v", result)
	}
	noEvidence := NewEngine(func(context.Context, Journey) error { return &DenyError{Code: CodeThreatDenied, Message: "denied"} })
	noEvidence.Evidence = nil
	result = noEvidence.Run(context.Background(), j)
	if !result.Denied || !result.NonDisclosing || result.EvidenceID != "" || result.UnauthorizedEffects != 0 {
		t.Fatalf("nil evidence result = %+v", result)
	}
	for _, message := range []string{"resource exists", "field: salary", "count: 2", "trace span"} {
		leaking := NewEngine(func(context.Context, Journey) error { return &DenyError{Code: CodeThreatDenied, Message: message} })
		got := leaking.Run(context.Background(), j)
		if !got.ExistenceLeak && !strings.Contains(message, "field:") && !strings.Contains(message, "trace") && !strings.Contains(message, "count:") {
			t.Fatalf("message %q was not classified as existence leak: %+v", message, got)
		}
		if strings.Contains(message, "field:") && !got.MetadataLeak {
			t.Fatalf("message %q was not classified as metadata leak: %+v", message, got)
		}
		if strings.Contains(message, "trace") && !got.TelemetryLeak {
			t.Fatalf("message %q was not classified as telemetry leak: %+v", message, got)
		}
	}
}

func TestVerifyNoLeakage_ReportsEachInvariantAndParityDetectsMismatch(t *testing.T) {
	valid := JourneyResult{JourneyID: "j", Denied: true, NonDisclosing: true, EvidenceID: "ev", EvidenceRedacted: true}
	cases := []struct {
		name   string
		mutate func(*JourneyResult)
	}{
		{"allowed", func(r *JourneyResult) { r.Denied = false }},
		{"disclosing", func(r *JourneyResult) { r.NonDisclosing = false }},
		{"existence leak", func(r *JourneyResult) { r.ExistenceLeak = true }},
		{"metadata leak", func(r *JourneyResult) { r.MetadataLeak = true }},
		{"telemetry leak", func(r *JourneyResult) { r.TelemetryLeak = true }},
		{"unauthorized effect", func(r *JourneyResult) { r.UnauthorizedEffects = 1 }},
		{"missing evidence", func(r *JourneyResult) { r.EvidenceID = "" }},
		{"unredacted evidence", func(r *JourneyResult) { r.EvidenceRedacted = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := valid
			tc.mutate(&result)
			if err := VerifyNoLeakage([]JourneyResult{result}); err == nil {
				t.Fatal("VerifyNoLeakage accepted invalid result")
			}
		})
	}
	if err := VerifyNoLeakage(nil); err != nil {
		t.Fatalf("empty result set rejected: %v", err)
	}
	if !ParityAcrossChannels([]JourneyResult{{JourneyID: "same", Denied: true}, {JourneyID: "same", Denied: true}}) {
		t.Fatal("matching channel decisions reported as non-parity")
	}
	if ParityAcrossChannels([]JourneyResult{{JourneyID: "same", Denied: true}, {JourneyID: "same", Denied: false}}) {
		t.Fatal("mismatched channel decisions reported as parity")
	}
}

func TestPresetJourneys_AreCompleteAndReturnedSliceIsIndependent(t *testing.T) {
	journeys := PresetJourneys()
	if len(journeys) != 12 {
		t.Fatalf("PresetJourneys length = %d", len(journeys))
	}
	first := journeys[0]
	journeys[0].ID = "tampered"
	if PresetJourneys()[0].ID != first.ID {
		t.Fatal("PresetJourneys exposed mutable backing state")
	}
	for _, journey := range PresetJourneys() {
		if journey.ID == "" || journey.Channel == "" || journey.Tenant == "" || journey.Target == "" || journey.Action == "" {
			t.Fatalf("incomplete preset journey: %+v", journey)
		}
	}
}
