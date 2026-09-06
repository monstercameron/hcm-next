package timer

import (
	"errors"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/schedule"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Decision is what a declared misfire policy decided about one overdue timer.
// The values are internal/engines/schedule's own
// ([schedule.MisfireDecision]): this package classifies nothing on its own.
type Decision = schedule.MisfireDecision

// The decisions a timer can carry, re-exported so a caller does not have to
// name the schedule engine to read a [Settled] record.
const (
	// DecisionOnTime says the timer came due within its declared grace.
	DecisionOnTime = schedule.MisfireOnTime
	// DecisionFireNow says an overdue timer settles at the caller's instant
	// rather than its own.
	DecisionFireNow = schedule.MisfireFire
	// DecisionSkipped says an overdue timer is abandoned: the promise is
	// cancelled and nothing is woken.
	DecisionSkipped = schedule.MisfireSkipped
	// DecisionCatchUp says an overdue timer settles at its own instant, late.
	DecisionCatchUp = schedule.MisfireCatch
	// DecisionReview says an overdue timer needs a human. The row stays
	// pending.
	DecisionReview = schedule.MisfireNeedsReview
)

// misfireOccurrenceKey is the synthetic occurrence key [Decide] hands to
// SCHED-002. The policy engine sorts by instant and then by key; a single
// occurrence never needs disambiguating, so one constant is enough and it is
// never persisted.
const misfireOccurrenceKey = "hcmnext.workflow.timer.single-occurrence"

// Decide applies a declared SCHED-002 misfire policy to one durable timer.
//
// It is a thin, deliberate delegation: the timer's own instant becomes a
// one-element occurrence set and the caller's instant is the resume instant,
// so grace, catch-up bounds and the REVIEW refusal are all
// internal/engines/schedule's, evaluated by the same code a scheduled trigger
// goes through. Re-deriving them here would be a second authority on what
// "late" means.
//
// The returned instant is when the woken work becomes eligible: the timer's
// own instant for ON_TIME and CATCH_UP, the caller's for FIRE_NOW, and the
// zero instant for SKIP and REVIEW, which wake nothing.
func Decide(firesAt, now time.Time, config schedule.MisfireConfig) (Decision, time.Time, error) {
	if firesAt.IsZero() || now.IsZero() {
		return "", time.Time{}, errors.New("timer: misfire decision needs both the timer instant and the caller's instant")
	}
	if config.Policy == schedule.MisfireUnspecified {
		return "", time.Time{}, ErrMisfirePolicyRequired
	}
	// Validating the config here rather than only inside ApplyMisfirePolicy is
	// what lets the REVIEW branch below be sure the refusal it maps to a
	// decision really is "occurrences exceeded grace" and not a malformed
	// policy wearing the same sentinel.
	if err := config.Validate(); err != nil {
		return "", time.Time{}, err
	}
	occurrences := []schedule.Occurrence{{
		Key:    misfireOccurrenceKey,
		At:     values.NewInstant(firesAt),
		Source: schedule.SourceCron,
	}}
	out, err := schedule.ApplyMisfirePolicy(occurrences, values.NewInstant(now), config)
	if err != nil {
		// SCHED-002 answers REVIEW by refusing rather than by returning an
		// occurrence, because for a scheduled trigger there is nothing safe to
		// emit. Here the refusal is itself the decision: the timer stays
		// pending and a human resolves it.
		if config.Policy == schedule.MisfireReview {
			return DecisionReview, time.Time{}, nil
		}
		return "", time.Time{}, err
	}
	if len(out) == 0 {
		// SKIP drops the missed occurrence entirely.
		return DecisionSkipped, time.Time{}, nil
	}
	decided := out[0]
	switch decided.Misfire {
	case schedule.MisfireFire:
		return DecisionFireNow, now.UTC(), nil
	case schedule.MisfireOnTime, schedule.MisfireCatch:
		return decided.Misfire, firesAt.UTC(), nil
	default:
		return decided.Misfire, firesAt.UTC(), nil
	}
}
