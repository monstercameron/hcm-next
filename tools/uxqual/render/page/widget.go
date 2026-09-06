package page

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// WidgetContext is everything a registered [Widget] constructor is given to
// build its slot's component tree: which page and region it is mounted
// into, and the slot itself. It never carries fetched business data or a
// live RPC connection -- a PageDefinition names data bindings and actions
// by RPC reference only (tools/uxqual/pagedef.DataBinding, ActionRef), and
// resolving one of those into a real value is a later layer's job, not
// this renderer's.
type WidgetContext struct {
	// PageID and PageVersion identify the page the widget is being mounted
	// into (pagedef.PageDefinition.PageID / Version).
	PageID      string
	PageVersion int
	// Region is the region the slot belongs to, exactly as the
	// PageDefinition declared it (including its own Bindings and Actions,
	// which a widget may read to know what it is permitted to ask for).
	Region pagedef.Region
	// Slot is the widget slot being mounted.
	Slot pagedef.WidgetSlot
}

// Widget is a registered widget's constructor.
//
// It must be a pure function of ctx: called twice with an equal ctx it must
// return trees that render to identical bytes (see this package's doc
// comment on determinism). A nil return is treated as a rendering fault by
// [Render] -- a widget that legitimately has nothing to show must still
// return an empty, semantically valid node (e.g. a labelled empty list),
// never nil, so "no content" and "constructor is broken" are never the same
// signal.
type Widget func(ctx WidgetContext) ui.Node

// ErrDuplicateWidget is the sentinel [Registry.Register] wraps when a ref is
// registered twice.
var ErrDuplicateWidget = errors.New("page: widget ref already registered")

// ErrUnregisteredWidget is the sentinel every [*UnregisteredWidgetError]
// wraps, so a caller can test for the class of failure with errors.Is
// without matching on the message.
var ErrUnregisteredWidget = errors.New("page: unregistered widget ref")

// UnregisteredWidgetError is returned by [Render] when a PageDefinition
// names a widget ref no [Registry] entry resolves. It names the page,
// region, and slot so the message points at the exact offending reference
// rather than a bare "not found", and it is typed (rather than a bare
// fmt.Errorf) so a caller can distinguish "this ref is not registered" from
// every other reason Render can fail.
type UnregisteredWidgetError struct {
	PageID    string
	RegionID  string
	SlotID    string
	WidgetRef string
}

func (e *UnregisteredWidgetError) Error() string {
	return fmt.Sprintf("page: page %q region %q slot %q references unregistered widget %q",
		e.PageID, e.RegionID, e.SlotID, e.WidgetRef)
}

// Unwrap lets errors.Is(err, ErrUnregisteredWidget) succeed for any
// *UnregisteredWidgetError.
func (e *UnregisteredWidgetError) Unwrap() error { return ErrUnregisteredWidget }

// Registry is the in-memory widget registry port [Render] resolves widget
// refs against: widget ref -> GWC component constructor. It is the only
// thing this package trusts to say what a widget ref may resolve to; there
// is no fallback that renders a widget ref this registry does not know
// about.
//
// A zero-value Registry is usable: NewRegistry is a convenience for the
// common "many refs at once" construction case, not a requirement.
type Registry struct {
	widgets map[string]Widget
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{widgets: make(map[string]Widget)}
}

// Register adds ctor under ref. It refuses a blank ref, a nil constructor,
// and a ref already registered (a page-composition-time authoring mistake,
// not something a later caller should silently overwrite).
func (r *Registry) Register(ref string, ctor Widget) error {
	if r == nil {
		return fmt.Errorf("page: cannot register %q on a nil Registry", ref)
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("page: widget ref must not be empty")
	}
	if ctor == nil {
		return fmt.Errorf("page: widget %q constructor must not be nil", ref)
	}
	if r.widgets == nil {
		r.widgets = make(map[string]Widget)
	}
	if _, exists := r.widgets[ref]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateWidget, ref)
	}
	r.widgets[ref] = ctor
	return nil
}

// Lookup returns the constructor registered under ref, and whether one is.
func (r *Registry) Lookup(ref string) (Widget, bool) {
	if r == nil {
		return nil, false
	}
	ctor, ok := r.widgets[ref]
	return ctor, ok
}

// Refs returns every registered widget ref, sorted, so a caller (or a test)
// can enumerate the registry's contents without depending on Go's
// randomized map order.
func (r *Registry) Refs() []string {
	if r == nil {
		return nil
	}
	refs := make([]string, 0, len(r.widgets))
	for ref := range r.widgets {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}
