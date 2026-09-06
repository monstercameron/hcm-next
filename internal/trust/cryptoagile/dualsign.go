package cryptoagile

import (
	"fmt"
	"time"
)

// DualSigner emits one [Envelope] per suite the migration plan declares
// live at signing time: always the current window's active suite and, when
// the window declares one, also its dual suite - dual-sign, so a message
// stays verifiable under the suite readers have not yet cut over to.
type DualSigner struct {
	plan MigrationPlan
	keys SignerPort
	now  func() time.Time
}

// NewDualSigner validates plan and returns a signer that consults it (and
// now, defaulting to time.Now) on every SignAll call.
func NewDualSigner(plan MigrationPlan, keys SignerPort, now func() time.Time) (*DualSigner, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if keys == nil {
		return nil, fmt.Errorf("cryptoagile: dual signer needs a non-nil SignerPort")
	}
	if now == nil {
		now = time.Now
	}
	return &DualSigner{plan: plan, keys: keys, now: now}, nil
}

// ErrNoCurrentWindow reports that the signer's clock reads a time before
// the migration plan's first window starts.
var ErrNoCurrentWindow = fmt.Errorf("cryptoagile: no migration window covers the current time")

// SignAll signs message under the current window's active suite and, when
// declared, its dual suite, returning one Envelope per suite actually used
// in a stable order: active first, dual second (when present).
func (d *DualSigner) SignAll(message []byte) ([]Envelope, error) {
	win, _, ok := d.plan.WindowAt(d.now())
	if !ok {
		return nil, ErrNoCurrentWindow
	}
	signer := NewEnvelopeSigner(d.keys)
	active, err := signer.Sign(win.ActiveSuiteID, message)
	if err != nil {
		return nil, err
	}
	envs := []Envelope{active}
	if win.DualSuiteID != "" {
		dual, err := signer.Sign(win.DualSuiteID, message)
		if err != nil {
			return nil, err
		}
		envs = append(envs, dual)
	}
	return envs, nil
}
