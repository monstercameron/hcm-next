package temporal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// at builds an assertion at a (stream, sequence) with the given class,
// field and times, for the pure fold tests.
func at(seq int64, class datalogger.AssertionClass, field string, effective, recorded time.Time) Assertion {
	return Assertion{
		Ref:            datalogger.EventRef{StreamKey: "worker:1", Sequence: seq},
		SourceEventID:  uuid.New(),
		SchemaRef:      field,
		AssertionClass: class,
		EffectiveAt:    effective,
		RecordedAt:     recorded,
		Payload:        []byte(field + "@" + string(rune('0'+seq))),
	}
}

func corrects(a Assertion, seq int64) Assertion {
	a.Corrects = &datalogger.EventRef{StreamKey: "worker:1", Sequence: seq}
	return a
}

func TestResolveTruthClass(t *testing.T) {
	domain := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
	first := corrects(at(2, datalogger.Correction, "f@1", tMarch, tApril), 1)
	second := corrects(at(3, datalogger.Correction, "f@1", tMarch, tJune), 2)
	orphan := corrects(at(4, datalogger.Correction, "f@1", tMarch, tJune), 99)
	dangling := at(5, datalogger.Correction, "f@1", tMarch, tJune) // no Corrects at all

	visible := map[datalogger.EventRef]Assertion{}
	for _, a := range []Assertion{domain, first, second, orphan, dangling} {
		visible[a.Ref] = a
	}

	cases := []struct {
		name string
		a    Assertion
		want TruthClass
	}{
		{"a non-correction carries its own class", domain, TruthDomain},
		{"a correction inherits its target's class", first, TruthDomain},
		{"a chain of corrections inherits the origin's class", second, TruthDomain},
		{"a correction whose target is not visible fails closed", orphan, TruthUnresolved},
		{"a correction with no target at all fails closed", dangling, TruthUnresolved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTruthClass(tc.a, visible); got != tc.want {
				t.Fatalf("resolveTruthClass = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a cycle fails closed instead of looping", func(t *testing.T) {
		a := corrects(at(6, datalogger.Correction, "f@1", tMarch, tJune), 7)
		b := corrects(at(7, datalogger.Correction, "f@1", tMarch, tJune), 6)
		cyclic := map[datalogger.EventRef]Assertion{a.Ref: a, b.Ref: b}
		if got := resolveTruthClass(a, cyclic); got != TruthUnresolved {
			t.Fatalf("resolveTruthClass over a cycle = %q, want %q", got, TruthUnresolved)
		}
	})

	t.Run("an external observation is never anything else", func(t *testing.T) {
		obs := at(8, datalogger.ExternalObservation, "f@1", tJune, tJune)
		if got := resolveTruthClass(obs, visible); got != TruthObserved {
			t.Fatalf("resolveTruthClass = %q, want %q", got, TruthObserved)
		}
		if TruthObserved.Promotable() {
			t.Fatal("an external observation is promotable")
		}
	})
}

func TestBeatsIsTheLedgerResolutionOrder(t *testing.T) {
	base := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)

	laterEffective := at(1, datalogger.DomainFact, "f@1", tApril, tMarch)
	laterRecorded := at(1, datalogger.DomainFact, "f@1", tMarch, tApril)
	higherSequence := at(2, datalogger.DomainFact, "f@1", tMarch, tMarch)

	if !beats(laterEffective, base) {
		t.Error("a later effective instant does not win")
	}
	if beats(base, laterEffective) {
		t.Error("an earlier effective instant wins")
	}
	if !beats(laterRecorded, base) {
		t.Error("at equal effective time, a later recorded time does not win")
	}
	if !beats(higherSequence, base) {
		t.Error("at equal times, a higher sequence does not win")
	}
	if beats(base, base) {
		t.Error("an assertion beats itself; the comparison is not strict")
	}
	// Effective time outranks recorded time.
	if beats(laterRecorded, laterEffective) {
		t.Error("a later recorded time beat a later effective time")
	}
}

func TestFold(t *testing.T) {
	coord := Coordinate{EffectiveAt: tJune, KnownAt: tJune}

	t.Run("an observation never displaces a domain fact on the same field", func(t *testing.T) {
		domain := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
		observed := at(2, datalogger.ExternalObservation, "f@1", tApril, tApril)

		state := Fold("worker:1", coord, []Assertion{domain, observed})
		if len(state.Domain) != 1 || state.Domain[0].Ref.Sequence != 1 {
			t.Fatalf("domain state = %+v, want only the domain fact", state.Domain)
		}
		if len(state.Unpromoted) != 1 || state.Unpromoted[0].Ref.Sequence != 2 {
			t.Fatalf("unpromoted = %+v, want the observation", state.Unpromoted)
		}
		if state.Considered != 2 {
			t.Fatalf("considered %d assertions, want 2", state.Considered)
		}
	})

	t.Run("a superseded assertion is not part of the state", func(t *testing.T) {
		domain := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
		correction := corrects(at(2, datalogger.Correction, "f@1", tMarch, tApril), 1)

		state := Fold("worker:1", coord, []Assertion{domain, correction})
		if len(state.Domain) != 1 || state.Domain[0].Ref.Sequence != 2 {
			t.Fatalf("domain state = %+v, want only the correction", state.Domain)
		}
		if state.Domain[0].TruthClass != TruthDomain {
			t.Fatalf("the correction resolved to truth class %q, want %q", state.Domain[0].TruthClass, TruthDomain)
		}
	})

	t.Run("transaction records resolve separately from domain truth", func(t *testing.T) {
		domain := at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)
		transaction := at(2, datalogger.TransactionFact, "f@1", tApril, tApril)

		state := Fold("worker:1", coord, []Assertion{domain, transaction})
		if len(state.Domain) != 1 || len(state.Transaction) != 1 {
			t.Fatalf("state = domain %+v transaction %+v, want one of each", state.Domain, state.Transaction)
		}
		if state.Domain[0].Ref.Sequence != 1 || state.Transaction[0].Ref.Sequence != 2 {
			t.Fatal("the two truth classes were resolved against each other")
		}
	})

	t.Run("one winner per field, sorted by field", func(t *testing.T) {
		state := Fold("worker:1", coord, []Assertion{
			at(1, datalogger.DomainFact, "z@1", tMarch, tMarch),
			at(2, datalogger.DomainFact, "a@1", tMarch, tMarch),
			at(3, datalogger.DomainFact, "a@1", tApril, tApril),
		})
		if len(state.Domain) != 2 {
			t.Fatalf("domain holds %d assertions, want one per field", len(state.Domain))
		}
		if state.Domain[0].SchemaRef != "a@1" || state.Domain[1].SchemaRef != "z@1" {
			t.Fatalf("domain is ordered %q, %q; want sorted by field",
				state.Domain[0].SchemaRef, state.Domain[1].SchemaRef)
		}
		if state.Domain[0].Ref.Sequence != 3 {
			t.Fatalf("field a@1 resolved to sequence %d, want the later assertion 3", state.Domain[0].Ref.Sequence)
		}
	})

	t.Run("an empty set folds to an empty state, not a wrong one", func(t *testing.T) {
		state := Fold("worker:1", coord, nil)
		if len(state.Domain)+len(state.Transaction)+len(state.Unpromoted) != 0 {
			t.Fatalf("state = %+v, want empty", state)
		}
		if state.Subject != "worker:1" || !state.Coordinate.KnownAt.Equal(tJune) {
			t.Fatalf("state = %+v, want the subject and coordinate carried through", state)
		}
	})

	t.Run("folding is order independent", func(t *testing.T) {
		assertions := []Assertion{
			at(1, datalogger.DomainFact, "f@1", tMarch, tMarch),
			at(2, datalogger.ExternalObservation, "f@1", tApril, tApril),
			corrects(at(3, datalogger.Correction, "f@1", tMarch, tJune), 1),
		}
		forward, err := Fold("worker:1", coord, assertions).Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		reversed := []Assertion{assertions[2], assertions[1], assertions[0]}
		backward, err := Fold("worker:1", coord, reversed).Digest()
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if forward != backward {
			t.Fatalf("fold order changed the answer: %s vs %s", forward, backward)
		}
	})
}

func TestReconstructLimit(t *testing.T) {
	if got := reconstructLimit(Request{}); got != MaxReconstructEvents {
		t.Fatalf("default limit = %d, want %d", got, MaxReconstructEvents)
	}
	if got := reconstructLimit(Request{Limit: 5}); got != 5 {
		t.Fatalf("limit = %d, want the request's 5", got)
	}
	if got := reconstructLimit(Request{Limit: MaxReconstructEvents * 2}); got != MaxReconstructEvents {
		t.Fatalf("limit = %d, want it capped at %d", got, MaxReconstructEvents)
	}
}

// stubPlan answers with a fixed state, so plan-equivalence checking can be
// tested without a database.
type stubPlan struct {
	name  string
	state State
	err   error
}

func (p stubPlan) Name() string { return p.name }

func (p stubPlan) Reconstruct(context.Context, Querier, Request, Decision, ...Option) (State, error) {
	return p.state, p.err
}

func TestVerifyPlanEquivalence(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	req := Request{Tenant: tenant, Mode: ModeReconstruct, Subject: "worker:1"}
	dec := Decision{Tenant: tenant}

	agreeing := State{Subject: "worker:1", Domain: []Assertion{at(1, datalogger.DomainFact, "f@1", tMarch, tMarch)}}
	differing := agreeing
	differing.Domain = []Assertion{at(2, datalogger.DomainFact, "f@1", tApril, tApril)}

	t.Run("agreeing plans return the first plan's state", func(t *testing.T) {
		got, err := VerifyPlanEquivalence(ctx, nil, req, dec, []Plan{
			stubPlan{name: "a", state: agreeing},
			stubPlan{name: "b", state: agreeing},
		})
		if err != nil {
			t.Fatalf("VerifyPlanEquivalence: %v", err)
		}
		if len(got.Domain) != 1 || got.Domain[0].Ref.Sequence != 1 {
			t.Fatalf("returned %+v, want the first plan's state", got.Domain)
		}
	})

	t.Run("disagreeing plans are named", func(t *testing.T) {
		_, err := VerifyPlanEquivalence(ctx, nil, req, dec, []Plan{
			stubPlan{name: "fast", state: differing},
			stubPlan{name: "ledger", state: agreeing},
		})
		var disagree ErrPlansDisagree
		if !errors.As(err, &disagree) {
			t.Fatalf("VerifyPlanEquivalence = %v, want ErrPlansDisagree", err)
		}
		if disagree.PlanA != "fast" || disagree.PlanB != "ledger" {
			t.Fatalf("error names plans %q and %q, want fast and ledger", disagree.PlanA, disagree.PlanB)
		}
		if disagree.DigestA == disagree.DigestB {
			t.Fatal("the reported digests are equal; the comparison did not use them")
		}
	})

	t.Run("a single plan trivially agrees", func(t *testing.T) {
		if _, err := VerifyPlanEquivalence(ctx, nil, req, dec, []Plan{stubPlan{name: "only", state: agreeing}}); err != nil {
			t.Fatalf("VerifyPlanEquivalence: %v", err)
		}
	})

	t.Run("no plan at all is refused", func(t *testing.T) {
		_, err := VerifyPlanEquivalence(ctx, nil, req, dec, nil)
		var invalid ErrRequestInvalid
		if !errors.As(err, &invalid) {
			t.Fatalf("VerifyPlanEquivalence with no plan = %v, want ErrRequestInvalid", err)
		}
	})

	t.Run("a failing plan is reported by name and wraps its cause", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := VerifyPlanEquivalence(ctx, nil, req, dec, []Plan{
			stubPlan{name: "broken", err: boom},
		})
		if !errors.Is(err, boom) {
			t.Fatalf("VerifyPlanEquivalence = %v, want it to wrap the plan's failure", err)
		}
		if !strings.Contains(err.Error(), "broken") {
			t.Fatalf("error %q does not name the failing plan", err)
		}
	})

	t.Run("plan names are stable", func(t *testing.T) {
		if got := (LedgerPlan{}).Name(); got != "ledger" {
			t.Fatalf("LedgerPlan.Name() = %q, want %q", got, "ledger")
		}
		if got := (HistoryFoldPlan{}).Name(); got != "history-fold" {
			t.Fatalf("HistoryFoldPlan.Name() = %q, want %q", got, "history-fold")
		}
	})
}
