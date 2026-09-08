package compensate

import "errors"

var (
	ErrInvalidRequest        = errors.New("compensate: invalid request")
	ErrAuthorizationRequired = errors.New("compensate: authorization is required")
	ErrIdempotencyRequired   = errors.New("compensate: idempotency key is required")
	ErrVerificationRequired  = errors.New("compensate: verification observation is required")
	ErrIrreversible          = errors.New("compensate: irreversible effect requires repair handling")
	ErrCapabilityFailed      = errors.New("compensate: corrective capability failed")
	ErrObservationFailed     = errors.New("compensate: verification observation failed")
)
