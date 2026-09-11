package compensation_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func compDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		t.Fatal(err)
	}
	decimal, err := values.NewDecimal(text, 2, rounding)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return decimal
}

func compCurrent() []compensation.CurrentComponent {
	return []compensation.CurrentComponent{
		{Type: compensation.ComponentBase, Revision: 4, Retroactive: true},
		{Type: compensation.ComponentBonusTarget, Revision: 2},
		{Type: compensation.ComponentAllowance, Revision: 7},
	}
}

func compProposal(t *testing.T) compensation.PackageProposal {
	return compensation.PackageProposal{
		PackageRef:      "package/assignment-1",
		EffectiveDate:   "2026-10-01",
		SnapshotDigest:  "sha256:snap",
		Approvals:       []string{"approval/captains-1"},
		TransactionPlan: "txplan/88",
		Fence:           11,
		Components: []compensation.ComponentProposal{
			{Type: compensation.ComponentBase, Op: compensation.OpRevise,
				Revision: 4, Amount: compDecimal(t, "95000.00"), HasAmount: true,
				Currency: "USD", Frequency: "annual"},
		},
	}
}

func compPolicy() compensation.CompositionPolicy {
	return compensation.CompositionPolicy{
		Version:        "v1",
		AtomicBoundary: []compensation.ComponentType{compensation.ComponentBase, compensation.ComponentBonusTarget, compensation.ComponentAllowance},
		MaxChildren:    4,
	}
}

// TestCompensationIntentCompositionPreservesOmittedComponentsAndAtomicMeaning
// proves one package proposal composes parent and bounded child intents
// without losing omitted components, revisions, approvals, modes or the
// atomic boundary.
func TestCompensationIntentCompositionPreservesOmittedComponentsAndAtomicMeaning(t *testing.T) {
	composed, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
	if err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	if len(composed.Children) != 1 {
		t.Fatalf("children = %d, want one bounded child for the revised base", len(composed.Children))
	}
	byType := make(map[compensation.ComponentType]compensation.ComposedComponent)
	for _, component := range composed.Components {
		byType[component.Type] = component
	}
	if byType[compensation.ComponentBonusTarget].Revision != 2 || byType[compensation.ComponentAllowance].Revision != 7 {
		t.Fatalf("omitted components overwritten: %+v", composed.Components)
	}
	if byType[compensation.ComponentBase].Revision != 5 {
		t.Fatalf("revised base carries %+v, want revision 5", byType[compensation.ComponentBase])
	}
	if composed.Children[0].ApprovalRef != "approval/captains-1" {
		t.Fatalf("child approval drifted: %+v", composed.Children[0])
	}
	again, err := compensation.ComposePackage(compCurrent(), compProposal(t), compPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != composed.Digest {
		t.Fatal("same fence recomposed a different package")
	}

	adversaries := []struct {
		name   string
		mutate func(*compensation.PackageProposal)
		cause  error
	}{
		{"ambiguous null revise", func(p *compensation.PackageProposal) { p.Components[0].HasAmount = false }, compensation.ErrAmbiguousNull},
		{"end without condition", func(p *compensation.PackageProposal) {
			p.Components[0].Op = compensation.OpEnd
		}, compensation.ErrMissingEndCondition},
		{"unknown component", func(p *compensation.PackageProposal) {
			p.Components[0].Type = compensation.ComponentCommission
		}, compensation.ErrUnknownComponent},
		{"duplicate component", func(p *compensation.PackageProposal) {
			p.Components = append(p.Components, p.Components[0])
		}, compensation.ErrDuplicateComponent},
		{"lost retro mode", func(p *compensation.PackageProposal) {
			p.Components[0].Op = compensation.OpCorrect
			p.Components[0].CorrectionOf = "rev-4"
		}, compensation.ErrLostRetroMode},
		{"approval mismatch", func(p *compensation.PackageProposal) { p.Approvals = nil }, compensation.ErrApprovalMismatch},
		{"outside atomic boundary", func(p *compensation.PackageProposal) {
			p.Components = append(p.Components, compensation.ComponentProposal{
				Type: compensation.ComponentCommission, Op: compensation.OpAdd,
				Amount: compDecimal(t, "5000.00"), HasAmount: true, Currency: "USD", Frequency: "annual",
			})
		}, compensation.ErrOutsideAtomicBoundary},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			proposal := compProposal(t)
			tc.mutate(&proposal)
			if _, err := compensation.ComposePackage(compCurrent(), proposal, compPolicy()); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}

	t.Run("correction appends explainable chain", func(t *testing.T) {
		current := []compensation.CurrentComponent{{Type: compensation.ComponentBase, Revision: 4, Retroactive: true}}
		proposal := compProposal(t)
		proposal.Components[0].Op = compensation.OpCorrect
		proposal.Components[0].CorrectionOf = "rev-4"
		proposal.Components[0].RetroactiveReason = "backpay-q3"
		composed, err := compensation.ComposePackage(current, proposal, compPolicy())
		if err != nil {
			t.Fatalf("correction rejected: %v", err)
		}
		if len(composed.CorrectionChain) != 1 {
			t.Fatalf("correction chain = %+v", composed.CorrectionChain)
		}
	})
}
