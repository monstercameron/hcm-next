package shadow

import (
	"sort"
)

// State is the tenant state snapshot supplied to a shadow run. Values are
// opaque bytes; the view never exposes the caller's backing slices.
type State map[string][]byte

func NewState(values map[string][]byte) State {
	out := State{}
	for key, value := range values {
		out[key] = append([]byte(nil), value...)
	}
	return out
}

func (s State) Clone() State { return NewState(s) }

// View is a copy-on-write tenant view. Set and Delete affect only the shadow
// view and can never mutate the source State.
type View struct {
	base    State
	writes  State
	deleted map[string]bool
}

func NewView(base State) *View {
	return &View{base: base.Clone(), writes: State{}, deleted: map[string]bool{}}
}

func (v *View) Get(key string) ([]byte, bool) {
	if v == nil || v.deleted[key] {
		return nil, false
	}
	if value, ok := v.writes[key]; ok {
		return append([]byte(nil), value...), true
	}
	value, ok := v.base[key]
	return append([]byte(nil), value...), ok
}

func (v *View) Set(key string, value []byte) {
	if v == nil {
		return
	}
	delete(v.deleted, key)
	v.writes[key] = append([]byte(nil), value...)
}

func (v *View) Delete(key string) {
	if v == nil {
		return
	}
	delete(v.writes, key)
	v.deleted[key] = true
}

func (v *View) Snapshot() State {
	if v == nil {
		return nil
	}
	out := v.base.Clone()
	for key := range v.deleted {
		delete(out, key)
	}
	for key, value := range v.writes {
		out[key] = append([]byte(nil), value...)
	}
	return out
}

func stateKeys(s State) []string {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
