// Package clock owns the governed registry of physical time devices and
// logical time sources. It is a conformance contract: registration validates
// policy facts but does not write authority, emit events, enqueue work, or
// contact a provider.
package clock

import (
	"strconv"
	"strings"
)

const RegistryVersion = "v1"

type State string

const (
	Active  State = "ACTIVE"
	Revoked State = "REVOKED"
)

// Registration contains the controls required before a source may produce
// authoritative time observations. All references are explicit; no omitted
// field gets a permissive default.
type Registration struct {
	ID                string
	Version           string
	State             State
	OwnerRef          string
	LocationRef       string
	ClockTrustPolicy  string
	OfflinePolicy     string
	ReplayPolicy      string
	SignaturePolicy   string
	FirmwarePolicy    string
	CertificatePolicy string
	RetentionPolicy   string
}

// TimeDevice is the physical capture endpoint registration.
type TimeDevice = Registration

// TimeSource is the logical source identity used by a device or time
// authority. Keeping it a distinct name makes source/device references
// explicit to callers while preserving one governed contract.
type TimeSource = Registration

func (r Registration) Validate() error {
	state := r.State
	if state == "" {
		state = Active
	}
	if r.Version == "" {
		return reject("version", string(state), r.Version, "is required")
	}
	if r.ID == "" {
		return reject("id", string(state), r.Version, "is required")
	}
	if state != Active && state != Revoked {
		return reject("state", string(state), r.Version, "is not declared")
	}
	fields := []struct{ name, value string }{
		{"owner_ref", r.OwnerRef}, {"location_ref", r.LocationRef},
		{"clock_trust_policy", r.ClockTrustPolicy}, {"offline_policy", r.OfflinePolicy},
		{"replay_policy", r.ReplayPolicy}, {"signature_policy", r.SignaturePolicy},
		{"firmware_policy", r.FirmwarePolicy}, {"certificate_policy", r.CertificatePolicy},
		{"retention_policy", r.RetentionPolicy},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			return reject(f.name, string(state), r.Version, "is required")
		}
	}
	return nil
}

// Registry is an immutable, validated snapshot. NewRegistry copies input
// slices and rejects duplicate IDs, so callers cannot mutate its authority
// after construction.
type Registry struct {
	Version string
	Devices []TimeDevice
	Sources []TimeSource
}

func NewRegistry(devices []TimeDevice, sources []TimeSource) (Registry, error) {
	seen := make(map[string]struct{}, len(devices)+len(sources))
	for i, d := range devices {
		if err := d.Validate(); err != nil {
			return Registry{}, err
		}
		if _, ok := seen[d.ID]; ok {
			return Registry{}, reject("devices["+itoa(i)+"].id", string(d.State), d.Version, "duplicates another registration")
		}
		seen[d.ID] = struct{}{}
	}
	for i, s := range sources {
		if err := s.Validate(); err != nil {
			return Registry{}, err
		}
		if _, ok := seen[s.ID]; ok {
			return Registry{}, reject("sources["+itoa(i)+"].id", string(s.State), s.Version, "duplicates another registration")
		}
		seen[s.ID] = struct{}{}
	}
	return Registry{Version: RegistryVersion, Devices: append([]TimeDevice(nil), devices...), Sources: append([]TimeSource(nil), sources...)}, nil
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
