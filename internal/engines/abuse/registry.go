package abuse

import (
	"errors"
	"fmt"
	"sync"
)

// RegistryRejectionCode is the stable wire-level code for a refused
// DetectorVersion publication into a Registry.
const RegistryRejectionCode = "ABUSE_001_DETECTOR_VERSION_REJECTED"

// ErrRegistryRejected is matched by every typed Registry publication
// rejection.
var ErrRegistryRejected = errors.New(RegistryRejectionCode)

// RegistryRejection names the offending field, state, detector id and
// semver of a refused DetectorVersion publication.
type RegistryRejection struct {
	Code, Field, State, DetectorID, Semver string
	Cause                                  error
}

func (r *RegistryRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s detector=%s semver=%s", r.Code, r.Field, r.State, r.DetectorID, r.Semver)
}

func (r *RegistryRejection) Unwrap() []error {
	if r.Cause == nil {
		return []error{ErrRegistryRejected}
	}
	return []error{ErrRegistryRejected, r.Cause}
}

// IsRegistryRejected reports whether err is a Registry publication
// rejection.
func IsRegistryRejected(err error) bool { return errors.Is(err, ErrRegistryRejected) }

func classifyDetectorVersion(err error) (field, state string) {
	switch {
	case errors.Is(err, ErrDetectorVersionIdentity):
		return "identity", "MISSING_OR_INVALID"
	case errors.Is(err, ErrDetectorVersionSemver):
		return "semver", "MALFORMED"
	case errors.Is(err, ErrDetectorVersionInputs):
		return "declared_inputs", "MISSING"
	case errors.Is(err, ErrDetectorVersionInputKind):
		return "declared_inputs", "UNGOVERNED_KIND"
	case errors.Is(err, ErrDetectorVersionOutputs):
		return "declared_outputs", "MISSING_OR_INVALID"
	case errors.Is(err, ErrDetectorVersionThreshold):
		return "thresholds", "UNREFERENCED"
	case errors.Is(err, ErrDetectorVersionActivation):
		return "activated_at", "MISSING"
	default:
		return "detector_version", "INVALID"
	}
}

type registryEntry struct {
	version DetectorVersion
	digest  string
}

// Registry holds published DetectorVersions for one or more detectors.
// Publication is idempotent by (detector id, semver): republishing an
// identical body is a no-op that returns the existing receipt, while
// republishing a *changed* body under the same (detector id, semver) is
// refused -- a published DetectorVersion is immutable. Exactly one version
// is ever active per detector id: the most recently published version for
// a detector id supersedes whichever version was previously active, and
// Active reports only that one version.
type Registry struct {
	mu       sync.RWMutex
	versions map[string]map[string]registryEntry // detectorID -> semver -> entry
	active   map[string]string                   // detectorID -> active semver
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		versions: map[string]map[string]registryEntry{},
		active:   map[string]string{},
	}
}

// Publish validates v and records it. Republishing the exact same body
// under a (detector id, semver) already on file is idempotent (returns the
// stored version, no error); republishing a *different* body under that
// same (detector id, semver) is refused with a RegistryRejection, since a
// published DetectorVersion is immutable. On success (first publication or
// idempotent republish), v becomes the sole active version for its
// detector id.
func (reg *Registry) Publish(v DetectorVersion) (DetectorVersion, error) {
	if err := v.Validate(); err != nil {
		field, state := classifyDetectorVersion(err)
		return DetectorVersion{}, &RegistryRejection{Code: RegistryRejectionCode, Field: field, State: state, DetectorID: v.DetectorID, Semver: v.Semver, Cause: err}
	}
	d, err := v.Digest()
	if err != nil {
		return DetectorVersion{}, err
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	byID, ok := reg.versions[v.DetectorID]
	if !ok {
		byID = map[string]registryEntry{}
		reg.versions[v.DetectorID] = byID
	}
	if existing, ok := byID[v.Semver]; ok {
		if existing.digest != d {
			return DetectorVersion{}, &RegistryRejection{Code: RegistryRejectionCode, Field: "body", State: "IMMUTABLE_VERSION_CHANGED", DetectorID: v.DetectorID, Semver: v.Semver}
		}
		reg.active[v.DetectorID] = v.Semver
		return existing.version, nil
	}
	byID[v.Semver] = registryEntry{version: v, digest: d}
	reg.active[v.DetectorID] = v.Semver
	return v, nil
}

// Active returns the sole currently active DetectorVersion for detectorID,
// and whether one exists.
func (reg *Registry) Active(detectorID string) (DetectorVersion, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	sv, ok := reg.active[detectorID]
	if !ok {
		return DetectorVersion{}, false
	}
	return reg.versions[detectorID][sv].version, true
}

// Get returns the DetectorVersion published under (detectorID, semver),
// active or not, for audit/Explain purposes.
func (reg *Registry) Get(detectorID, semver string) (DetectorVersion, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	byID, ok := reg.versions[detectorID]
	if !ok {
		return DetectorVersion{}, false
	}
	e, ok := byID[semver]
	return e.version, ok
}
