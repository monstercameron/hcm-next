package cba

import (
	"errors"
	"time"
)

// BargainingUnitRevision is an immutable description of a represented unit.
type BargainingUnitRevision struct {
	ID, UnitID, Revision, AgreementID string
	Name, Representative, Source      string
	EffectiveFrom, EffectiveTo        time.Time
	KnownFrom, KnownTo                time.Time
	Retired                           bool
}

type BargainingUnit = BargainingUnitRevision

func (u BargainingUnitRevision) Validate() error {
	if u.ID == "" || u.UnitID == "" || u.Revision == "" || u.AgreementID == "" {
		return errors.New("cba: unit id, unit_id, agreement_id and revision are required")
	}
	if u.EffectiveFrom.IsZero() || (!u.EffectiveTo.IsZero() && !u.EffectiveTo.After(u.EffectiveFrom)) {
		return errors.New("cba: unit effective interval is invalid")
	}
	if u.KnownFrom.IsZero() || (!u.KnownTo.IsZero() && !u.KnownTo.After(u.KnownFrom)) {
		return errors.New("cba: unit known interval is invalid")
	}
	if u.Representative == "" || u.Source == "" {
		return errors.New("cba: unit representative and source are required")
	}
	return nil
}

func (u BargainingUnitRevision) active(at, known time.Time) bool {
	return !at.Before(u.EffectiveFrom) && (u.EffectiveTo.IsZero() || at.Before(u.EffectiveTo)) &&
		!known.Before(u.KnownFrom) && (u.KnownTo.IsZero() || known.Before(u.KnownTo)) && !u.Retired
}
