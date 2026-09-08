// Package crm owns governed talent-pool and pool-membership semantics.
package crm

import "errors"

var (
	ErrInvalidPool       = errors.New("crm: invalid talent pool")
	ErrInvalidMembership = errors.New("crm: invalid talent pool membership")
	ErrInvalidReference  = errors.New("crm: invalid reference")
	ErrInvalidProspect   = errors.New("crm: invalid prospect")
)
