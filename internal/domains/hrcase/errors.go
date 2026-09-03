package hrcase

import "errors"

var (
	ErrInvalidDefinition = errors.New("hrcase: invalid case definition")
	ErrInvalidRequest    = errors.New("hrcase: invalid HR request")
	ErrInvalidRevision   = errors.New("hrcase: invalid case revision")
	ErrInvalidTransition = errors.New("hrcase: invalid case transition")
	ErrTerminalCase      = errors.New("hrcase: terminal case cannot be edited")
)
