package migrate

import "time"

// Approval is the governance evidence WF-RUN-018 requires before [Migrate]
// may touch a paused instance: a named approver, distinct from whoever
// executes the migration, explicitly naming the exact [PreviewRecord.Digest]
// they reviewed. An approval naming any other digest -- including one from a
// preview that has since gone stale -- is not this approval, and [Migrate]
// refuses it as [CodeUnapprovedDigest].
type Approval struct {
	// PreviewDigest must equal the [PreviewRecord.Digest] of the preview
	// presented to [Migrate].
	PreviewDigest string
	// Approver is the principal who reviewed and authorized the migration. It
	// must differ from [Request.MigratedBy]: the approval is a two-person
	// action even when both principals are automated operators.
	Approver   string
	Reason     string
	ApprovedAt time.Time
}

func (a Approval) validate() error {
	switch {
	case a.PreviewDigest == "":
		return refuse(CodeInvalidRequest, "", "approval names no preview digest")
	case a.Approver == "":
		return refuse(CodeInvalidRequest, "", "approval names no approver")
	case a.Reason == "":
		return refuse(CodeInvalidRequest, "", "approval records no reason")
	case a.ApprovedAt.IsZero():
		return refuse(CodeInvalidRequest, "", "approval records no instant")
	default:
		return nil
	}
}
