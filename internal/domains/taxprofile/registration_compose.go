package taxprofile

import (
	"errors"
	"fmt"
	"sort"
)

// ErrDuplicateRegistration reports two registrations from one port that share
// a registration reference and revision.
var ErrDuplicateRegistration = errors.New("taxprofile: duplicate registration from port")

// ComposeRegistrations reads every registration the port reports, validates
// each one, refuses duplicates, and returns them ordered by jurisdiction,
// registration reference and revision so a profile snapshot composed from an
// external authority is deterministic. It is the in-process consumer of
// [RegistrationPort].
func ComposeRegistrations(port RegistrationPort) ([]TaxRegistrationRevision, error) {
	if port == nil {
		return nil, errors.New("taxprofile: registration port is required")
	}
	values := port.Registrations()
	out := make([]TaxRegistrationRevision, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, registration := range values {
		if err := registration.Validate(); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s\x00%d", registration.RegistrationIDRef, registration.Revision)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: %s revision %d", ErrDuplicateRegistration, registration.RegistrationIDRef, registration.Revision)
		}
		seen[key] = struct{}{}
		out = append(out, registration)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Jurisdiction != out[j].Jurisdiction {
			return out[i].Jurisdiction < out[j].Jurisdiction
		}
		if out[i].RegistrationIDRef != out[j].RegistrationIDRef {
			return out[i].RegistrationIDRef < out[j].RegistrationIDRef
		}
		return out[i].Revision < out[j].Revision
	})
	return out, nil
}
