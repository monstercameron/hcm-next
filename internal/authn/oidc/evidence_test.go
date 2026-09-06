package oidc_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/authn/oidc"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

func TestFlow_HandleCallback_EmitsEvidenceOnDenial(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, "rsa-1")},
		nil, 0,
	)
	sink := &recordingSink{}
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), sink)

	_, ev, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: "unknown", Code: "code-1", Now: baseTime})
	if err == nil {
		t.Fatalf("HandleCallback: got nil error, want one")
	}
	if ev.Outcome != oidc.OutcomeDenied {
		t.Fatalf("evidence outcome = %q, want DENIED", ev.Outcome)
	}
	if ev.Reason == "" {
		t.Fatalf("evidence has no reason on a denial")
	}
	if ev.EvidenceID == "" {
		t.Fatalf("evidence has no EvidenceID")
	}
	records := sink.all()
	if len(records) != 1 || records[0].EvidenceID != ev.EvidenceID {
		t.Fatalf("recording sink = %+v, want exactly one record matching the returned evidence", records)
	}
}

func TestFlow_HandleCallback_NilSinkIsSafe(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, "rsa-1")},
		nil, 0,
	)
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil) // no sink configured
	if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: "unknown", Code: "code-1", Now: baseTime}); err == nil {
		t.Fatalf("HandleCallback: got nil error, want one")
	}
}

func TestEvidence_IdenticalInputsProduceIdenticalID(t *testing.T) {
	t.Parallel()
	// Two independent denials for the exact same (tenant, issuer, subject,
	// outcome, reason, time) must carry the same evidence id -- the
	// digest is a pure function of those fields, not of call order or
	// wall-clock jitter.
	keys := newTestKeys(t)
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, "rsa-1")},
		nil, 0,
	)
	flow1 := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)
	flow2 := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)

	_, ev1, _ := flow1.HandleCallback(context.Background(), oidc.CallbackParams{State: "unknown", Code: "code-1", Now: baseTime})
	_, ev2, _ := flow2.HandleCallback(context.Background(), oidc.CallbackParams{State: "unknown", Code: "code-1", Now: baseTime})
	if ev1.EvidenceID != ev2.EvidenceID {
		t.Fatalf("evidence ids differ for identical inputs: %q vs %q", ev1.EvidenceID, ev2.EvidenceID)
	}
	if ev1.Reason != ev2.Reason {
		t.Fatalf("evidence reasons differ for identical inputs: %q vs %q", ev1.Reason, ev2.Reason)
	}
}
