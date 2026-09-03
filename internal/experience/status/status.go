// Package status defines the tenant-safe service health and incident advisory
// presentation contract. It contains no transport or persistence behavior.
package status

import (
	"errors"
	"sort"
	"strings"
	"time"
)

type Authorization string

const (
	AuthorizationUnknown Authorization = "UNKNOWN"
	AuthorizationCurrent Authorization = "CURRENT"
	AuthorizationDenied  Authorization = "DENIED"
	AuthorizationExpired Authorization = "EXPIRED"
)

type Freshness string

const (
	FreshnessUnknown Freshness = "UNKNOWN"
	FreshnessCurrent Freshness = "CURRENT"
	FreshnessStale   Freshness = "STALE"
)

type ServiceState string

const (
	Unknown     ServiceState = "UNKNOWN"
	Operational ServiceState = "OPERATIONAL"
	Degraded    ServiceState = "DEGRADED"
	Outage      ServiceState = "OUTAGE"
	Maintenance ServiceState = "MAINTENANCE"
)

// Descriptive aliases keep the contract convenient without creating a second vocabulary.
const (
	ServiceUnknown     = Unknown
	ServiceOperational = Operational
	ServiceDegraded    = Degraded
	ServiceOutage      = Outage
	ServiceMaintenance = Maintenance
)

type Severity string

const (
	Info     Severity = "INFO"
	Warning  Severity = "WARNING"
	Critical Severity = "CRITICAL"
)
const (
	SeverityInfo     = Info
	SeverityWarning  = Warning
	SeverityCritical = Critical
)

type RegistryEntry struct {
	Code        string
	Label       string
	Description string
}

var registry = [...]RegistryEntry{
	{string(Unknown), "Unknown", "Service status has not been established."},
	{string(Operational), "Operational", "Service is operating normally."},
	{string(Degraded), "Degraded", "Service is available with reduced performance or capability."},
	{string(Outage), "Outage", "Service is unavailable or materially impaired."},
	{string(Maintenance), "Maintenance", "Service is undergoing planned maintenance."},
}

func RegistryMatrix() []RegistryEntry { return append([]RegistryEntry(nil), registry[:]...) }
func Lookup(code string) (RegistryEntry, bool) {
	for _, e := range registry {
		if e.Code == code {
			return e, true
		}
	}
	return RegistryEntry{}, false
}

type Service struct {
	ID, Name, TenantID string
	State              ServiceState
	UpdatedAt          time.Time
}
type Incident struct {
	ID, TenantID, ServiceID string
	Severity                Severity
	State                   ServiceState
	Title, Summary          string
	StartedAt, UpdatedAt    time.Time
	ResolvedAt              *time.Time
}
type Advisory struct {
	ID, TenantID, IncidentID, ServiceID string
	Severity                            Severity
	Title, Message                      string
	CreatedAt, UpdatedAt                time.Time
	ExpiresAt                           *time.Time
}

type Snapshot struct {
	TenantID   string
	ObservedAt time.Time
	Freshness  Freshness
	Services   []Service
	Incidents  []Incident
	Advisories []Advisory
}

func (s Snapshot) Clone() Snapshot {
	s.Services = append([]Service(nil), s.Services...)
	s.Incidents = append([]Incident(nil), s.Incidents...)
	s.Advisories = append([]Advisory(nil), s.Advisories...)
	return s
}

var (
	ErrTenantRequired = errors.New("status: tenant is required")
	ErrUnauthorized   = errors.New("status: unauthorized")
	ErrInvalid        = errors.New("status: invalid record")
)

type Request struct {
	TenantID      string
	Authorization Authorization
	Now           time.Time
	MaxAge        time.Duration
}

// Present filters all tenant-bearing records before sorting. Ties use stable
// IDs, making the presentation deterministic even when timestamps coincide.
func Present(req Request, services []Service, incidents []Incident, advisories []Advisory) (Snapshot, error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return Snapshot{}, ErrTenantRequired
	}
	if req.Authorization != AuthorizationCurrent {
		return Snapshot{}, ErrUnauthorized
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	if req.MaxAge <= 0 {
		req.MaxAge = 5 * time.Minute
	}
	o := Snapshot{TenantID: req.TenantID, ObservedAt: req.Now, Freshness: FreshnessCurrent}
	for _, v := range services {
		if v.ID == "" || v.TenantID == "" || !validState(v.State) {
			return Snapshot{}, ErrInvalid
		}
		if v.TenantID == req.TenantID {
			o.Services = append(o.Services, v)
		}
	}
	for _, v := range incidents {
		if v.ID == "" || v.TenantID == "" {
			return Snapshot{}, ErrInvalid
		}
		if v.TenantID == req.TenantID {
			o.Incidents = append(o.Incidents, v)
		}
	}
	for _, v := range advisories {
		if v.ID == "" || v.TenantID == "" {
			return Snapshot{}, ErrInvalid
		}
		if v.TenantID == req.TenantID {
			o.Advisories = append(o.Advisories, v)
		}
	}
	for _, v := range o.Services {
		if stale(req.Now, v.UpdatedAt, req.MaxAge) {
			o.Freshness = FreshnessStale
		}
	}
	for _, v := range o.Incidents {
		if stale(req.Now, v.UpdatedAt, req.MaxAge) {
			o.Freshness = FreshnessStale
		}
	}
	for _, v := range o.Advisories {
		if stale(req.Now, v.UpdatedAt, req.MaxAge) || (v.ExpiresAt != nil && !req.Now.Before(*v.ExpiresAt)) {
			o.Freshness = FreshnessStale
		}
	}
	sort.SliceStable(o.Services, func(i, j int) bool { return o.Services[i].ID < o.Services[j].ID })
	sort.SliceStable(o.Incidents, func(i, j int) bool {
		if o.Incidents[i].UpdatedAt.Equal(o.Incidents[j].UpdatedAt) {
			return o.Incidents[i].ID < o.Incidents[j].ID
		}
		return o.Incidents[i].UpdatedAt.After(o.Incidents[j].UpdatedAt)
	})
	sort.SliceStable(o.Advisories, func(i, j int) bool {
		if o.Advisories[i].UpdatedAt.Equal(o.Advisories[j].UpdatedAt) {
			return o.Advisories[i].ID < o.Advisories[j].ID
		}
		return o.Advisories[i].UpdatedAt.After(o.Advisories[j].UpdatedAt)
	})
	return o, nil
}
func stale(now, updated time.Time, max time.Duration) bool {
	return !updated.IsZero() && now.Sub(updated) > max
}
func validState(v ServiceState) bool {
	return v == Unknown || v == Operational || v == Degraded || v == Outage || v == Maintenance
}
