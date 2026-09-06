package evolution

import (
	"encoding/json"
	"errors"
	"flag"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent"
)

var updateGolden = flag.Bool("update", false, "rewrite the checked-in golden verdict table")

// TestTodo_INTENT_028_Golden pins the verdict table for one fixture pair of
// Promotion definition versions covering every named rule at once: an
// optional input added, a required input added, a required input removed,
// an input's kind changed, the effect class raised and the approval
// requirement removed.
func TestTodo_INTENT_028_Golden(t *testing.T) {
	report, err := CompatibilityCheck(promotionGoldenV1(), promotionGoldenV2())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.OK() {
		t.Fatal("the golden fixture pair must be INCOMPATIBLE")
	}

	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "promotion_verdict_table.golden.json")
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("golden verdict table drifted\n got: %s\nwant: %s", got, want)
	}
}

// TestTodo_INTENT_028_Property checks, over a randomized table of small
// synthetic definition pairs, that CompatibilityCheck's verdict always
// agrees with an independently computed oracle: INCOMPATIBLE if and only if
// at least one of the five named rules fired, and every fired rule's field
// is exactly named in a violation. The seed is fixed so the property never
// flakes.
func TestTodo_INTENT_028_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(28))
	for trial := 0; trial < 200; trial++ {
		prev, curr, wantIncompatible, wantFields := randomPromotionPair(rng)

		report, err := CompatibilityCheck(prev, curr)
		if err != nil {
			t.Fatalf("trial %d: unexpected error: %v", trial, err)
		}
		gotIncompatible := !report.OK()
		if gotIncompatible != wantIncompatible {
			t.Fatalf("trial %d: want incompatible=%v, got verdict %s (violations %+v)",
				trial, wantIncompatible, report.Verdict, report.Violations)
		}
		gotFields := map[string]bool{}
		for _, v := range report.Violations {
			gotFields[v.Field] = true
		}
		for f := range wantFields {
			if !gotFields[f] {
				t.Fatalf("trial %d: expected a violation naming %q, got %+v", trial, f, report.Violations)
			}
		}
		for f := range gotFields {
			if !wantFields[f] {
				t.Fatalf("trial %d: unexpected violation naming %q, got %+v", trial, f, report.Violations)
			}
		}
	}
}

// randomPromotionPair builds a random previous/current pair by applying a
// random subset of the five incompatible triggers (plus, always, one
// harmless optional-input addition) to the base fixture, and returns the
// oracle's own independent expectation.
func randomPromotionPair(rng *rand.Rand) (prev, curr intent.Definition, wantIncompatible bool, wantFields map[string]bool) {
	prev = promotionEffectBase(1, intent.EffectClassInternalMutation)
	curr = promotionEffectBase(2, intent.EffectClassInternalMutation)
	wantFields = map[string]bool{}

	// Always add a harmless optional input: it must never affect the
	// verdict, whatever else this trial does.
	curr.RequiredInputs = append(append([]intent.RequiredInput(nil), curr.RequiredInputs...),
		intent.RequiredInput{Path: "trial_note", Kind: intent.InputKindDocument, Required: false})

	if rng.Intn(2) == 0 {
		curr.RequiredInputs = append(curr.RequiredInputs,
			intent.RequiredInput{Path: "trial_required_addition", Kind: intent.InputKindReference, Required: true})
		wantIncompatible = true
		wantFields["trial_required_addition"] = true
	}
	if rng.Intn(2) == 0 {
		var kept []intent.RequiredInput
		for _, in := range curr.RequiredInputs {
			if in.Path == "reason_ref" {
				continue
			}
			kept = append(kept, in)
		}
		curr.RequiredInputs = kept
		wantIncompatible = true
		wantFields["reason_ref"] = true
	}
	if rng.Intn(2) == 0 {
		for i := range curr.RequiredInputs {
			if curr.RequiredInputs[i].Path == "target_position_ref" {
				curr.RequiredInputs[i].Kind = intent.InputKindScalar
			}
		}
		wantIncompatible = true
		wantFields["target_position_ref"] = true
	}
	if rng.Intn(2) == 0 {
		curr.EffectClass = intent.EffectClassExternalMutation
		wantIncompatible = true
		wantFields["effect_class"] = true
	}
	if rng.Intn(2) == 0 {
		curr.ApprovalRequired = false
		wantIncompatible = true
		wantFields["approval_required"] = true
	}
	return prev, curr, wantIncompatible, wantFields
}

// TestTodo_INTENT_028_Race runs CompatibilityCheck, DefinitionDigest and
// RefuseInPlaceEdit concurrently from many goroutines over the same shared
// fixture values, and checks every goroutine observed the identical,
// deterministic result: these functions read their arguments and never
// mutate shared state, so concurrent use is safe with or without the race
// detector.
func TestTodo_INTENT_028_Race(t *testing.T) {
	prev := promotionV1()
	curr := promotionV2RequiredInputAdded()

	wantReport, err := CompatibilityCheck(prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDigest, err := DefinitionDigest(prev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const goroutines = 32
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*3)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := CompatibilityCheck(prev, curr)
			if err != nil {
				errs <- err
				return
			}
			if !reflect.DeepEqual(r, wantReport) {
				errs <- errFormat("compatibility report diverged under concurrency")
				return
			}
			d, err := DefinitionDigest(prev)
			if err != nil {
				errs <- err
				return
			}
			if d != wantDigest {
				errs <- errFormat("digest diverged under concurrency")
				return
			}
			if err := RefuseInPlaceEdit(prev, prev); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type errFormat string

func (e errFormat) Error() string { return string(e) }

// TestTodo_INTENT_028_Fault proves malformed input is refused with a typed
// error rather than a panic or a silently wrong verdict.
func TestTodo_INTENT_028_Fault(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on malformed input: %v", r)
		}
	}()

	if _, err := CompatibilityCheck(intent.Definition{}, intent.Definition{}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("want ErrInvalidDefinition for two zero-value definitions, got %v", err)
	}
	if _, err := DefinitionDigest(intent.Definition{}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("want ErrInvalidDefinition for a zero-value definition, got %v", err)
	}
	if _, err := NewSupersessionRecord(Report{}, "", "", "", mustInstant(2026, 9, 5), PolicyDrain); err == nil {
		t.Fatal("want an error for a zero-value (COMPATIBLE) report")
	}
	if err := RefuseInPlaceEdit(intent.Definition{}, intent.Definition{}); err == nil {
		t.Fatal("want an error digesting two invalid same-ref definitions")
	}
}

// TestTodo_INTENT_028_Security proves the segregation-of-duties rule: the
// principal who authored an incompatible successor may never also be its
// approver, closing the self-approval bypass a governance ceremony exists
// to prevent.
func TestTodo_INTENT_028_Security(t *testing.T) {
	report := incompatibleReport(t)
	if _, err := NewSupersessionRecord(report, "reason", "cam", "cam",
		mustInstant(2026, 9, 5), PolicyDrain); !errors.Is(err, ErrApproverIsAuthor) {
		t.Fatalf("want ErrApproverIsAuthor, got %v", err)
	}
	// A digest-tampered record must never verify, even when only one field
	// was flipped to something that looks like a legitimate value.
	rec, err := NewSupersessionRecord(report, "reason", "author-1", "approver-1",
		mustInstant(2026, 9, 5), PolicyDrain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.ApproverPrincipalID = "attacker"
	if err := rec.Verify(); !errors.Is(err, ErrSupersessionTampered) {
		t.Fatalf("want ErrSupersessionTampered after swapping the approver post-hoc, got %v", err)
	}
}

// TestTodo_INTENT_028_Conformance sweeps every named rule in isolation and
// proves each produces exactly the declared verdict naming exactly the
// declared field: the one compatible trigger (an added optional input) and
// the five incompatible triggers (required input added, required input
// removed, input kind changed, effect class raised, approval requirement
// removed).
func TestTodo_INTENT_028_Conformance(t *testing.T) {
	cases := []struct {
		name       string
		prev, curr intent.Definition
		verdict    Verdict
		code       ChangeCode
		field      string
	}{
		{"optional_input_added", promotionV1(), promotionV2CompatibleOptionalAdded(),
			VerdictCompatible, ChangeOptionalInputAdded, "notes"},
		{"required_input_added", promotionV1(), promotionV2RequiredInputAdded(),
			VerdictIncompatible, ChangeRequiredInputAdded, "compensation_committee_approval_ref"},
		{"required_input_removed", promotionV1(), promotionV2RequiredInputRemoved(),
			VerdictIncompatible, ChangeRequiredInputRemoved, "reason_ref"},
		{"input_type_changed", promotionV1(), promotionV2InputTypeChanged(),
			VerdictIncompatible, ChangeInputTypeChanged, "target_position_ref"},
		{"effect_class_raised", promotionV1EffectInternal(), promotionV2EffectRaised(),
			VerdictIncompatible, ChangeEffectClassRaised, "effect_class"},
		{"approval_requirement_removed", promotionV1(), promotionV2ApprovalRemoved(),
			VerdictIncompatible, ChangeApprovalRequirementRemoved, "approval_required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := CompatibilityCheck(c.prev, c.curr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Verdict != c.verdict {
				t.Fatalf("want verdict %s, got %s (%+v / %+v)", c.verdict, r.Verdict, r.Changes, r.Violations)
			}
			if c.verdict == VerdictCompatible {
				for _, ch := range r.Changes {
					if ch.Code == c.code && ch.Field == c.field {
						return
					}
				}
				t.Fatalf("want a %s change naming %s, got %+v", c.code, c.field, r.Changes)
			}
			for _, v := range r.Violations {
				if v.Code == c.code && v.Field == c.field {
					return
				}
			}
			t.Fatalf("want a %s violation naming %s, got %+v", c.code, c.field, r.Violations)
		})
	}
}

// TestTodo_INTENT_028_Recovery proves the recovery path from a refused
// mutation attempt: an in-place edit of a published version is refused, and
// the correct next step — bumping the version and going through
// CompatibilityCheck plus, where needed, a SupersessionRecord — succeeds
// cleanly afterwards.
func TestTodo_INTENT_028_Recovery(t *testing.T) {
	published := promotionV1()

	attemptedEdit := published
	attemptedEdit.ApprovalRequired = false
	if err := RefuseInPlaceEdit(published, attemptedEdit); !errors.Is(err, ErrInPlaceEdit) {
		t.Fatalf("want the in-place edit refused, got %v", err)
	}

	// Recovery: the same content change, correctly proposed as a new,
	// advancing version instead.
	properSuccessor := promotionBase(2)
	properSuccessor.ApprovalRequired = false
	report, err := CompatibilityCheck(published, properSuccessor)
	if err != nil {
		t.Fatalf("the correctly versioned proposal must be judged cleanly: %v", err)
	}
	if report.OK() {
		t.Fatal("removing the approval requirement must still be INCOMPATIBLE")
	}
	record, err := NewSupersessionRecord(report, "recovered from a refused in-place edit",
		"author-1", "approver-1", mustInstant(2026, 9, 5), PolicyMigrateWithPreview)
	if err != nil {
		t.Fatalf("a properly versioned incompatible change must be supersedable: %v", err)
	}
	if err := record.Verify(); err != nil {
		t.Fatalf("the recovered record must verify: %v", err)
	}
}

// TestTodo_INTENT_028_Mutation proves CompatibilityCheck and
// NewSupersessionRecord never mutate their inputs: a caller's definition
// values and required-input slices come back exactly as they went in.
func TestTodo_INTENT_028_Mutation(t *testing.T) {
	prev := promotionV1()
	curr := promotionV2RequiredInputAdded()
	prevInputsBefore := append([]intent.RequiredInput(nil), prev.RequiredInputs...)
	currInputsBefore := append([]intent.RequiredInput(nil), curr.RequiredInputs...)

	report, err := CompatibilityCheck(prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(prev.RequiredInputs, prevInputsBefore) {
		t.Fatal("CompatibilityCheck mutated the previous definition's required inputs")
	}
	if !reflect.DeepEqual(curr.RequiredInputs, currInputsBefore) {
		t.Fatal("CompatibilityCheck mutated the current definition's required inputs")
	}

	reportBefore := report
	rec, err := NewSupersessionRecord(report, "reason", "author-1", "approver-1",
		mustInstant(2026, 9, 5), PolicyDrain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(report, reportBefore) {
		t.Fatal("NewSupersessionRecord mutated the report it was given")
	}
	_ = rec
}
