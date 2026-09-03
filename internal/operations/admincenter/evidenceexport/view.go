package evidenceexport

// View is the bounded operator-facing representation; it contains progress
// and recovery affordances but never raw records.
type View struct {
	OperationID                 string
	Status                      Status
	Completed, Total, NextChunk int
	Failure                     string
	ManifestDigest              string
	Manifest                    Manifest
	CanResume, CanRetry         bool
}

func BuildView(o Operation) View {
	return View{OperationID: o.ID, Status: o.Status, Completed: o.Completed, Total: o.Total, NextChunk: o.NextChunk, Failure: o.Failure, ManifestDigest: o.Manifest.ManifestDigest, Manifest: o.Manifest, CanResume: o.Status == StatusPending || o.Status == StatusRunning || o.Status == StatusFailed, CanRetry: o.Status == StatusFailed}
}

type Command string

const (
	CommandStart  Command = "start"
	CommandResume Command = "resume"
	CommandRetry  Command = "retry"
	CommandVerify Command = "verify"
)

func (v View) Allows(c Command) bool {
	switch c {
	case CommandResume:
		return v.CanResume
	case CommandRetry:
		return v.CanRetry
	case CommandStart, CommandVerify:
		return true
	}
	return false
}
