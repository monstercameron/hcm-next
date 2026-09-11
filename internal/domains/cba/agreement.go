package cba

import (
	"errors"
	"fmt"
	"time"
)

// AgreementRevision is an immutable, effective-dated revision of an
// agreement. Effective intervals are half-open; a zero EffectiveTo is open.
type AgreementRevision struct {
	ID, AgreementID, Revision, Version string
	Title, Representative, Source      string
	EffectiveFrom, EffectiveTo         time.Time
	KnownFrom, KnownTo, KnownAt        time.Time
	Precedence                         int
	Retired                            bool
}

func (a AgreementRevision) Validate() error {
	if a.ID == "" || a.AgreementID == "" || a.Revision == "" {
		return errors.New("cba: agreement id, agreement_id and revision are required")
	}
	if a.EffectiveFrom.IsZero() || (!a.EffectiveTo.IsZero() && !a.EffectiveTo.After(a.EffectiveFrom)) {
		return errors.New("cba: agreement effective interval is invalid")
	}
	if a.KnownFrom.IsZero() && !a.KnownAt.IsZero() {
		a.KnownFrom = a.KnownAt
	}
	if a.KnownFrom.IsZero() || (!a.KnownTo.IsZero() && !a.KnownTo.After(a.KnownFrom)) {
		return errors.New("cba: agreement known interval is invalid")
	}
	if a.Representative == "" || a.Source == "" {
		return errors.New("cba: agreement representative and source are required")
	}
	return nil
}

func (a AgreementRevision) active(at, known time.Time) bool {
	if a.KnownFrom.IsZero() {
		a.KnownFrom = a.KnownAt
	}
	return !at.Before(a.EffectiveFrom) && (a.EffectiveTo.IsZero() || at.Before(a.EffectiveTo)) &&
		!known.Before(a.KnownFrom) && (a.KnownTo.IsZero() || known.Before(a.KnownTo)) && !a.Retired
}

func (a AgreementRevision) String() string { return fmt.Sprintf("%s@%s", a.AgreementID, a.Revision) }

// Agreement is retained as a concise contract name.
type Agreement = AgreementRevision
