// Package productui resolves the execution-status
// dimensions model. The status surface announces five
// lifecycle dimensions with one accessible label; this
// resolution is its governed content: every dimension
// through the surface's own token mapping, absent
// presences through the presence tokens, invalid enums
// failing closed, and the same joined accessible label.
// An unavailable projection resolves to the surface's
// unavailable copy with no items. Projections are never
// mutated.
package productui

import "strings"

// StatusDimensionsModel is the resolved dimensions
// content: availability, the unavailable copy when not,
// the five dimensions in surface order, and the joined
// accessible label.
type StatusDimensionsModel struct {
	Available       bool
	Unavailable     string
	Items           []statusDimension
	AccessibleLabel string
}

// ResolveStatusDimensions resolves the dimensions model
// for one status projection.
func ResolveStatusDimensions(locale LocaleContext, projection StatusProjection) StatusDimensionsModel {
	if !projection.Available {
		return StatusDimensionsModel{Unavailable: locale.Text("status.projection_unavailable")}
	}
	groupLabel := locale.Text("status.group")
	items := [5]statusDimension{
		statusItem(locale, "request", locale.Text("status.request_label"), projection.Request, requestToken),
		statusItem(locale, "execution", locale.Text("status.execution_label"), projection.Execution, executionToken),
		statusItem(locale, "business", locale.Text("status.business_label"), projection.Business, businessToken),
		statusItem(locale, "consistency", locale.Text("status.consistency_label"), projection.Consistency, consistencyToken),
		statusItem(locale, "obligation", locale.Text("status.obligation_label"), projection.Obligation, obligationToken),
	}
	accessible := make([]string, 1, len(items)+1)
	accessible[0] = groupLabel
	for _, item := range items {
		accessible = append(accessible, item.label+": "+item.value)
	}
	return StatusDimensionsModel{
		Available:       true,
		Items:           items[:],
		AccessibleLabel: strings.Join(accessible, "; "),
	}
}
