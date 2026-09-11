package eventpolicy_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/eventpolicy"
)

var errTruthDown = errors.New("truth store unavailable")

func validPolicy() eventpolicy.TriggerPolicy {
	return eventpolicy.TriggerPolicy{
		PolicyVersion: "v3",
		Tenants:       []string{"tenant-1"},
		Types: map[string]eventpolicy.TypeRule{
			"timesheet.approved": {
				Schema:          "hcm/timesheet",
				AllowedVersions: []string{"v1", "v2"},
				Sources:         []eventpolicy.EventSource{eventpolicy.SourceInternal, eventpolicy.SourceExternal},
				Providers:       []string{"timekeeping-svc"},
				FixedScope:      []string{"org:acme"},
				Fanout:          2,
				RequireTruth:    true,
			},
		},
		MaxRedelivery: 3,
		MaxFanout:     4,
	}
}

func validObservation() eventpolicy.Observation {
	return eventpolicy.Observation{
		EventID:         "evt-001",
		EventType:       "timesheet.approved",
		Schema:          "hcm/timesheet",
		SchemaVersion:   "v2",
		Tenant:          "tenant-1",
		Source:          eventpolicy.SourceInternal,
		PayloadDigest:   "sha256:payload",
		CausationID:     "intent/payroll-run-87",
		RedeliveryCount: 0,
	}
}

func truthResolver() eventpolicy.TruthResolver {
	return func(eventpolicy.Observation) (string, error) {
		return "sha256:current-truth", nil
	}
}

// TestEventToIntentPolicyRejectsReplayStormAndAuthorityConfusion proves one
// accepted event names bounded intents under a deterministic causal key,
// redeliveries converge, and every policy violation is evidenced instead of
// inventing authority.
func TestEventToIntentPolicyRejectsReplayStormAndAuthorityConfusion(t *testing.T) {
	converter := eventpolicy.NewConverter()
	outcome, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
	if err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if outcome.Decision != eventpolicy.DecisionAccepted || len(outcome.Intents) != 2 {
		t.Fatalf("want 2 bounded intents, got %+v", outcome)
	}
	if outcome.Intents[0].IdempotencyKey == outcome.Intents[1].IdempotencyKey {
		t.Fatal("fanned-out intents share one idempotency key")
	}
	replay, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
	if err != nil {
		t.Fatalf("redelivery rejected: %v", err)
	}
	if !replay.Duplicate || len(replay.Intents) != 2 || replay.Intents[0].IntentID != outcome.Intents[0].IntentID {
		t.Fatalf("redelivery diverged: %+v vs %+v", outcome, replay)
	}

	adversaries := []struct {
		name      string
		mutate    func(*eventpolicy.Observation)
		policy    func(*eventpolicy.TriggerPolicy)
		resolver  eventpolicy.TruthResolver
		decision  eventpolicy.Decision
		expectErr bool
	}{
		{"replay storm", func(o *eventpolicy.Observation) { o.RedeliveryCount = 4 }, nil, nil, eventpolicy.DecisionQuarantined, false},

		{"self-caused cycle", func(o *eventpolicy.Observation) { o.CausationID = o.EventID }, nil, nil, eventpolicy.DecisionQuarantined, false},
		{"unsupported schema", func(o *eventpolicy.Observation) { o.Schema = "evil/schema" }, nil, nil, eventpolicy.DecisionUnknown, false},
		{"unsupported version", func(o *eventpolicy.Observation) { o.SchemaVersion = "v9" }, nil, nil, eventpolicy.DecisionUnknown, false},
		{"unknown tenant", func(o *eventpolicy.Observation) { o.Tenant = "tenant-9" }, nil, nil, eventpolicy.DecisionUnknown, false},
		{"unknown type", func(o *eventpolicy.Observation) { o.EventType = "payroll.hack" }, nil, nil, eventpolicy.DecisionUnknown, false},
		{"unlisted provider", func(o *eventpolicy.Observation) { o.Source = eventpolicy.SourceExternal; o.Provider = "mallory" }, nil, nil, eventpolicy.DecisionQuarantined, false},
		{"provider scope grab", func(o *eventpolicy.Observation) {
			o.Source = eventpolicy.SourceExternal
			o.Provider = "timekeeping-svc"
			o.RequestedScope = []string{"org:evil"}
		}, nil, nil, eventpolicy.DecisionQuarantined, false},
		{"payload as truth", nil, nil, nil, eventpolicy.DecisionQuarantined, false},
		{"unresolvable truth", nil, nil, eventpolicy.TruthResolver(func(eventpolicy.Observation) (string, error) { return "", errTruthDown }), eventpolicy.DecisionQuarantined, false},
		{"malformed event", func(o *eventpolicy.Observation) { o.EventID = "" }, nil, nil, "", true},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			policy := validPolicy()
			if tc.policy != nil {
				tc.policy(&policy)
			}
			observation := validObservation()
			if tc.mutate != nil {
				tc.mutate(&observation)
			}
			resolver := tc.resolver
			if resolver == nil && tc.name != "payload as truth" && tc.name != "unresolvable truth" {
				resolver = truthResolver()
			}
			outcome, err := eventpolicy.NewConverter().Convert(policy, observation, resolver)
			if tc.expectErr {
				if err == nil {
					t.Fatal("malformed event accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("violation was an error instead of evidence: %v", err)
			}
			if outcome.Decision != tc.decision {
				t.Fatalf("want %s, got %+v", tc.decision, outcome)
			}
			if len(outcome.Intents) != 0 {
				t.Fatalf("violating event named intents: %+v", outcome)
			}
			if outcome.Evidence == "" {
				t.Fatal("violation carries no evidence")
			}
		})
	}

	t.Run("conflicting redelivery quarantines without overwriting", func(t *testing.T) {
		converter := eventpolicy.NewConverter()
		original, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		tampered := validObservation()
		tampered.PayloadDigest = "sha256:tampered"
		conflict, err := converter.Convert(validPolicy(), tampered, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if conflict.Decision != eventpolicy.DecisionQuarantined || len(conflict.Intents) != 0 {
			t.Fatalf("conflicting redelivery not quarantined: %+v", conflict)
		}
		replay, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if !replay.Duplicate || replay.Intents[0].IntentID != original.Intents[0].IntentID {
			t.Fatalf("original delivery lost after conflict: %+v", replay)
		}
	})
}
