package snapshot_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	enginesnapshot "github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// TestDigestIsTheKernelsMaterialEncoding is the digest's definition, stated
// once: it is sha256 over the material payload the intent kernel itself
// produces for the material-input projection. If this package ever grew a
// second encoder, this is the test that would fail.
func TestDigestIsTheKernelsMaterialEncoding(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))

	sum := sha256.Sum256(snap.MaterialInputs().MaterialPayload().WireBytes)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if snap.Digest != want {
		t.Fatalf("digest %s, want the kernel material encoding %s", snap.Digest, want)
	}
	if schema := snap.MaterialInputs().MaterialPayload().Schema; schema.SchemaID != intent.MaterialSchema().SchemaID {
		t.Fatalf("the projection encodes under schema %q, want the kernel material schema %q",
			schema.SchemaID, intent.MaterialSchema().SchemaID)
	}
}

// TestMaterialInputsBindEveryInputOnce proves the projection asserts each
// bound input exactly once, under its own name, and never quotes a
// non-disclosed value.
func TestMaterialInputsBindEveryInputOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.BudgetDisclosable = false
	req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"
	snap, _ := promosnapshot.Build(t.Context(), h.readers(), req)

	projection := snap.MaterialInputs()
	seen := map[string]bool{}
	for _, assertion := range projection.CurrentState {
		if seen[assertion.FieldPath] {
			t.Fatalf("input %q is asserted twice", assertion.FieldPath)
		}
		seen[assertion.FieldPath] = true
		if assertion.Subject.Kind == "" || assertion.Subject.AuthorityDomain == "" {
			t.Fatalf("input %q asserts an unnamed subject", assertion.FieldPath)
		}
		if err := assertion.ResourceKey.Validate(); err != nil {
			t.Fatalf("input %q has an invalid resource key: %v", assertion.FieldPath, err)
		}
		if assertion.FieldPath != promosnapshot.InputBudgetAvailability {
			continue
		}
		if assertion.CanonicalText != string(promosnapshot.AvailabilityWithheld)+":" {
			t.Fatalf("the withheld budget input asserts %q, want a bare WITHHELD marker",
				assertion.CanonicalText)
		}
	}
	if len(seen) != len(promosnapshot.InputNames()) {
		t.Fatalf("the projection asserts %d input(s), want %d", len(seen), len(promosnapshot.InputNames()))
	}
	// Every assertion carries its availability first, so a disclosed value and
	// a denial can never encode identically.
	for _, assertion := range projection.CurrentState {
		if !strings.Contains(assertion.CanonicalText, ":") {
			t.Fatalf("input %q asserts %q with no availability marker",
				assertion.FieldPath, assertion.CanonicalText)
		}
	}
}

// TestBaselineSnapshotRecordsEveryNonDisclosureAsANegativeState proves the
// kernel baseline never carries a silent gap: an input that did not resolve to
// a value reaches intent.Preflight as a stated negative.
func TestBaselineSnapshotRecordsEveryNonDisclosureAsANegativeState(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.BudgetDisclosable = false
	req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"
	snap, _ := promosnapshot.Build(t.Context(), h.readers(), req)

	baseline := snap.BaselineSnapshot()
	nonDisclosed := 0
	for _, in := range snap.Inputs() {
		if in.Availability != promosnapshot.AvailabilityDisclosed {
			nonDisclosed++
		}
	}
	if len(baseline.NegativeStates) != nonDisclosed {
		t.Fatalf("the baseline records %d negative state(s) for %d non-disclosed input(s)",
			len(baseline.NegativeStates), nonDisclosed)
	}
	if len(baseline.PresentInputs)+nonDisclosed != len(promosnapshot.InputNames()) {
		t.Fatalf("the baseline accounts for %d input(s), want %d",
			len(baseline.PresentInputs)+nonDisclosed, len(promosnapshot.InputNames()))
	}
	redacted := 0
	unavailable := 0
	for _, state := range baseline.NegativeStates {
		switch state {
		case intent.NegativeRedacted:
			redacted++
		case intent.NegativeUnavailable:
			unavailable++
		}
	}
	if redacted != 1 {
		t.Fatalf("the baseline records %d REDACTED state(s), want the one withheld budget", redacted)
	}
	if unavailable != 1 {
		t.Fatalf("the baseline records %d UNAVAILABLE state(s), want the one absent vacancy", unavailable)
	}
}

// TestBaselineSnapshotKnowsBothSubjects proves the baseline names the worker
// and the target position, which is what lets the kernel resolve both subjects
// of a promotion against one snapshot.
func TestBaselineSnapshotKnowsBothSubjects(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))
	baseline := snap.BaselineSnapshot()

	kinds := map[string]bool{}
	for _, subject := range baseline.KnownSubjects {
		if err := subject.Validate(); err != nil {
			t.Fatalf("baseline subject %+v is invalid: %v", subject, err)
		}
		kinds[subject.Kind] = true
	}
	for _, want := range []string{"EMPLOYMENT", "POSITION", "BUDGET"} {
		if !kinds[want] {
			t.Fatalf("the baseline names no %s subject: %v", want, baseline.KnownSubjects)
		}
	}
}

// TestLookupAndDisclosedRefuseAnUndeclaredInput covers the two accessors'
// negative paths.
func TestLookupAndDisclosedRefuseAnUndeclaredInput(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))

	if _, ok := snap.Lookup("promotion.not_an_input"); ok {
		t.Fatal("Lookup answered for an undeclared input")
	}
	if _, ok := snap.Disclosed("promotion.not_an_input"); ok {
		t.Fatal("Disclosed answered for an undeclared input")
	}
	if _, ok := snap.Disclosed(promosnapshot.InputTargetPositionVacancy); ok {
		t.Fatal("Disclosed handed back an ABSENT input")
	}
}

// TestInputErrorIsTypedAndValueFree covers the refusal type on its own terms.
func TestInputErrorIsTypedAndValueFree(t *testing.T) {
	t.Parallel()
	err := error(&promosnapshot.InputError{
		InputName:    promosnapshot.InputBudgetAvailability,
		Verdict:      enginesnapshot.VerdictMissing,
		Availability: promosnapshot.AvailabilityAbsent,
		Detail:       "no compensation pool observation for this scope and period",
	})
	if !errors.Is(err, promosnapshot.ErrInputUnavailable) {
		t.Fatalf("an InputError does not match ErrInputUnavailable: %v", err)
	}
	if got := promosnapshot.InputNameOf(err); got != promosnapshot.InputBudgetAvailability {
		t.Fatalf("InputNameOf returned %q", got)
	}
	if got := promosnapshot.InputNameOf(errors.New("something else")); got != "" {
		t.Fatalf("InputNameOf invented the input name %q for an unrelated error", got)
	}
	message := err.Error()
	for _, want := range []string{promosnapshot.InputBudgetAvailability, "MISSING", "ABSENT"} {
		if !strings.Contains(message, want) {
			t.Fatalf("the refusal %q does not carry %q", message, want)
		}
	}
}
