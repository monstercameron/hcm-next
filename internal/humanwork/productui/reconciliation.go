// Package productui presents the reconciliation and
// repair posture. The rollup reads the resolved
// consistency and execution dimension keys: trouble keys
// and invalid keys mean attention, a repairing
// consistency means repair in progress, a consistent one
// means healthy, and everything else idles quietly. The
// attention detail names the troubling dimensions from
// their resolved labels and values. The rollup presents
// posture only; repair itself stays service
// responsibility.
package productui

import "strings"

// ReconciliationState is the presented reconcile-and-
// repair posture. The zero value idles quietly so a
// missing rollup never cries for attention.
type ReconciliationState int

const (
	// ReconciliationIdle marks nothing to reconcile.
	ReconciliationIdle ReconciliationState = iota
	// ReconciliationHealthy marks a consistent projection.
	ReconciliationHealthy
	// ReconciliationRepairing marks repair in progress.
	ReconciliationRepairing
	// ReconciliationAttention marks trouble or unreadable keys.
	ReconciliationAttention
)

// ReconciliationStatus is the presented posture: its
// state with headline copy and, for attention, the
// troubling dimensions.
type ReconciliationStatus struct {
	State  ReconciliationState
	Text   string
	Detail string
}

// ResolveReconciliation resolves the posture from one
// resolved consistency dimension and one resolved
// execution dimension. Dimensions are never mutated.
func ResolveReconciliation(locale LocaleContext, consistency, execution statusDimension) ReconciliationStatus {
	troubled := reconciliationTrouble(consistency, execution)
	if len(troubled) > 0 {
		parts := make([]string, 0, len(troubled))
		for _, dimension := range troubled {
			parts = append(parts, dimension.label+": "+dimension.value)
		}
		return ReconciliationStatus{State: ReconciliationAttention, Text: locale.Text("status.reconciliation.attention"), Detail: strings.Join(parts, "; ")}
	}
	switch consistency.valueKey {
	case "repairing":
		return ReconciliationStatus{State: ReconciliationRepairing, Text: locale.Text("status.reconciliation.repairing")}
	case "consistent":
		return ReconciliationStatus{State: ReconciliationHealthy, Text: locale.Text("status.reconciliation.healthy")}
	}
	return ReconciliationStatus{State: ReconciliationIdle, Text: locale.Text("status.reconciliation.idle")}
}

// reconciliationTrouble collects the dimensions whose
// keys mean trouble: degraded, blocked, repair-required,
// or unreadable.
func reconciliationTrouble(dimensions ...statusDimension) []statusDimension {
	troubled := []statusDimension{}
	for _, dimension := range dimensions {
		switch dimension.valueKey {
		case "degraded", "blocked", "repair_required", "invalid":
			troubled = append(troubled, dimension)
		}
	}
	return troubled
}
