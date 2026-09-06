package cryptoagile

import (
	"errors"
	"testing"
	"time"
)

func seedFor(b byte) []byte {
	s := make([]byte, 32)
	for i := range s {
		s[i] = b
	}
	return s
}

func TestDualSigner_SignsBothSuitesDuringWindow(t *testing.T) {
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("old", seedFor(1)); err != nil {
		t.Fatalf("AddEd25519 old: %v", err)
	}
	if err := keys.AddEd25519("new", seedFor(2)); err != nil {
		t.Fatalf("AddEd25519 new: %v", err)
	}

	plan := MigrationPlan{Windows: []Window{
		{Start: day(0), End: day(10), ActiveSuiteID: "old"},
		{Start: day(10), End: day(20), ActiveSuiteID: "new", DualSuiteID: "old"},
		{Start: day(20), ActiveSuiteID: "new"},
	}}

	clock := day(15)
	signer, err := NewDualSigner(plan, keys, func() time.Time { return clock })
	if err != nil {
		t.Fatalf("NewDualSigner: %v", err)
	}

	envs, err := signer.SignAll([]byte("payload"))
	if err != nil {
		t.Fatalf("SignAll: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("SignAll returned %d envelopes, want 2 during the dual window", len(envs))
	}
	if envs[0].SuiteID != "new" || envs[1].SuiteID != "old" {
		t.Fatalf("SignAll order = [%s, %s], want [new, old] (active first, dual second)", envs[0].SuiteID, envs[1].SuiteID)
	}

	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "old", Kind: KindSignature, Status: StatusDual, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register old: %v", err)
	}
	if err := registry.Register(AlgorithmSuite{ID: "new", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(10)}); err != nil {
		t.Fatalf("Register new: %v", err)
	}
	verifier := NewEnvelopeVerifier(registry, keys)
	for _, env := range envs {
		if err := verifier.Verify([]byte("payload"), env); err != nil {
			t.Fatalf("Verify(%s): %v", env.SuiteID, err)
		}
	}
}

func TestDualSigner_SingleSuiteOutsideDualWindow(t *testing.T) {
	keys := NewFakeKeySource()
	if err := keys.AddEd25519("old", seedFor(1)); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}
	plan := MigrationPlan{Windows: []Window{
		{Start: day(0), ActiveSuiteID: "old"},
	}}
	signer, err := NewDualSigner(plan, keys, func() time.Time { return day(5) })
	if err != nil {
		t.Fatalf("NewDualSigner: %v", err)
	}
	envs, err := signer.SignAll([]byte("payload"))
	if err != nil {
		t.Fatalf("SignAll: %v", err)
	}
	if len(envs) != 1 || envs[0].SuiteID != "old" {
		t.Fatalf("SignAll = %+v, want exactly one envelope for suite old", envs)
	}
}

func TestDualSigner_RefusesTimeOutsidePlan(t *testing.T) {
	keys := NewFakeKeySource()
	plan := MigrationPlan{Windows: []Window{{Start: day(10), ActiveSuiteID: "old"}}}
	signer, err := NewDualSigner(plan, keys, func() time.Time { return day(0) })
	if err != nil {
		t.Fatalf("NewDualSigner: %v", err)
	}
	if _, err := signer.SignAll([]byte("m")); !errors.Is(err, ErrNoCurrentWindow) {
		t.Fatalf("SignAll before plan start = %v, want ErrNoCurrentWindow", err)
	}
}

func TestNewDualSigner_RefusesInvalidPlan(t *testing.T) {
	if _, err := NewDualSigner(MigrationPlan{}, NewFakeKeySource(), nil); !errors.Is(err, ErrPlanEmpty) {
		t.Fatalf("NewDualSigner(empty plan) = %v, want ErrPlanEmpty", err)
	}
}

func TestNewDualSigner_RefusesNilKeys(t *testing.T) {
	plan := MigrationPlan{Windows: []Window{{Start: day(0), ActiveSuiteID: "a"}}}
	if _, err := NewDualSigner(plan, nil, nil); err == nil {
		t.Fatalf("NewDualSigner(nil keys) = nil, want error")
	}
}
