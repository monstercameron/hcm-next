package evidence_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// controls is the minimal pinned control set a receipt needs.
var controls = []evidence.ControlVersion{{Name: "rule_pack", Version: "1.0.0"}}

func TestZeroEffectReceiptRefusesAnyCountedEffect(t *testing.T) {
	counters := []struct {
		name string
		c    evidence.EffectCounters
	}{
		{"domain write", evidence.EffectCounters{DomainWrites: 1}},
		{"reservation", evidence.EffectCounters{Reservations: 1}},
		{"work item", evidence.EffectCounters{WorkItems: 1}},
		{"timer", evidence.EffectCounters{Timers: 1}},
		{"message", evidence.EffectCounters{Messages: 1}},
		{"outbox entry", evidence.EffectCounters{OutboxEntries: 1}},
		{"provider call", evidence.EffectCounters{ProviderCalls: 1}},
		{"approval binding", evidence.EffectCounters{ApprovalBindings: 1}},
	}
	for _, tc := range counters {
		t.Run(tc.name, func(t *testing.T) {
			_, err := evidence.NewZeroEffectReceipt("t", "v1", evidence.ModeSimulate,
				evidence.RequestStateSimulated, controls, "sha256:a", "sha256:b", tc.c)
			if !errors.Is(err, evidence.ErrEffectsNotZero) {
				t.Fatalf("error = %v, want ErrEffectsNotZero", err)
			}
			if tc.c.IsZero() {
				t.Fatal("a counter with one effect must not report itself as zero")
			}
			if len(tc.c.NonZero()) != 1 {
				t.Fatalf("NonZero() = %v, want exactly one entry", tc.c.NonZero())
			}
		})
	}

	t.Run("a negative count is rejected before the zero check", func(t *testing.T) {
		_, err := evidence.NewZeroEffectReceipt("t", "v1", evidence.ModeSimulate,
			evidence.RequestStateSimulated, controls, "sha256:a", "sha256:b",
			evidence.EffectCounters{DomainWrites: -1})
		if !errors.Is(err, evidence.ErrNegativeCount) {
			t.Fatalf("error = %v, want ErrNegativeCount", err)
		}
	})
}

func TestZeroEffectReceiptRequiresIdentityDigestsAndPinnedControls(t *testing.T) {
	cases := []struct {
		name          string
		intentType    string
		intentVersion string
		mode          evidence.Mode
		requestState  string
		controls      []evidence.ControlVersion
		inputsDigest  string
		resultDigest  string
	}{
		{"no intent type", "", "v1", evidence.ModeSimulate, evidence.RequestStateSimulated, controls, "sha256:a", "sha256:b"},
		{"no intent version", "t", "", evidence.ModeSimulate, evidence.RequestStateSimulated, controls, "sha256:a", "sha256:b"},
		{"not a P1A mode", "t", "v1", evidence.Mode("EXECUTE"), evidence.RequestStateSimulated, controls, "sha256:a", "sha256:b"},
		{"no request state", "t", "v1", evidence.ModeSimulate, "", controls, "sha256:a", "sha256:b"},
		{"no controls", "t", "v1", evidence.ModeSimulate, evidence.RequestStateSimulated, nil, "sha256:a", "sha256:b"},
		{"unversioned control", "t", "v1", evidence.ModeSimulate, evidence.RequestStateSimulated,
			[]evidence.ControlVersion{{Name: "rule_pack"}}, "sha256:a", "sha256:b"},
		{"no inputs digest", "t", "v1", evidence.ModeSimulate, evidence.RequestStateSimulated, controls, "", "sha256:b"},
		{"no result digest", "t", "v1", evidence.ModeSimulate, evidence.RequestStateSimulated, controls, "sha256:a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := evidence.NewZeroEffectReceipt(tc.intentType, tc.intentVersion, tc.mode,
				tc.requestState, tc.controls, tc.inputsDigest, tc.resultDigest, evidence.ZeroEffects())
			if err == nil {
				t.Fatal("an incomplete receipt was accepted")
			}
		})
	}
}

func TestZeroEffectReceiptSortsControlsSoTheEncodingIsOrderIndependent(t *testing.T) {
	forward := []evidence.ControlVersion{
		{Name: "annualization_rule", Version: "1.0.0"},
		{Name: "pay_band_catalog", Version: "2026.1"},
		{Name: "policy", Version: "3"},
	}
	reversed := []evidence.ControlVersion{forward[2], forward[1], forward[0]}

	a, err := evidence.NewZeroEffectReceipt("t", "v1", evidence.ModePreflight,
		evidence.RequestStatePreflighted, forward, "sha256:a", "sha256:b", evidence.ZeroEffects())
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	b, err := evidence.NewZeroEffectReceipt("t", "v1", evidence.ModePreflight,
		evidence.RequestStatePreflighted, reversed, "sha256:a", "sha256:b", evidence.ZeroEffects())
	if err != nil {
		t.Fatalf("reversed: %v", err)
	}
	if !bytes.Equal(a.Canonical(), b.Canonical()) {
		t.Fatal("the control order changed the canonical encoding")
	}
	if a.ExecutionState != evidence.ExecutionStateNotPlanned {
		t.Fatalf("execution state = %q; P1A never leaves NOT_PLANNED", a.ExecutionState)
	}
}

func TestSourceAuthorityAndProvenanceRefuseIncompleteEvidence(t *testing.T) {
	at, err := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("timestamp: %v", err)
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(at))
	if err != nil {
		t.Fatalf("recorded at: %v", err)
	}

	authorities := []evidence.SourceAuthority{
		{},
		{Kind: evidence.AuthorityLocal},
		{Kind: evidence.AuthorityLocal, System: "hcmnext.people"},
	}
	for i, a := range authorities {
		if err := a.Validate(); !errors.Is(err, evidence.ErrSourceAuthority) {
			t.Errorf("authority %d: error = %v, want ErrSourceAuthority", i, err)
		}
		if a.Canonical() != nil {
			t.Errorf("authority %d encoded despite being incomplete", i)
		}
	}
	complete := evidence.SourceAuthority{
		Kind: evidence.AuthorityExternalObservation, System: "workday", PolicyRef: "p/1",
	}
	if err := complete.Validate(); err != nil {
		t.Fatalf("complete authority: %v", err)
	}
	if complete.String() != "EXTERNAL_OBSERVATION:workday@p/1" {
		t.Errorf("authority string = %q", complete.String())
	}

	provenances := []evidence.Provenance{
		{},
		{Source: "workday"},
		{Source: "workday", EvidenceRef: "evd_1"},
	}
	for i, p := range provenances {
		if err := p.Validate(); !errors.Is(err, evidence.ErrProvenance) {
			t.Errorf("provenance %d: error = %v, want ErrProvenance", i, err)
		}
	}
	good := evidence.Provenance{Source: "workday", EvidenceRef: "evd_1", RecordedAt: recordedAt}
	if err := good.Validate(); err != nil {
		t.Fatalf("complete provenance: %v", err)
	}
	if good.Canonical() == nil {
		t.Fatal("a complete provenance must encode")
	}
}
