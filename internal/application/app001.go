// Package application owns the immutable PartnerApplication/version registry.
// It deliberately has no persistence, transport, intent, or provider imports.
package application

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

const RejectionCode = "APP_001_REJECTED"

var ErrRejected = errors.New(RejectionCode)

type Rejection struct{ Code, Field, State, Version string }

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", e.Code, e.Field, e.State, e.Version)
}
func (e *Rejection) Unwrap() error { return ErrRejected }
func IsRejected(err error) bool    { return errors.Is(err, ErrRejected) }

type State string

const (
	StateDraft     State = "DRAFT"
	StateReviewed  State = "REVIEWED"
	StatePublished State = "PUBLISHED"
	StateRetired   State = "RETIRED"
)

type Publisher struct{ ID, Name, Contact string }
type SoftwareIdentity struct{ Name, Vendor, Package, Identifier string }
type Callback struct{ Name, URL, Method string }
type SBOM struct{ Format, Digest, URI string }
type Provenance struct{ Builder, Source, Digest, Signature string }
type SupportPlan struct{ Owner, Contact, SLA string }
type ExitPlan struct{ Export, Revocation, Destruction string }
type Compatibility struct{ API, Schema, Runtime, Policy string }

// PartnerApplication is the stable identity and governed declaration shared by
// all versions. Once registered, it is never replaced.
type PartnerApplication struct {
	ID                    string
	Publisher             Publisher
	Software              SoftwareIdentity
	RequestedCapabilities []string
	RequestedData         []string
	Callbacks             []Callback
	Regions               []string
	SBOM                  SBOM
	Provenance            Provenance
	Support               SupportPlan
	Exit                  ExitPlan
	Compatibility         Compatibility
}

// ApplicationVersion is an immutable, reviewed release of an application.
type ApplicationVersion struct {
	ApplicationID         string
	Version               string
	State                 State
	Reviewed              bool
	Mutable               bool
	Publisher             Publisher
	Software              SoftwareIdentity
	RequestedCapabilities []string
	RequestedData         []string
	Callbacks             []Callback
	Regions               []string
	SBOM                  SBOM
	Provenance            Provenance
	Support               SupportPlan
	Exit                  ExitPlan
	Compatibility         Compatibility
}

type Event struct{ ApplicationID, Version, Kind string }
type EventSink interface{ Append(Event) error }

type Registry struct {
	mu       sync.RWMutex
	apps     map[string]PartnerApplication
	versions map[string]map[string]ApplicationVersion
	sink     EventSink
}

func NewRegistry(sinks ...EventSink) *Registry {
	var s EventSink
	if len(sinks) > 0 {
		s = sinks[0]
	}
	return &Registry{apps: map[string]PartnerApplication{}, versions: map[string]map[string]ApplicationVersion{}, sink: s}
}

func (r *Registry) Register(app PartnerApplication) error {
	if app.ID == "" {
		return reject("application.id", "missing", "")
	}
	if app.Publisher.ID == "" || app.Publisher.Name == "" {
		return reject("publisher", "missing", "")
	}
	if app.Software.Identifier == "" && app.Software.Name == "" {
		return reject("software.identity", "missing", "")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.apps[app.ID]; ok {
		return fmt.Errorf("application %q already registered", app.ID)
	}
	r.apps[app.ID] = cloneApplication(app)
	r.versions[app.ID] = map[string]ApplicationVersion{}
	return nil
}

// Publish validates completely before mutating registry or event sinks.
func (r *Registry) Publish(v ApplicationVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	app, ok := r.apps[v.ApplicationID]
	if !ok {
		return reject("application.id", "unknown", v.ApplicationID)
	}
	if v.Version == "" {
		return reject("version", "missing", "")
	}
	if v.State != StateReviewed && v.State != StatePublished {
		return reject("state", string(v.State), v.Version)
	}
	if v.Mutable {
		return reject("mutable", "true", v.Version)
	}
	if !v.Reviewed {
		return reject("reviewed", "false", v.Version)
	}
	mergeApplication(&app, &v)
	if err := validateMerged(app, v); err != nil {
		return err
	}
	if _, exists := r.versions[v.ApplicationID][v.Version]; exists {
		return fmt.Errorf("application version %s/%s already registered", v.ApplicationID, v.Version)
	}
	v.State = StatePublished
	v = cloneVersion(v)
	if r.sink != nil {
		if err := r.sink.Append(Event{ApplicationID: v.ApplicationID, Version: v.Version, Kind: "APPLICATION_VERSION_PUBLISHED"}); err != nil {
			return err
		}
	}
	r.versions[v.ApplicationID][v.Version] = v
	return nil
}

func (r *Registry) Lookup(applicationID, version string) (ApplicationVersion, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.versions[applicationID][version]
	if !ok {
		return ApplicationVersion{}, false
	}
	return cloneVersion(v), true
}
func (r *Registry) Versions(applicationID string) []ApplicationVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ApplicationVersion, 0, len(r.versions[applicationID]))
	for _, v := range r.versions[applicationID] {
		out = append(out, cloneVersion(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

func validateMerged(a PartnerApplication, v ApplicationVersion) error {
	mergeApplication(&a, &v)
	checks := []struct {
		field, state string
		missing      bool
	}{{"publisher", "missing", v.Publisher.ID == "" || v.Publisher.Name == ""}, {"software.identity", "missing", v.Software.Identifier == "" && v.Software.Name == ""}, {"requested_capabilities", "missing", len(v.RequestedCapabilities) == 0}, {"requested_data", "missing", len(v.RequestedData) == 0}, {"callbacks", "missing", len(v.Callbacks) == 0}, {"regions", "missing", len(v.Regions) == 0}, {"sbom", "missing", v.SBOM.Digest == ""}, {"provenance", "missing", v.Provenance.Digest == ""}, {"support", "missing", v.Support.Owner == "" || v.Support.Contact == ""}, {"exit", "missing", v.Exit.Export == "" || v.Exit.Revocation == ""}, {"compatibility", "missing", v.Compatibility.API == ""}}
	for _, c := range checks {
		if c.missing {
			return reject(c.field, c.state, v.Version)
		}
	}
	return nil
}

func mergeApplication(a *PartnerApplication, v *ApplicationVersion) {
	if v.Publisher.ID == "" {
		v.Publisher = a.Publisher
	}
	if v.Software.Identifier == "" && v.Software.Name == "" {
		v.Software = a.Software
	}
	if len(v.RequestedCapabilities) == 0 {
		v.RequestedCapabilities = a.RequestedCapabilities
	}
	if len(v.RequestedData) == 0 {
		v.RequestedData = a.RequestedData
	}
	if len(v.Callbacks) == 0 {
		v.Callbacks = a.Callbacks
	}
	if len(v.Regions) == 0 {
		v.Regions = a.Regions
	}
	if v.SBOM.Digest == "" {
		v.SBOM = a.SBOM
	}
	if v.Provenance.Digest == "" {
		v.Provenance = a.Provenance
	}
	if v.Support.Owner == "" {
		v.Support = a.Support
	}
	if v.Exit.Export == "" {
		v.Exit = a.Exit
	}
	if v.Compatibility.API == "" {
		v.Compatibility = a.Compatibility
	}
}
func reject(field, state, version string) error {
	return &Rejection{Code: RejectionCode, Field: field, State: state, Version: version}
}
func cloneApplication(a PartnerApplication) PartnerApplication {
	a.RequestedCapabilities = append([]string(nil), a.RequestedCapabilities...)
	a.RequestedData = append([]string(nil), a.RequestedData...)
	a.Regions = append([]string(nil), a.Regions...)
	a.Callbacks = append([]Callback(nil), a.Callbacks...)
	return a
}
func cloneVersion(v ApplicationVersion) ApplicationVersion {
	v.RequestedCapabilities = append([]string(nil), v.RequestedCapabilities...)
	v.RequestedData = append([]string(nil), v.RequestedData...)
	v.Regions = append([]string(nil), v.Regions...)
	v.Callbacks = append([]Callback(nil), v.Callbacks...)
	return v
}
