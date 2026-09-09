package simulate

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// ProjectionReads answers an OBSERVE node from a pinned in-memory projection.
//
// It is a fixture, and it is honest about being one: it compares the projected
// state against the node's declared expected-state fields and cites a source
// watermark, so a PASS means "the projection agreed at this watermark" rather
// than "the call returned". Anything it cannot answer is UNKNOWN.
type ProjectionReads struct {
	// State is the projected value per expected-state field path, keyed by the
	// input field the observation compares.
	State map[string]string
	// Watermark is the source position every answer cites.
	Watermark string
	// Drifted forces the comparison to disagree, so a caller can exercise the
	// plan's degraded route without inventing a second fixture.
	Drifted bool
	// Unavailable makes the source unreadable, which is UNKNOWN and never
	// PASS.
	Unavailable bool
}

// PromotionReads returns the reference read port: the employment projection
// agrees with the proposed job at the environment's watermark.
func PromotionReads(env *Environment) ProjectionReads {
	return ProjectionReads{Watermark: env.Watermark}
}

// Observe implements ReadPort.
func (p ProjectionReads) Observe(_ context.Context, req ObservationRequest) (Observation, error) {
	if p.Unavailable {
		return Observation{
			Outcome: workflow.OutcomeUnknown,
			Outputs: Bag{},
			Detail:  "source " + req.Observe.SourceAuthority + " could not be read",
		}, nil
	}
	if p.Watermark == "" {
		return Observation{
			Outcome: workflow.OutcomeUnknown,
			Outputs: Bag{},
			Detail:  "source " + req.Observe.SourceAuthority + " cited no watermark",
		}, nil
	}

	// Every expected-state field the node declared has to be present in the
	// inputs, or there is nothing to compare and the answer is UNKNOWN.
	expected := map[string]string{}
	for _, path := range req.Observe.ExpectedStateFields {
		v, ok := req.Inputs[path]
		if !ok {
			return Observation{
				Outcome: workflow.OutcomeUnknown,
				Outputs: Bag{},
				Detail:  "expected state field " + path + " was not supplied",
			}, nil
		}
		expected[path] = v.Text
	}

	drift := p.Drifted
	observedJob := expected["expected_job_id"]
	for path, want := range expected {
		got, ok := p.State[path]
		if !ok {
			continue
		}
		if got != want {
			drift = true
			if path == "expected_job_id" {
				observedJob = got
			}
		}
	}
	if p.Drifted {
		observedJob = observedJob + "-DRIFTED"
	}

	outcome := workflow.OutcomePass
	detail := "projection agrees with the proposed placement at " + p.Watermark
	if drift {
		outcome = workflow.OutcomeFail
		detail = "projection disagrees with the proposed placement at " + p.Watermark
	}
	return Observation{
		Outcome: outcome,
		Outputs: Bag{
			"observed_job_id":  NewBranded("JobID", observedJob).Nullable(),
			"drift_detected":   NewBool(drift),
			"source_watermark": NewString(p.Watermark),
		},
		Watermark: p.Watermark,
		Detail:    detail,
	}, nil
}
