package compensation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrInvalidPackage reports a package proposal that cannot compose: a
	// missing ref, date, snapshot, approval, transaction plan or fence.
	ErrInvalidPackage = errors.New("compensation: invalid package proposal")

	// ErrAmbiguousNull reports a REVISE without an amount: null is never
	// an implicit instruction.
	ErrAmbiguousNull = errors.New("compensation: ambiguous null revision")

	// ErrMissingEndCondition reports an END without its end condition.
	ErrMissingEndCondition = errors.New("compensation: end without condition")

	// ErrUnknownComponent reports a non-ADD operation on a component the
	// current package does not carry.
	ErrUnknownComponent = errors.New("compensation: unknown component")

	// ErrDuplicateComponent reports one component proposed twice.
	ErrDuplicateComponent = errors.New("compensation: duplicate component")

	// ErrLostRetroMode reports a correction of a retroactive component
	// without its retroactive reason.
	ErrLostRetroMode = errors.New("compensation: correction lost retro mode")

	// ErrApprovalMismatch reports a package whose approvals cannot cover
	// its children: every child binds one of the proposal approvals.
	ErrApprovalMismatch = errors.New("compensation: child approval differs from proposal")

	// ErrOutsideAtomicBoundary reports a changed component outside the
	// policy atomic boundary.
	ErrOutsideAtomicBoundary = errors.New("compensation: component outside atomic boundary")
)

// ComponentType is one package component kind.
type ComponentType string

// Component kinds.
const (
	ComponentBase        ComponentType = "BASE"
	ComponentBonusTarget ComponentType = "BONUS_TARGET"
	ComponentAllowance   ComponentType = "ALLOWANCE"
	ComponentCommission  ComponentType = "COMMISSION"
)

// ComponentOp is one component operation. Absent marks an omitted
// component, which composition carries forward untouched.
type ComponentOp string

// Component operations.
const (
	OpAdd     ComponentOp = "ADD"
	OpRevise  ComponentOp = "REVISE"
	OpEnd     ComponentOp = "END"
	OpCorrect ComponentOp = "CORRECT"
)

// CurrentComponent is one carried package fact.
type CurrentComponent struct {
	Type        ComponentType
	Revision    uint64
	Retroactive bool
}

// ComponentProposal is one mentioned component. Unmentioned components
// stay omitted: composition never invents them.
type ComponentProposal struct {
	Type              ComponentType
	Op                ComponentOp
	Revision          uint64
	Amount            values.Decimal
	HasAmount         bool
	Currency          string
	Frequency         string
	EndCondition      string
	CorrectionOf      string
	RetroactiveReason string
}

// PackageProposal is one versioned package change.
type PackageProposal struct {
	PackageRef      string
	EffectiveDate   string
	SnapshotDigest  string
	Approvals       []string
	TransactionPlan string
	Fence           uint64
	Components      []ComponentProposal
}

// CompositionPolicy bounds one composition.
type CompositionPolicy struct {
	Version        string
	AtomicBoundary []ComponentType
	MaxChildren    int
}

// ChildIntent is one bounded child intent for one changed component.
type ChildIntent struct {
	Component   ComponentType
	Op          ComponentOp
	Revision    uint64
	ApprovalRef string
}

// ComposedComponent is one output component: changed or carried.
type ComposedComponent struct {
	Type        ComponentType
	Op          ComponentOp
	Revision    uint64
	Carried     bool
	Retroactive bool
}

// ComposedPackage is one composed proposal: the parent plus bounded
// children, one aggregate digest and the explainable correction chain.
type ComposedPackage struct {
	PackageRef      string
	Children        []ChildIntent
	Components      []ComposedComponent
	CorrectionChain []string
	Digest          string
}

// ComposePackage composes one proposal over its current package. It is
// pure: the same fence over the same inputs always yields the same
// package, so a failed external effect can retry without rerunning the
// mutation.
func ComposePackage(current []CurrentComponent, proposal PackageProposal, policy CompositionPolicy) (ComposedPackage, error) {
	if strings.TrimSpace(proposal.PackageRef) == "" || strings.TrimSpace(proposal.EffectiveDate) == "" ||
		strings.TrimSpace(proposal.SnapshotDigest) == "" ||
		strings.TrimSpace(proposal.TransactionPlan) == "" || proposal.Fence == 0 {
		return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage: %w", ErrInvalidPackage)
	}
	if len(proposal.Approvals) == 0 {
		return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage: %w", ErrApprovalMismatch)
	}
	if strings.TrimSpace(policy.Version) == "" || policy.MaxChildren <= 0 {
		return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage: %w", ErrInvalidPackage)
	}
	carried := make(map[ComponentType]CurrentComponent, len(current))
	for _, component := range current {
		carried[component.Type] = component
	}
	boundary := make(map[ComponentType]bool, len(policy.AtomicBoundary))
	for _, component := range policy.AtomicBoundary {
		boundary[component] = true
	}
	seen := make(map[ComponentType]bool, len(proposal.Components))
	composed := ComposedPackage{PackageRef: proposal.PackageRef}
	for i, component := range proposal.Components {
		field := fmt.Sprintf("components[%d]", i)
		if seen[component.Type] {
			return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrDuplicateComponent)
		}
		seen[component.Type] = true
		previous, known := carried[component.Type]
		if !known && component.Op != OpAdd {
			return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrUnknownComponent)
		}
		switch component.Op {
		case OpAdd:
			if !component.HasAmount {
				return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrAmbiguousNull)
			}
		case OpRevise:
			if !component.HasAmount {
				return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrAmbiguousNull)
			}
			if component.Revision != previous.Revision {
				return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrInvalidPackage)
			}
		case OpEnd:
			if strings.TrimSpace(component.EndCondition) == "" {
				return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrMissingEndCondition)
			}
		case OpCorrect:
			if strings.TrimSpace(component.CorrectionOf) == "" {
				return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrInvalidPackage)
			}
			if previous.Retroactive && strings.TrimSpace(component.RetroactiveReason) == "" {
				return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrLostRetroMode)
			}
		default:
			return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrInvalidPackage)
		}
		if !boundary[component.Type] {
			return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage %s: %w", field, ErrOutsideAtomicBoundary)
		}
	}
	if len(seen) > policy.MaxChildren {
		return ComposedPackage{}, fmt.Errorf("compensation: ComposePackage: %w", ErrInvalidPackage)
	}
	approval := proposal.Approvals[0]
	order := []ComponentType{ComponentBase, ComponentBonusTarget, ComponentAllowance, ComponentCommission}
	byType := make(map[ComponentType]ComponentProposal, len(proposal.Components))
	for _, component := range proposal.Components {
		byType[component.Type] = component
	}
	for _, kind := range order {
		proposed, mentioned := byType[kind]
		previous, known := carried[kind]
		switch {
		case !mentioned && known:
			composed.Components = append(composed.Components, ComposedComponent{
				Type: kind, Op: OpRevise, Revision: previous.Revision,
				Carried: true, Retroactive: previous.Retroactive,
			})
		case mentioned:
			revision := previous.Revision + 1
			if proposed.Op == OpAdd {
				revision = 1
			}
			composed.Components = append(composed.Components, ComposedComponent{
				Type: kind, Op: proposed.Op, Revision: revision,
				Retroactive: previous.Retroactive || strings.TrimSpace(proposed.RetroactiveReason) != "",
			})
			composed.Children = append(composed.Children, ChildIntent{
				Component: kind, Op: proposed.Op, Revision: revision, ApprovalRef: approval,
			})
			if proposed.Op == OpCorrect {
				composed.CorrectionChain = append(composed.CorrectionChain,
					fmt.Sprintf("corrects:%s@%d because %s", kind, previous.Revision, proposed.RetroactiveReason))
			}
		}
	}
	composed.Digest = packageDigest(proposal, policy, composed)
	return composed, nil
}

// packageDigest binds one deterministic identity over policy, proposal
// and composed output.
func packageDigest(proposal PackageProposal, policy CompositionPolicy, composed ComposedPackage) string {
	parts := []string{"composition", policy.Version, proposal.PackageRef, proposal.EffectiveDate,
		proposal.SnapshotDigest, proposal.TransactionPlan, fmt.Sprintf("fence=%d", proposal.Fence)}
	approvals := append([]string(nil), proposal.Approvals...)
	sort.Strings(approvals)
	parts = append(parts, "approvals:"+strings.Join(approvals, ","))
	for _, component := range composed.Components {
		parts = append(parts, fmt.Sprintf("component:%s\x01%s\x01rev=%d\x01carried=%v",
			component.Type, component.Op, component.Revision, component.Carried))
	}
	parts = append(parts, "corrections:"+strings.Join(composed.CorrectionChain, ","))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
