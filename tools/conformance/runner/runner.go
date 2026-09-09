// Package runner defines the seam between CONF-001's document-level
// conformance analysis and a future compiled workflow engine. Runner is a
// port: DocumentRunner is the only implementation until internal/workflow
// exists, and it evaluates a parsed reference-workflow document rather
// than executing anything. When the compiled engine is available, a second
// Runner implementation can execute the same workflows in real SIMULATE
// (and later REPLAY) mode against it, without CONF-001's report shape
// changing.
package runner

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/checks"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

// Clock supplies the current instant. Production SIMULATE runs use a fixed
// clock so the resulting report is byte-identical across runs; a future
// REPLAY runner would use a clock pinned to the historical instant it is
// replaying.
type Clock interface {
	Now() time.Time
}

// FixedClock always returns the same instant.
type FixedClock struct {
	At time.Time
}

// Now implements Clock.
func (f FixedClock) Now() time.Time { return f.At }

// DefaultFixedClock is the deterministic instant CONF-001's P1A depth uses
// for SIMULATE mode. See planning/todos.md CONF-001's 2026-09-02
// disposition: "P1A for CONF-001 at the depth the no-effect Promotion
// fixture needs (simulate mode, fixed clock, zero-effect receipt)."
var DefaultFixedClock = FixedClock{At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}

// Effect would describe one mutation or external call a real engine
// performed. DocumentRunner never populates it: SIMULATE mode over a
// reference-workflow document reads and checks the document, and performs
// no mutation and no external effect, matching workflow-runtime.md's
// "Simulation suppresses all mutations and external effects."
type Effect struct {
	Kind   string
	Target string
	Detail string
}

// Receipt is the outcome of one Simulate call: the zero-effect SIMULATE
// receipt required by CONF-001's P1A disposition. Effects is always empty
// for DocumentRunner; its presence as a typed, always-empty field makes
// that guarantee inspectable in the report rather than merely asserted in
// a comment.
type Receipt struct {
	WorkflowID    string
	ExecutionMode string
	ClockAt       time.Time
	Effects       []Effect
	Checks        []checks.Result
}

// Runner is the port a compiled workflow engine can implement later.
// DocumentRunner is CONF-001's own implementation; it works on the parsed
// reference-workflow document as data rather than on a live workflow
// instance, because no compiled engine exists yet to execute one.
type Runner interface {
	Simulate(wf *model.Workflow, clock Clock) (Receipt, error)
}

// DocumentRunner evaluates a parsed reference-workflow document against
// the kernel vocabulary and context contract via the checks package. It
// never returns an error: every failure mode a document can exhibit is
// expressed as a FAIL or UNKNOWN check result instead, so a batch of many
// documents can always produce a complete report.
type DocumentRunner struct {
	Vocab *vocab.Vocabulary
}

// Simulate runs every registered check against wf and returns a
// zero-effect Receipt stamped with clock's instant.
func (r DocumentRunner) Simulate(wf *model.Workflow, clock Clock) (Receipt, error) {
	return Receipt{
		WorkflowID:    wf.ID,
		ExecutionMode: "SIMULATE",
		ClockAt:       clock.Now(),
		Effects:       nil,
		Checks:        checks.Evaluate(wf, r.Vocab),
	}, nil
}
