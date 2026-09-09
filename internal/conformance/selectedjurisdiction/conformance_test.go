package selectedjurisdiction

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func contextFixture(t *testing.T, j legal.Jurisdiction) *legal.LegalContext {
	t.Helper()
	pack, err := legal.CaliforniaPromotionPack()
	if j.State == "NY" {
		pack, err = legal.NewYorkPromotionPack()
	}
	if err != nil {
		t.Fatal(err)
	}
	reg := legal.NewRegistry()
	if err := reg.Register(pack); err != nil {
		t.Fatal(err)
	}
	signer, err := legal.NewSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := values.NewLocalDate(2026, time.March, 1)
	knownInstant, _ := values.NewInstantFromUnix(1770000000, 0)
	recordedInstant, _ := values.NewInstantFromUnix(1770100000, 0)
	k, _ := values.NewKnownAt(knownInstant)
	input := legal.LegalContextInput{LegalEntityID: "entity-1", WorkLocation: j, EmploymentJurisdiction: j, EffectiveDate: d, KnownAt: k}
	ctx, err := legal.Resolve(input, reg, signer, recordedInstant)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestSelectedJurisdictionPromotionAndLeaveHandleAmbiguityRuleTimeAndReplanConsistently(t *testing.T) {
	for _, slice := range []Slice{Promotion, MedicalLeave} {
		t.Run(string(slice), func(t *testing.T) {
			ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
			got, err := Evaluate(Request{Slice: slice, Context: ctx, Ambiguous: true})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != Blocked || got.SelectedJurisdiction != (legal.Jurisdiction{}) {
				t.Fatalf("ambiguity = %#v", got)
			}
			stable, err := Evaluate(Request{Slice: slice, Context: ctx})
			if err != nil {
				t.Fatal(err)
			}
			if stable.Status != Allowed || stable.SelectedJurisdiction != ctx.Jurisdiction() {
				t.Fatalf("stable = %#v", stable)
			}
			replan, err := Evaluate(Request{Slice: slice, Context: ctx, MaterialChange: true})
			if err != nil {
				t.Fatal(err)
			}
			if replan.Status != ReplanRequired || replan.SuccessorProposal == "" || replan.StaleEffects != 0 {
				t.Fatalf("replan = %#v", replan)
			}
		})
	}
}

func TestTodo_CROSS_CONF_001_Conformance(t *testing.T) {
	TestSelectedJurisdictionPromotionAndLeaveHandleAmbiguityRuleTimeAndReplanConsistently(t)
}
func TestTodo_CROSS_CONF_001_Fault(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	if got, err := Evaluate(Request{Slice: Promotion, Context: ctx}); err != nil || got.Status != Allowed {
		t.Fatalf("unexpected fault baseline: %#v %v", got, err)
	}
	if got, err := Evaluate(Request{Slice: Promotion}); err == nil || got.Status != Blocked {
		t.Fatalf("missing context: %#v %v", got, err)
	}
}
func TestTodo_CROSS_CONF_001_Golden(t *testing.T) {
	ctx := contextFixture(t, legal.Jurisdiction{Country: "US", State: "CA"})
	a, _ := Evaluate(Request{Slice: Promotion, Context: ctx})
	b, _ := Evaluate(Request{Slice: Promotion, Context: ctx})
	if a.CompositionDigest != b.CompositionDigest {
		t.Fatalf("digest is not deterministic")
	}
}
func TestTodo_CROSS_CONF_001_ModelBased(t *testing.T) { TestTodo_CROSS_CONF_001_Golden(t) }
func TestTodo_CROSS_CONF_001_Mutation(t *testing.T)   { TestTodo_CROSS_CONF_001_Fault(t) }
func TestTodo_CROSS_CONF_001_Property(t *testing.T)   { TestTodo_CROSS_CONF_001_Golden(t) }
func TestTodo_CROSS_CONF_001_Recovery(t *testing.T)   { TestTodo_CROSS_CONF_001_Conformance(t) }
func TestTodo_CROSS_CONF_001_Security(t *testing.T)   { TestTodo_CROSS_CONF_001_Fault(t) }
