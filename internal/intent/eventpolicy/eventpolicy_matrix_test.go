package eventpolicy_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/eventpolicy"
)

func TestTodo_INTENT_018_Golden(t *testing.T) {
	converter := eventpolicy.NewConverter()
	accepted, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := converter.Convert(validPolicy(), eventpolicy.Observation{
		EventID: "evt-unknown", EventType: "payroll.hack", Schema: "evil/schema",
		SchemaVersion: "v9", Tenant: "tenant-9", Source: eventpolicy.SourceExternal,
	}, truthResolver())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(struct {
		Accepted  eventpolicy.Outcome `json:"accepted"`
		Duplicate eventpolicy.Outcome `json:"duplicate"`
		Unknown   eventpolicy.Outcome `json:"unknown"`
	}{accepted, duplicate, unknown}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_INTENT_018_Race(t *testing.T) {
	converter := eventpolicy.NewConverter()
	const workers = 16
	type result struct {
		outcome eventpolicy.Outcome
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
			results <- result{outcome, err}
		}()
	}
	wg.Wait()
	close(results)
	first := true
	var id string
	winners := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent Convert failed: %v", r.err)
		}
		if r.outcome.Decision != eventpolicy.DecisionAccepted || len(r.outcome.Intents) != 2 {
			t.Fatalf("concurrent Convert diverged: %+v", r.outcome)
		}
		if first {
			id = r.outcome.Intents[0].IntentID
			first = false
		}
		if r.outcome.Intents[0].IntentID != id {
			t.Fatal("concurrent redeliveries named different intents")
		}
		if !r.outcome.Duplicate {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d concurrent conversions claimed first acceptance, want exactly 1", winners)
	}
}

func TestTodo_INTENT_018_Integration(t *testing.T) {
	raw, err := json.Marshal(validObservation())
	if err != nil {
		t.Fatal(err)
	}
	var decoded eventpolicy.Observation
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("observation does not survive serialization: %v", err)
	}
	converter := eventpolicy.NewConverter()
	first, err := converter.Convert(validPolicy(), decoded, truthResolver())
	if err != nil {
		t.Fatal(err)
	}
	if first.Decision != eventpolicy.DecisionAccepted {
		t.Fatalf("decoded observation not accepted: %+v", first)
	}
	var replayed eventpolicy.Observation
	if err := json.Unmarshal(raw, &replayed); err != nil {
		t.Fatal(err)
	}
	second, err := converter.Convert(validPolicy(), replayed, truthResolver())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.Intents[0].IntentID != first.Intents[0].IntentID {
		t.Fatal("serialized redelivery diverged from the wire")
	}
	policyRaw, err := json.Marshal(validPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var policyDecoded eventpolicy.TriggerPolicy
	if err := json.Unmarshal(policyRaw, &policyDecoded); err != nil {
		t.Fatalf("policy does not survive serialization: %v", err)
	}
	third, err := eventpolicy.NewConverter().Convert(policyDecoded, decoded, truthResolver())
	if err != nil {
		t.Fatal(err)
	}
	if third.Intents[0].IntentID != first.Intents[0].IntentID {
		t.Fatal("deserialized policy derives different intent identities")
	}
}

func TestTodo_INTENT_018_Security(t *testing.T) {
	t.Run("cross-tenant policy does not govern foreign events", func(t *testing.T) {
		observation := validObservation()
		observation.Tenant = "tenant-2"
		outcome, err := eventpolicy.NewConverter().Convert(validPolicy(), observation, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != eventpolicy.DecisionUnknown || len(outcome.Intents) != 0 {
			t.Fatalf("foreign tenant governed: %+v", outcome)
		}
	})
	t.Run("external provider cannot escalate scope", func(t *testing.T) {
		observation := validObservation()
		observation.Source = eventpolicy.SourceExternal
		observation.Provider = "timekeeping-svc"
		observation.RequestedScope = []string{"org:acme", "payroll:admin"}
		outcome, err := eventpolicy.NewConverter().Convert(validPolicy(), observation, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != eventpolicy.DecisionQuarantined {
			t.Fatalf("scope escalation not quarantined: %+v", outcome)
		}
	})
	t.Run("in-policy requested scope still yields fixed scope", func(t *testing.T) {
		observation := validObservation()
		observation.Source = eventpolicy.SourceExternal
		observation.Provider = "timekeeping-svc"
		observation.RequestedScope = []string{"org:acme"}
		outcome, err := eventpolicy.NewConverter().Convert(validPolicy(), observation, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != eventpolicy.DecisionAccepted {
			t.Fatalf("in-policy scope rejected: %+v", outcome)
		}
		for _, named := range outcome.Intents {
			if len(named.Scope) != 1 || named.Scope[0] != "org:acme" {
				t.Fatalf("named intent carries non-fixed scope: %+v", named)
			}
		}
	})
	t.Run("truth is resolved, never taken from the payload", func(t *testing.T) {
		observation := validObservation()
		observation.PayloadDigest = "sha256:attacker-truth"
		outcome, err := eventpolicy.NewConverter().Convert(validPolicy(), observation, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		for _, named := range outcome.Intents {
			if named.TruthDigest != "sha256:current-truth" {
				t.Fatalf("truth taken from payload: %+v", named)
			}
			if named.ObservedDigest != "sha256:attacker-truth" {
				t.Fatalf("payload digest lost: %+v", named)
			}
		}
	})
}

func TestTodo_INTENT_018_Recovery(t *testing.T) {
	t.Run("refused observation leaves nothing", func(t *testing.T) {
		converter := eventpolicy.NewConverter()
		bad := validObservation()
		bad.EventID = ""
		if _, err := converter.Convert(validPolicy(), bad, truthResolver()); err == nil {
			t.Fatal("malformed observation accepted")
		}
		good, err := converter.Convert(validPolicy(), validObservation(), truthResolver())
		if err != nil {
			t.Fatalf("valid event after refusal failed: %v", err)
		}
		if good.Duplicate || good.Decision != eventpolicy.DecisionAccepted {
			t.Fatalf("refusal left state behind: %+v", good)
		}
	})
	t.Run("quarantine names nothing and replays identically", func(t *testing.T) {
		converter := eventpolicy.NewConverter()
		observation := validObservation()
		observation.RedeliveryCount = 99
		first, err := converter.Convert(validPolicy(), observation, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		second, err := converter.Convert(validPolicy(), observation, truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if first.Decision != eventpolicy.DecisionQuarantined || !second.Duplicate ||
			second.Evidence != first.Evidence || len(second.Intents) != 0 {
			t.Fatalf("quarantine not stable: %+v vs %+v", first, second)
		}
	})
	t.Run("policy revision re-keys identities", func(t *testing.T) {
		first, err := eventpolicy.NewConverter().Convert(validPolicy(), validObservation(), truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		revised := validPolicy()
		revised.PolicyVersion = "v4"
		second, err := eventpolicy.NewConverter().Convert(revised, validObservation(), truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if second.Intents[0].IntentID == first.Intents[0].IntentID {
			t.Fatal("policy revision kept the old intent identity")
		}
	})
}

func TestTodo_INTENT_018_Mutation(t *testing.T) {
	singles := []struct {
		name     string
		mutate   func(*eventpolicy.Observation)
		decision eventpolicy.Decision
	}{
		{"storm bound", func(o *eventpolicy.Observation) { o.RedeliveryCount = 99 }, eventpolicy.DecisionQuarantined},
		{"self cycle", func(o *eventpolicy.Observation) { o.CausationID = o.EventID }, eventpolicy.DecisionQuarantined},
		{"schema drift", func(o *eventpolicy.Observation) { o.Schema = "hcm/overtime" }, eventpolicy.DecisionUnknown},
		{"version drift", func(o *eventpolicy.Observation) { o.SchemaVersion = "v0" }, eventpolicy.DecisionUnknown},
		{"tenant drift", func(o *eventpolicy.Observation) { o.Tenant = "tenant-0" }, eventpolicy.DecisionUnknown},
		{"source drift", func(o *eventpolicy.Observation) { o.Source = "CARRIER_PIGEON" }, eventpolicy.DecisionUnknown},
	}
	for _, tc := range singles {
		t.Run(tc.name, func(t *testing.T) {
			observation := validObservation()
			tc.mutate(&observation)
			outcome, err := eventpolicy.NewConverter().Convert(validPolicy(), observation, truthResolver())
			if err != nil {
				t.Fatalf("mutant was an error instead of evidence: %v", err)
			}
			if outcome.Decision != tc.decision || len(outcome.Intents) != 0 {
				t.Fatalf("mutant survived: %+v", outcome)
			}
		})
	}
	t.Run("ignored type stays ignored", func(t *testing.T) {
		policy := validPolicy()
		rule := policy.Types["timesheet.approved"]
		rule.Ignore = true
		rule.IgnoreReason = "noisy duplicate feed, handled upstream"
		policy.Types["timesheet.approved"] = rule
		outcome, err := eventpolicy.NewConverter().Convert(policy, validObservation(), truthResolver())
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != eventpolicy.DecisionIgnored || !strings.Contains(outcome.Evidence, "handled upstream") {
			t.Fatalf("ignored type not evidenced: %+v", outcome)
		}
	})
	t.Run("resolver failure quarantines", func(t *testing.T) {
		failing := eventpolicy.TruthResolver(func(eventpolicy.Observation) (string, error) {
			return "", errors.New("truth store unavailable")
		})
		outcome, err := eventpolicy.NewConverter().Convert(validPolicy(), validObservation(), failing)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != eventpolicy.DecisionQuarantined {
			t.Fatalf("resolver failure not quarantined: %+v", outcome)
		}
	})
}
