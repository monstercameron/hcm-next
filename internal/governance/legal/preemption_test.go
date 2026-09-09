package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type preemptionFixture struct {
	state, locality, source, section string
	kind                             ObligationType
}

var preemptionGoldenFixtures = []preemptionFixture{
	{state: "WI", locality: "Milwaukee", source: "wisconsin.md", section: "Wis. Stat. § 103.10(1m)", kind: ObligationTypeLeaveInteraction},
	{state: "LA", locality: "New Orleans", source: "louisiana.md", section: "La. R.S. 23:642", kind: ObligationTypeLeaveInteraction},
	{state: "TX", locality: "Austin", source: "texas.md", section: "HB 2127", kind: ObligationTypeLeaveInteraction},
	{state: "OK", locality: "Oklahoma City", source: "oklahoma.md", section: "40 O.S. § 160", kind: ObligationTypeLeaveInteraction},
	{state: "TN", locality: "Nashville", source: "tennessee.md", section: "Tenn. Code § 50-2-112", kind: ObligationTypeLeaveInteraction},
}

func preemptionPacks(f preemptionFixture) (JurisdictionSet, []RulePack) {
	state := Jurisdiction{Country: "US", State: f.state}
	locality := Jurisdiction{Country: "US", State: f.state, Locality: f.locality}
	citation := func(section string) Citation {
		return Citation{SourceFile: "planning/research/state-employment-law/" + f.source, Section: section, Status: ReviewStatusUnreviewed, ConfidenceMarker: ConfidenceMarkerConfirmed}
	}
	statePack := RulePack{
		PackID: "us-" + f.state + "-state", Version: 1, Jurisdiction: state,
		Window:               mustPreemptionWindow(),
		PreemptionAssertions: []PreemptionAssertion{{Kind: f.kind, Scope: "LOCALITY_ONLY", Citation: citation(f.section)}},
		LeaveInteractions:    []LeaveInteraction{{ID: "state-leave", LeaveType: "state", InteractionRule: "state rule remains", Citation: citation(f.section)}},
	}
	localPack := RulePack{
		PackID: "us-" + f.state + "-local", Version: 1, Jurisdiction: locality,
		Window:            mustPreemptionWindow(),
		LeaveInteractions: []LeaveInteraction{{ID: "local-leave", LeaveType: "local", InteractionRule: "local rule is removed", Citation: citation(f.section)}},
	}
	return JurisdictionSet{Primary: state, Overlays: []Jurisdiction{locality}}, []RulePack{statePack, localPack}
}

func mustPreemptionWindow() EffectiveWindow {
	start, _ := values.NewLocalDate(2020, time.January, 1)
	window, _ := NewOpenEffectiveWindow(start)
	return window
}

func TestTodo_LEGAL_013(t *testing.T) {
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	receipt, err := ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatalf("ComposeJurisdictionSet: %v", err)
	}
	if len(receipt.PreemptionsApplied) != 1 {
		t.Fatalf("preemptions = %+v", receipt.PreemptionsApplied)
	}
	record := receipt.PreemptionsApplied[0]
	if record.AssertingJurisdiction != set.Primary || !slices.Equal(record.RemovedObligationIDs, []string{"local-leave"}) {
		t.Fatalf("preemption record = %+v", record)
	}
	if len(receipt.Inputs) != 1 || receipt.Inputs[0].ID != "state-leave" {
		t.Fatalf("surviving inputs = %+v, want only subdivision obligation", receipt.Inputs)
	}
}

func TestTodo_LEGAL_013_Integration(t *testing.T) {
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	registry := NewRegistry()
	for _, pack := range packs {
		if err := registry.Register(pack); err != nil {
			t.Fatalf("Register(%s): %v", pack.PackID, err)
		}
	}
	signer := fixedSigner(t, 0x13)
	input := validInput(t, set.Overlays[0])
	ctx, err := Resolve(input, registry, signer, mustInstant(t, 1_770_100_000))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := ctx.RulePackReleases(); !slices.Equal(got, []RulePackRelease{packs[0].Release(), packs[1].Release()}) {
		t.Fatalf("pinned releases = %+v, want exact state then locality releases", got)
	}

	result, err := Evaluate(ctx, PromotionProposalSnapshot{OnProtectedLeave: true}, registry)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(result.PreemptionsApplied) != 1 || len(result.Obligations) != 1 || result.Obligations[0].ID != "state-leave" {
		t.Fatalf("pinned-release evaluation did not preempt before trigger evaluation: %+v", result)
	}
}

func TestTodo_LEGAL_013_Integration_LocalityResolutionBoundaries(t *testing.T) {
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	signer := fixedSigner(t, 0x14)
	now := mustInstant(t, 1_770_100_000)
	input := validInput(t, set.Overlays[0])

	t.Run("missing exact locality is recorded under permissive policy", func(t *testing.T) {
		registry := NewRegistry()
		if err := registry.Register(packs[0]); err != nil {
			t.Fatal(err)
		}
		ctx, err := Resolve(input, registry, signer, now)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got := ctx.RulePackReleases(); !slices.Equal(got, []RulePackRelease{packs[0].Release()}) {
			t.Fatalf("pinned releases = %+v, want exact state release only", got)
		}
		if !slices.Equal(ctx.UnregisteredLocalities(), []Jurisdiction{set.Overlays[0]}) || ctx.Verify() != nil {
			t.Fatalf("signed unregistered-locality evidence = %+v", ctx.UnregisteredLocalities())
		}
		result, err := Evaluate(ctx, PromotionProposalSnapshot{OnProtectedLeave: true}, registry)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(result.ReceiptNotes) != 1 || result.ReceiptNotes[0].Code != "unregistered_locality" || result.ReceiptNotes[0].Jurisdiction != set.Overlays[0] {
			t.Fatalf("receipt notes = %+v", result.ReceiptNotes)
		}
	})

	t.Run("tenant may configure missing locality to fail closed", func(t *testing.T) {
		registry := NewRegistry()
		if err := registry.Register(packs[0]); err != nil {
			t.Fatal(err)
		}
		closed := input
		closed.FailClosedOnUnregisteredLocality = true
		if _, err := Resolve(closed, registry, signer, now); !errors.Is(err, ErrLegalContextUnknown) {
			t.Fatalf("Resolve error = %v, want ErrLegalContextUnknown", err)
		}
	})

	t.Run("locality effective window is exact and start-inclusive", func(t *testing.T) {
		start := mustDate(t, 2026, time.March, 1)
		end := mustDate(t, 2026, time.April, 1)
		window, err := NewClosedEffectiveWindow(start, end)
		if err != nil {
			t.Fatal(err)
		}
		packs[1].Window = window
		registry := NewRegistry()
		for _, pack := range packs {
			if err := registry.Register(pack); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Resolve(input, registry, signer, now); err != nil {
			t.Fatalf("Resolve at inclusive locality start: %v", err)
		}
		before := input
		before.EffectiveDate = mustDate(t, 2026, time.February, 28)
		beforeContext, err := Resolve(before, registry, signer, now)
		if err != nil || !slices.Equal(beforeContext.UnregisteredLocalities(), []Jurisdiction{set.Overlays[0]}) {
			t.Fatalf("Resolve before locality window = context %+v, error %v", beforeContext, err)
		}
		atEnd := input
		atEnd.EffectiveDate = end
		endContext, err := Resolve(atEnd, registry, signer, now)
		if err != nil || !slices.Equal(endContext.UnregisteredLocalities(), []Jurisdiction{set.Overlays[0]}) {
			t.Fatalf("Resolve at exclusive locality end = context %+v, error %v", endContext, err)
		}
	})
}

func TestTodo_LEGAL_013_Property(t *testing.T) {
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	base, err := ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatal(err)
	}
	reversed := slices.Clone(packs)
	slices.Reverse(reversed)
	permuted, err := ComposeJurisdictionSet(set, reversed...)
	if err != nil {
		t.Fatal(err)
	}
	duplicated, err := ComposeJurisdictionSet(set, append(slices.Clone(packs), packs[0])...)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(base.Bytes(), permuted.Bytes()) || !slices.Equal(base.Bytes(), duplicated.Bytes()) {
		t.Fatal("preemption receipt is not commutative and idempotent")
	}
}

func TestTodo_LEGAL_013_LegacyReceiptCompatibility(t *testing.T) {
	set, packs := compositionFixtures(t)
	receipt, err := ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatal(err)
	}
	legacy := struct {
		Status         CompositionStatus          `json:"status"`
		Jurisdictions  []Jurisdiction             `json:"jurisdictions"`
		Inputs         []ObligationEvidence       `json:"inputs"`
		Obligations    []ComposedObligation       `json:"obligations"`
		Traces         []CompositionTrace         `json:"traces"`
		Contradictions []ContradictoryRequirement `json:"contradictions"`
	}{receipt.Status, receipt.Jurisdictions, receipt.Inputs, receipt.Obligations, receipt.Traces, receipt.Contradictions}
	want, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(receipt.CanonicalBytes(), want) {
		t.Fatalf("empty preemption metadata changed legacy canonical bytes\ngot  %s\nwant %s", receipt.CanonicalBytes(), want)
	}
}

func TestTodo_LEGAL_013_Golden(t *testing.T) {
	want := map[string]string{
		"WI": "8e0268b5288e3b197a4444eb9c2d04ed31c22d3b72b275e2a6d67a9095428780",
		"LA": "dd9a14733eff606921c18b77384356208cde2dd56d3864a55287fdd67fd195be",
		"TX": "20d0ceaf1b588c70adf88b9cd621a841210dca1060b1c07d4cdfa3c7544e321f",
		"OK": "3062baa116f176520c8156a8a95f77d1d2963dcf993bfacdc0c3a02ae0753947",
		"TN": "a7b54bb535fc52f6b2e620de6c5ce61b3ced202461e5812e52f8e392cc857fec",
	}
	for _, fixture := range preemptionGoldenFixtures {
		t.Run(fixture.state, func(t *testing.T) {
			set, packs := preemptionPacks(fixture)
			receipt, err := ComposeJurisdictionSet(set, packs...)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(receipt.Bytes())
			got := hex.EncodeToString(sum[:])
			if got != want[fixture.state] {
				t.Fatalf("receipt bytes digest = %s, want %s", got, want[fixture.state])
			}
		})
	}
}

func TestTodo_LEGAL_013_Mutation(t *testing.T) {
	set, packs := preemptionPacks(preemptionGoldenFixtures[0])
	packs[0].PreemptionAssertions[0].Citation.Section = ""
	if _, err := ComposeJurisdictionSet(set, packs...); err == nil {
		t.Fatal("composition accepted an uncited preemption assertion")
	}

	set, packs = preemptionPacks(preemptionGoldenFixtures[0])
	packs[0].PreemptionAssertions[0].Kind = ObligationTypeWageFloor
	receipt, err := ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Inputs) != 2 || len(receipt.PreemptionsApplied) != 0 {
		t.Fatalf("cross-kind assertion removed an obligation: %+v", receipt)
	}

	set, packs = preemptionPacks(preemptionGoldenFixtures[0])
	packs[1].Jurisdiction.Locality = ""
	set.Overlays = nil
	receipt, err = ComposeJurisdictionSet(set, packs...)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Inputs) != 2 {
		t.Fatalf("subdivision obligation was removed: %+v", receipt.Inputs)
	}

	set, packs = preemptionPacks(preemptionGoldenFixtures[0])
	conflictingBody := packs[0]
	conflictingBody.LeaveInteractions = slices.Clone(conflictingBody.LeaveInteractions)
	conflictingBody.LeaveInteractions[0].InteractionRule = "different release body"
	for _, candidates := range [][]RulePack{
		{packs[0], conflictingBody, packs[1]},
		{conflictingBody, packs[0], packs[1]},
	} {
		if _, err := ComposeJurisdictionSet(set, candidates...); !errors.Is(err, ErrConflictingDuplicateRelease) {
			t.Fatalf("conflicting obligation bodies error = %v, want ErrConflictingDuplicateRelease", err)
		}
	}

	conflictingAssertion := packs[0]
	conflictingAssertion.PreemptionAssertions = slices.Clone(conflictingAssertion.PreemptionAssertions)
	conflictingAssertion.PreemptionAssertions[0].Kind = ObligationTypeWageFloor
	for _, candidates := range [][]RulePack{
		{packs[0], conflictingAssertion, packs[1]},
		{conflictingAssertion, packs[0], packs[1]},
	} {
		if _, err := ComposeJurisdictionSet(set, candidates...); !errors.Is(err, ErrConflictingDuplicateRelease) {
			t.Fatalf("conflicting preemption assertions error = %v, want ErrConflictingDuplicateRelease", err)
		}
	}
}
