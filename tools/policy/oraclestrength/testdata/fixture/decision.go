package fixture

import "errors"

var (
	ErrDenied = errors.New("DENIED")
	ErrStale  = errors.New("STALE_VERSION")
)

type Input struct {
	Allowed         bool
	CurrentVersion  int
	ExpectedVersion int
}

func Decide(input Input) error {
	if input.Allowed == false {
		return ErrDenied
	}
	if input.CurrentVersion != input.ExpectedVersion {
		return ErrStale
	}
	return nil
}
