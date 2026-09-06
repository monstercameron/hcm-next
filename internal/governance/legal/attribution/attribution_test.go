package attribution

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTodo_LEGAL_TOOL_011(t *testing.T) {
	config, err := LoadFixture("remote-workers.yaml")
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	input, err := config.DecodeFixtureFacts("remote-one-state")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Resolve(input)
	if err != nil {
		t.Fatalf("Resolve A3: %v", err)
	}
	if result.Rule != "A3" || result.Primary.String() != "US-CO" {
		t.Fatalf("A3 result = %+v", result)
	}
	if result.Residence.String() != "US-GA" {
		t.Fatalf("residence = %s, want US-GA evidence", result.Residence)
	}
	if result.Confidence.String() != "ASSERTED" {
		t.Fatalf("confidence = %s, want ASSERTED when employment assertion disagrees", result.Confidence)
	}
	if !strings.Contains(result.Explain(), "rule=A3") {
		t.Fatalf("Explain = %q", result.Explain())
	}
}

func TestTodo_LEGAL_TOOL_011_Golden(t *testing.T) {
	config, err := LoadFixture("remote-workers.yaml")
	if err != nil {
		t.Fatal(err)
	}
	input, err := config.DecodeFixtureFacts("remote-threshold-winner")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Resolve(input)
	if err != nil {
		t.Fatalf("Resolve A4: %v", err)
	}
	if result.Rule != "A4" || result.Primary.String() != "US-CO" || len(result.Exposures) != 1 || result.Exposures[0].Jurisdiction.String() != "US-AL" {
		t.Fatalf("A4 result = %+v", result)
	}
	if result.Exposures[0].Share.String() != "0.2500" {
		t.Fatalf("A4 exposure share = %s", result.Exposures[0].Share)
	}

	noThreshold, err := config.DecodeFixtureFacts("remote-no-threshold")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(noThreshold); err == nil || !errors.Is(err, ErrLegalContextUnknown) || !errors.Is(err, ErrMultiStateUnresolved) || !strings.Contains(err.Error(), "primary_work_threshold") {
		t.Fatalf("A5 missing threshold error = %v", err)
	}
	noWinner, err := config.DecodeFixtureFacts("remote-no-winner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(noWinner); err == nil || !errors.Is(err, ErrMultiStateUnresolved) {
		t.Fatalf("A5 no winner error = %v", err)
	}
}

func TestTodo_LEGAL_TOOL_011_Conformance(t *testing.T) {
	config, err := LoadFixture("remote-workers.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Registry) != 50 || len(config.Fixtures) < 4 {
		t.Fatalf("fixture shape = %d registry rows, %d worker fixtures", len(config.Registry), len(config.Fixtures))
	}
	for _, row := range config.Registry {
		if row.Review != ReviewReviewed && row.Review != ReviewUnreviewed {
			t.Fatalf("state %s has invalid review %q", row.State, row.Review)
		}
	}
	input, err := config.DecodeFixtureFacts("remote-threshold-winner")
	if err != nil {
		t.Fatal(err)
	}
	port := factsPort{facts: input.Facts}
	got, err := Check(context.Background(), input.Facts.WorkerID, input.Window, input.Policy, port)
	if err != nil {
		t.Fatalf("Check through port: %v", err)
	}
	if got.Primary.String() != "US-CO" || got.Rule != "A4" || len(got.Digest) != 64 {
		t.Fatalf("port result = %+v", got)
	}
	bad := config
	bad.Registry[0].StatuteCitation = ""
	if err := bad.Validate(); err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "registry[0].statute_citation") {
		t.Fatalf("bad registry error = %v, want typed field refusal", err)
	}
}

type factsPort struct{ facts WorkerFacts }

func (p factsPort) WorkerFacts(context.Context, string, DateWindow) (WorkerFacts, error) {
	return p.facts, nil
}
