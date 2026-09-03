package capability

import (
	"sort"
	"sync"
)

// entry is the registry's own storage for one published capability version.
// It is never handed out directly; every accessor returns a clone.
type entry struct {
	def     Definition
	handler Handler
	status  Status
	digest  string
}

// Registry publishes CapabilityDefinition versions. Once a (ID, Version) key
// is registered it can never be replaced: Register on an existing key always
// fails, so a historical version can be trusted to mean today what it meant
// when it was published.
//
// Registry is safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	entries map[Key]entry
}

// NewRegistry returns an empty registry. Production code normally starts
// from NewBootstrapRegistry; NewRegistry is for tests that need a registry
// with a controlled, minimal set of definitions.
func NewRegistry() *Registry {
	return &Registry{entries: map[Key]entry{}}
}

// Register publishes one capability definition bound to its implementation
// handler. It fails when the definition is missing a required field
// (validate), when handler is nil (the implementation binding does not
// resolve), or when the (ID, Version) key is already published.
func (r *Registry) Register(def Definition, handler Handler) error {
	if err := validate(def); err != nil {
		return err
	}
	if handler == nil {
		return ErrImplementationUnbound{ID: def.ID, Version: def.Version}
	}
	digestValue, err := Digest(def)
	if err != nil {
		return err
	}

	key := def.Key()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[key]; ok {
		return ErrAlreadyRegistered{ID: def.ID, Version: def.Version}
	}
	r.entries[key] = entry{def: def.clone(), handler: handler, status: StatusActive, digest: digestValue}
	return nil
}

// Lookup resolves one exact capability version. A deprecated version still
// resolves (CAP-001 GREEN: "historical versions resolve after deprecation");
// only an unregistered key fails.
func (r *Registry) Lookup(key Key) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[key]
	if !ok {
		return Record{}, false
	}
	return Record{Definition: e.def.clone(), Status: e.status, Digest: e.digest}, true
}

// handlerFor returns the bound handler for a key, or ok=false if the key is
// unknown. It is unexported: only the gateway in this package invokes a
// handler directly.
func (r *Registry) handlerFor(key Key) (Handler, Status, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[key]
	if !ok {
		return nil, "", false
	}
	return e.handler, e.status, true
}

// Deprecate marks a published version deprecated. It remains resolvable by
// Lookup and invocable by the gateway; deprecation is a recommendation to
// stop adopting it, not a removal.
func (r *Registry) Deprecate(key Key) error {
	return r.setStatus(key, StatusDeprecated)
}

// Retire marks a published version retired. It remains resolvable by Lookup
// (history is never erased) but the gateway refuses to invoke it.
func (r *Registry) Retire(key Key) error {
	return r.setStatus(key, StatusRetired)
}

func (r *Registry) setStatus(key Key, status Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok {
		return ErrDefinitionInvalid{ID: key.ID, Version: key.Version, Field: "Key", Reason: "is not registered"}
	}
	e.status = status
	r.entries[key] = e
	return nil
}

// List returns every registered record in deterministic order: ascending by
// ID, then by Version. The same registry state always lists the same way,
// regardless of registration order.
func (r *Registry) List() []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, Record{Definition: e.def.clone(), Status: e.status, Digest: e.digest})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Definition.ID != out[j].Definition.ID {
			return out[i].Definition.ID < out[j].Definition.ID
		}
		return out[i].Definition.Version < out[j].Definition.Version
	})
	return out
}

// Versions returns every published version of one capability ID, ascending.
func (r *Registry) Versions(id string) []Record {
	all := r.List()
	out := make([]Record, 0, len(all))
	for _, rec := range all {
		if rec.Definition.ID == id {
			out = append(out, rec)
		}
	}
	return out
}

// LatestActive returns the highest-versioned ACTIVE record for a capability
// ID. It is what "the current version" means; a deprecated or retired
// version is still Lookup-able by exact key but is never "latest".
func (r *Registry) LatestActive(id string) (Record, bool) {
	versions := r.Versions(id)
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].Status == StatusActive {
			return versions[i], true
		}
	}
	return Record{}, false
}
