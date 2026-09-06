// Package outbound implements TRUST-017: controlled outbound destination
// trust. A [Policy] is a closed allowlist of [Destination] values, each
// pinning a trust bundle reference and the exact purposes and data
// classifications that destination is cleared to receive. [Policy.Check]
// refuses everything the allowlist does not explicitly clear: an
// unlisted destination, an allowlisted destination used for a purpose or a
// data classification it was not cleared for, and - when the caller presents
// one - an [internal/trust/lease.CredentialLease] minted for a different
// destination than the one dispatch is actually targeting.
//
// This package deliberately does not resolve DNS, open a TLS connection, or
// evaluate a redirect: those are the egress gateway's job downstream of this
// decision (see TRUST-018's DLP inspection and egress receipts). What this
// package answers is the policy question a gateway asks first - "is this
// destination, for this purpose and this data classification, allowed at
// all" - so that answer stays declarative, testable in isolation, and never
// widened by a network-layer bug.
package outbound

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/trust/lease"
)

// Errors. All are matchable with errors.Is.
var (
	// ErrInvalidPolicy is returned when a [Policy] cannot be built: a
	// destination missing its name, pinned trust bundle reference, or with an
	// empty purpose or data-class list (an empty list is not a wildcard - it
	// clears the destination for nothing).
	ErrInvalidPolicy = errors.New("outbound: invalid outbound destination trust policy")
	// ErrInvalidRequest is returned when a [CheckRequest] is missing a
	// destination, purpose, or data classification.
	ErrInvalidRequest = errors.New("outbound: invalid outbound destination check request")
	// ErrNotAllowlisted is returned when the requested destination has no
	// entry in the policy at all.
	ErrNotAllowlisted = errors.New("outbound: destination is not allowlisted")
	// ErrPurposeNotCleared is returned when an allowlisted destination does
	// not clear the requested purpose.
	ErrPurposeNotCleared = errors.New("outbound: destination is not cleared for this purpose")
	// ErrDataClassNotCleared is returned when an allowlisted destination does
	// not clear the requested data classification.
	ErrDataClassNotCleared = errors.New("outbound: destination is not cleared for this data classification")
	// ErrLeaseDestinationMismatch is returned when the presented credential
	// lease was minted for a destination other than the one being checked.
	ErrLeaseDestinationMismatch = errors.New("outbound: presented credential lease was minted for a different destination")
)

// Destination is one allowlisted outbound destination: a pinned trust bundle
// reference plus the purposes and data classifications it is cleared for.
// An empty Purposes or DataClasses list clears the destination for nothing,
// the same closed-world convention [internal/trust/secrets.AccessGrant]
// uses for its own dimensions.
type Destination struct {
	Name           string
	TrustBundleRef string
	Purposes       []string
	DataClasses    []string
}

func (d Destination) validate() error {
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Name) != d.Name {
		return fmt.Errorf("%w: destination has no name", ErrInvalidPolicy)
	}
	if strings.TrimSpace(d.TrustBundleRef) == "" {
		return fmt.Errorf("%w: destination %q has no pinned trust bundle reference", ErrInvalidPolicy, d.Name)
	}
	if len(d.Purposes) == 0 {
		return fmt.Errorf("%w: destination %q clears no purpose", ErrInvalidPolicy, d.Name)
	}
	if len(d.DataClasses) == 0 {
		return fmt.Errorf("%w: destination %q clears no data classification", ErrInvalidPolicy, d.Name)
	}
	return nil
}

func (d Destination) allowsPurpose(p string) bool {
	for _, x := range d.Purposes {
		if x == p {
			return true
		}
	}
	return false
}

func (d Destination) allowsDataClass(c string) bool {
	for _, x := range d.DataClasses {
		if x == c {
			return true
		}
	}
	return false
}

// Policy is the immutable allowlist [Policy.Check] evaluates against.
// Nothing outside [NewPolicy] can add a destination once built, and Check
// never consults anything but the destinations it was constructed with.
type Policy struct {
	destinations map[string]Destination
}

// NewPolicy validates and freezes destinations into a policy. A duplicate
// destination name is refused rather than silently taking the last one: two
// conflicting entries for the same destination is a configuration mistake,
// not a policy to resolve by ordering.
func NewPolicy(destinations ...Destination) (*Policy, error) {
	set := make(map[string]Destination, len(destinations))
	for _, d := range destinations {
		if err := d.validate(); err != nil {
			return nil, err
		}
		if _, dup := set[d.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate destination %q", ErrInvalidPolicy, d.Name)
		}
		cp := d
		cp.Purposes = append([]string(nil), d.Purposes...)
		cp.DataClasses = append([]string(nil), d.DataClasses...)
		set[d.Name] = cp
	}
	return &Policy{destinations: set}, nil
}

// CheckRequest is what [Policy.Check] evaluates: the destination an outbound
// dispatch targets, its purpose and data classification, and - when the
// dispatch is credentialed - the [lease.CredentialLease] presented for it.
type CheckRequest struct {
	Destination string
	Purpose     string
	DataClass   string
	// Lease, when non-nil, is the credential lease the caller intends to
	// present for this dispatch. Check refuses the request outright when
	// Lease.Destination does not equal Destination: a lease minted for one
	// destination is never honoured for dispatch to another, regardless of
	// what the allowlist itself would otherwise permit.
	Lease *lease.CredentialLease
}

func (r CheckRequest) validate() error {
	for _, item := range []struct{ name, value string }{
		{"destination", r.Destination}, {"purpose", r.Purpose}, {"data classification", r.DataClass},
	} {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, item.name)
		}
	}
	return nil
}

// Decision is the outcome of one [Policy.Check] call.
type Decision struct {
	Destination    string
	TrustBundleRef string
	Purpose        string
	DataClass      string
	Allowed        bool
}

// Explain renders a deterministic, redaction-safe one-line summary.
func (d Decision) Explain() string {
	return fmt.Sprintf("outbound decision destination=%s purpose=%s data_class=%s allowed=%t",
		d.Destination, d.Purpose, d.DataClass, d.Allowed)
}

// Check evaluates req against p and returns the allow decision, or refuses
// with a specific error. Checks run in a fixed order - lease-destination
// binding, then allowlist membership, then purpose, then data
// classification - so the first-fired reason is deterministic. Check never
// returns an Allowed Decision alongside a non-nil error: a refusal always
// means Allowed is false.
func (p *Policy) Check(req CheckRequest) (Decision, error) {
	if p == nil {
		return Decision{}, fmt.Errorf("%w: no policy", ErrInvalidPolicy)
	}
	if err := req.validate(); err != nil {
		return Decision{}, err
	}
	if req.Lease != nil && req.Lease.Destination != req.Destination {
		return Decision{Destination: req.Destination, Purpose: req.Purpose, DataClass: req.DataClass},
			fmt.Errorf("%w: lease minted for %q, dispatch targets %q", ErrLeaseDestinationMismatch, req.Lease.Destination, req.Destination)
	}

	d, ok := p.destinations[req.Destination]
	if !ok {
		return Decision{Destination: req.Destination, Purpose: req.Purpose, DataClass: req.DataClass},
			fmt.Errorf("%w: %q", ErrNotAllowlisted, req.Destination)
	}
	if !d.allowsPurpose(req.Purpose) {
		return Decision{Destination: d.Name, TrustBundleRef: d.TrustBundleRef, Purpose: req.Purpose, DataClass: req.DataClass},
			fmt.Errorf("%w: %q not cleared for purpose %q", ErrPurposeNotCleared, d.Name, req.Purpose)
	}
	if !d.allowsDataClass(req.DataClass) {
		return Decision{Destination: d.Name, TrustBundleRef: d.TrustBundleRef, Purpose: req.Purpose, DataClass: req.DataClass},
			fmt.Errorf("%w: %q not cleared for data class %q", ErrDataClassNotCleared, d.Name, req.DataClass)
	}

	return Decision{Destination: d.Name, TrustBundleRef: d.TrustBundleRef, Purpose: req.Purpose, DataClass: req.DataClass, Allowed: true}, nil
}
