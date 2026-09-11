package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// External effect states: provider acceptance alone never passes.
const (
	EffectPass    = "PASS"
	EffectUnknown = "UNKNOWN"
	EffectFailed  = "FAILED"
)

// ExternalEffect is one payroll, benefits or WFM effect with independent
// freshness, deadline, criticality and repair ownership.
type ExternalEffect struct {
	System           string
	Mandatory        bool
	FreshnessTick    int64
	DeadlineTick     int64
	State            string
	Observation      string
	ProviderAccepted bool
	Owner            string
}

// EffectDimensions is the honest rollup: Business stays LEAVE_ACTIVE
// while consistency and obligation states tell the truth. Valid leave is
// never rolled back by an unavailable observation.
type EffectDimensions struct {
	Business         string
	ConsistencyState string
	ObligationState  string
	Effects          []ExternalEffect
	RepairTargets    []string
	Digest           string
}

func dimensionsDigest(effects []ExternalEffect, business, consistency, obligation string) string {
	ordered := append([]ExternalEffect(nil), effects...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].System < ordered[j].System })
	parts := []string{"leave-effect-dimensions", business, consistency, obligation}
	for _, effect := range ordered {
		parts = append(parts, strings.Join([]string{effect.System, fmt.Sprint(effect.Mandatory), effect.State, effect.Observation, effect.Owner}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ReconcileEffects folds external observations without touching valid
// leave. Provider acceptance without an observation stays UNKNOWN;
// optional and mandatory effects never collapse into one verdict.
func ReconcileEffects(effects []ExternalEffect) (EffectDimensions, error) {
	if len(effects) == 0 {
		return EffectDimensions{}, fmt.Errorf("leave: reconciliation needs at least one effect")
	}
	dimensions := EffectDimensions{Business: BusinessLeaveActive}
	seen := make(map[string]bool, len(effects))
	for _, effect := range effects {
		if strings.TrimSpace(effect.System) == "" || seen[effect.System] {
			return EffectDimensions{}, fmt.Errorf("leave: effect systems must be unique and non-empty")
		}
		seen[effect.System] = true
		if strings.TrimSpace(effect.Owner) == "" {
			return EffectDimensions{}, fmt.Errorf("leave: effect %s needs a repair owner", effect.System)
		}
		switch effect.State {
		case EffectPass, EffectUnknown, EffectFailed:
		default:
			return EffectDimensions{}, fmt.Errorf("leave: effect %s state %q is not observed truth", effect.System, effect.State)
		}
		resolved := effect
		if effect.ProviderAccepted && strings.TrimSpace(effect.Observation) == "" {
			resolved.State = EffectUnknown
		}
		dimensions.Effects = append(dimensions.Effects, resolved)
	}
	sort.Slice(dimensions.Effects, func(i, j int) bool { return dimensions.Effects[i].System < dimensions.Effects[j].System })
	consistency := "OK"
	obligation := "SATISFIED"
	for _, effect := range dimensions.Effects {
		switch effect.State {
		case EffectFailed:
			consistency = "DEGRADED"
			dimensions.RepairTargets = append(dimensions.RepairTargets, effect.System)
			if effect.Mandatory {
				obligation = "PENDING"
			}
		case EffectUnknown:
			consistency = "DEGRADED"
			dimensions.RepairTargets = append(dimensions.RepairTargets, effect.System+":observe")
			if effect.Mandatory {
				obligation = "PENDING"
			}
		}
	}
	sort.Strings(dimensions.RepairTargets)
	dimensions.ConsistencyState = consistency
	dimensions.ObligationState = obligation
	dimensions.Digest = dimensionsDigest(dimensions.Effects, dimensions.Business, consistency, obligation)
	return dimensions, nil
}

// RepairPlan is the targeted repair for one effect system: it never
// reruns LeaveStarted or balance entries and never opens a second local
// leave transaction.
func (dimensions EffectDimensions) RepairPlan(system string) (string, error) {
	for _, effect := range dimensions.Effects {
		if effect.System != system {
			continue
		}
		if effect.State == EffectPass {
			return "", fmt.Errorf("leave: passing effect %s needs no repair", system)
		}
		return "repair:" + system + ":owner=" + effect.Owner, nil
	}
	return "", fmt.Errorf("leave: unknown effect %s", system)
}

// Verify recomputes the dimensions seal.
func (dimensions EffectDimensions) Verify() error {
	if dimensions.Digest == "" || dimensionsDigest(dimensions.Effects, dimensions.Business, dimensions.ConsistencyState, dimensions.ObligationState) != dimensions.Digest {
		return fmt.Errorf("leave: effect dimensions seal is broken")
	}
	if dimensions.Business != BusinessLeaveActive {
		return fmt.Errorf("leave: reconciliation rolled back valid leave")
	}
	return nil
}
