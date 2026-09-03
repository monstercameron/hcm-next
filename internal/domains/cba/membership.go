package cba

import (
	"errors"
	"time"
)

// MembershipRevision is an immutable, sourced placement of a worker in a
// bargaining unit. Free-text placement is intentionally not supported.
type MembershipRevision struct {
	ID, MembershipID, Revision, WorkerID, UnitID string
	Source                                       string
	EffectiveFrom, EffectiveTo                   time.Time
	KnownFrom, KnownTo                           time.Time
	Retired                                      bool
}

type Membership = MembershipRevision

func (m MembershipRevision) Validate() error {
	if m.ID == "" || m.MembershipID == "" || m.Revision == "" || m.WorkerID == "" || m.UnitID == "" {
		return errors.New("cba: membership id, membership_id, worker_id, unit_id and revision are required")
	}
	if m.EffectiveFrom.IsZero() || (!m.EffectiveTo.IsZero() && !m.EffectiveTo.After(m.EffectiveFrom)) {
		return errors.New("cba: membership effective interval is invalid")
	}
	if m.KnownFrom.IsZero() || (!m.KnownTo.IsZero() && !m.KnownTo.After(m.KnownFrom)) {
		return errors.New("cba: membership known interval is invalid")
	}
	if m.Source == "" {
		return errors.New("cba: membership source is required")
	}
	return nil
}

func (m MembershipRevision) active(at, known time.Time) bool {
	return !at.Before(m.EffectiveFrom) && (m.EffectiveTo.IsZero() || at.Before(m.EffectiveTo)) &&
		!known.Before(m.KnownFrom) && (m.KnownTo.IsZero() || known.Before(m.KnownTo)) && !m.Retired
}
