package wait

import "encoding/json"

// JSON renders a TimerRequirement as indented, deterministic JSON with its
// digest attached, mirroring internal/workflow/frontier.Transition.JSON: two
// identical computations render identical bytes, which is what makes a
// checked-in golden a contract rather than a snapshot.
func (r TimerRequirement) JSON() ([]byte, error) {
	view := requirementDigestView{
		WorkflowID:      r.WorkflowID,
		WorkflowVersion: r.WorkflowVersion,
		NodeID:          r.NodeID,
		FireAt:          r.FireAtText(),
		Zone:            r.Reference.Zone.String(),
		Calendar:        r.Reference.Calendar.String(),
		Policy:          r.Reference.Policy.String(),
		DatasetTzdb:     r.Dataset.TzdbVersion,
		DatasetCal:      r.Dataset.CalendarVersion,
		ReviewRequired:  r.ReviewRequired,
		ReviewReason:    r.ReviewReason,
		Evidence:        r.Evidence,
	}
	type rendered struct {
		requirementDigestView
		Digest string `json:"digest"`
	}
	b, err := json.MarshalIndent(rendered{requirementDigestView: view, Digest: r.Digest}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
