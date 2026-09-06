package issuerregistry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// StateEvent is one governed lifecycle transition, permanently recorded.
// [Publish], [Activate], [Suspend] and [Retire] each append exactly one;
// none of them ever edits or deletes an earlier event for the same
// (tenant, issuer) -- a later transition simply supersedes an earlier one
// by recency, and the earlier event's evidence remains in
// [Store.ListEvents]'s history. This is [StateEvent] evidence on every
// state change, the property AUTHN-001 requires by name.
type StateEvent struct {
	Tenant    values.TenantId
	IssuerURL string
	Revision  uint32

	From Status // empty for the very first event a (tenant, issuer) ever receives
	To   Status

	ActedBy   string
	Authority string
	Reason    string
	At        time.Time
}

// Evidence is what a caller presents to [Activate], [Suspend] and
// [Retire]. It mirrors internal/platform/configregistry.ActivationEvidence's
// shape.
type Evidence struct {
	// ActedBy names the principal performing the transition. Required.
	ActedBy string
	// Authority names the role or authority the principal acted under.
	// Optional context, not itself validated.
	Authority string
	// Reason is a free-text justification. Optional.
	Reason string
	// At is the moment of the transition. Required -- this package has no
	// clock of its own.
	At time.Time
}

func (e Evidence) validate() error {
	if strings.TrimSpace(e.ActedBy) == "" {
		return fmt.Errorf("%w: no acting principal presented", ErrMissingEvidence)
	}
	if e.At.IsZero() {
		return fmt.Errorf("%w: no transition time presented", ErrMissingEvidence)
	}
	return nil
}

// Store is the persistence port. [MemoryStore] is the in-memory adapter
// this package ships; [PGStore] is the PostgreSQL adapter over migration
// 00038's tables. See doc.go for why [MemoryStore] can answer the
// wrong-tenant question [PGStore] deliberately cannot.
type Store interface {
	// PutIssuer inserts rev as a new, immutable revision. Publish only
	// calls this once it has confirmed no revision already exists at
	// rev.Revision for (rev.Tenant, rev.IssuerURL); a genuine race between
	// two concurrent Publish calls at the same revision number is reported
	// as a conflict rather than swallowed.
	PutIssuer(rev Issuer) error
	// GetIssuer returns the exact revision named by ref, or found=false.
	GetIssuer(ref Ref) (Issuer, bool, error)
	// LatestRevision returns the highest revision number ever published for
	// (tenant, issuerURL), or found=false if none has been.
	LatestRevision(tenant values.TenantId, issuerURL string) (uint32, bool, error)

	// PutEvent appends evt. Like PutIssuer, this is an append -- no earlier
	// event for the group is ever edited or removed.
	PutEvent(evt StateEvent) error
	// LatestEvent returns the most recent lifecycle event for
	// (tenant, issuerURL), or found=false if the issuer has never been
	// published.
	LatestEvent(tenant values.TenantId, issuerURL string) (StateEvent, bool, error)
	// ListEvents returns every lifecycle event for (tenant, issuerURL),
	// oldest first -- the full, permanent transition history.
	ListEvents(tenant values.TenantId, issuerURL string) ([]StateEvent, error)
}

// Publish mints an immutable [Issuer] revision and records it in store,
// then appends the revision's first [StateEvent]: a transition into
// [StatusDraft], attributed to the publisher.
//
// It refuses an invalid issuer record (see [Issuer.validate], which is
// where the JWKS-not-pinned and algorithm-set refusals AUTHN-001 names
// live) and a revision number that is not strictly greater than the
// highest revision already published for (tenant, issuerURL).
//
// Publishing a new revision always starts that revision at DRAFT, even if
// an earlier revision for the same issuer was ACTIVE: new content is never
// live until a distinct approver activates it. The earlier revision's own
// history is untouched.
func Publish(store Store, issuer Issuer) (Issuer, error) {
	if store == nil {
		return Issuer{}, fmt.Errorf("%w: no store supplied", ErrInvalidIssuer)
	}
	if err := issuer.validate(); err != nil {
		return Issuer{}, err
	}

	current, found, err := store.LatestRevision(issuer.Tenant, issuer.IssuerURL)
	if err != nil {
		return Issuer{}, err
	}
	if found && issuer.Revision <= current {
		return Issuer{}, fmt.Errorf("%w: have %d, got %d", ErrRevisionConflict, current, issuer.Revision)
	}

	fromStatus := Status("")
	if found {
		latest, latestFound, err := store.LatestEvent(issuer.Tenant, issuer.IssuerURL)
		if err != nil {
			return Issuer{}, err
		}
		if latestFound {
			fromStatus = latest.To
		}
	}

	minted := issuer.clone()
	if err := store.PutIssuer(minted); err != nil {
		return Issuer{}, err
	}
	if err := store.PutEvent(StateEvent{
		Tenant: issuer.Tenant, IssuerURL: issuer.IssuerURL, Revision: issuer.Revision,
		From: fromStatus, To: StatusDraft,
		ActedBy: issuer.PublisherPrincipal, At: issuer.PublishedAt,
		Reason: "published",
	}); err != nil {
		return Issuer{}, err
	}
	return minted, nil
}

// transition is the shared implementation behind [Activate], [Suspend] and
// [Retire]: validate evidence, load the current state and the named
// revision, check the transition is permitted, and append the event.
func transition(store Store, ref Ref, evidence Evidence, to Status, requireDistinctApprover bool) (StateEvent, error) {
	if store == nil {
		return StateEvent{}, fmt.Errorf("%w: no store supplied", ErrInvalidIssuer)
	}
	if err := evidence.validate(); err != nil {
		return StateEvent{}, err
	}

	rev, found, err := store.GetIssuer(ref)
	if err != nil {
		return StateEvent{}, err
	}
	if !found {
		return StateEvent{}, fmt.Errorf("%w: revision %d of %q", ErrUnknownRevision, ref.Revision, ref.IssuerURL)
	}

	latest, latestFound, err := store.LatestEvent(ref.Tenant, ref.IssuerURL)
	if err != nil {
		return StateEvent{}, err
	}
	var from Status
	if latestFound {
		from = latest.To
	}
	if from.terminal() {
		return StateEvent{}, fmt.Errorf("%w: issuer %q is retired", ErrInvalidTransition, ref.IssuerURL)
	}

	// sameRevisionActive reports whether ref names the revision that is
	// already the group's active one -- distinct from "some revision of
	// this issuer is active", which is exactly what a rollback or
	// roll-forward Activate call to a *different* revision looks like.
	sameRevisionActive := from == StatusActive && latestFound && latest.Revision == ref.Revision

	switch to {
	case StatusActive:
		if sameRevisionActive {
			return StateEvent{}, fmt.Errorf("%w: revision %d is already active", ErrInvalidTransition, ref.Revision)
		}
		// DRAFT, SUSPENDED, or ACTIVE-on-a-different-revision (a
		// rollback/roll-forward to ref) may all activate ref. RETIRED
		// already returned above via the from.terminal() check.
	case StatusSuspended:
		if !sameRevisionActive {
			return StateEvent{}, fmt.Errorf("%w: cannot suspend from %q (or ref does not name the currently active revision)", ErrInvalidTransition, from)
		}
	case StatusRetired:
		// Any non-terminal state may be retired, regardless of revision.
	default:
		return StateEvent{}, fmt.Errorf("%w: unsupported target state %q", ErrInvalidTransition, to)
	}

	if requireDistinctApprover && evidence.ActedBy == rev.PublisherPrincipal {
		return StateEvent{}, fmt.Errorf("%w: %q published and attempted to activate revision %d", ErrSameApprover, evidence.ActedBy, ref.Revision)
	}

	evt := StateEvent{
		Tenant: ref.Tenant, IssuerURL: ref.IssuerURL, Revision: ref.Revision,
		From: from, To: to,
		ActedBy: evidence.ActedBy, Authority: evidence.Authority, Reason: evidence.Reason, At: evidence.At,
	}
	if err := store.PutEvent(evt); err != nil {
		return StateEvent{}, err
	}
	return evt, nil
}

// Activate transitions the revision named by ref into ACTIVE from DRAFT,
// SUSPENDED, or from ACTIVE on a *different* revision -- the last case is
// how a rollback or roll-forward works: activating an older or newer
// revision than the one currently active is permitted, and [Lookup]
// afterward resolves whichever revision the latest event names, not
// necessarily the highest-numbered one. It refuses re-activating the exact
// revision that is already active ([ErrInvalidTransition]), any transition
// out of RETIRED ([ErrInvalidTransition]), and evidence.ActedBy being the
// same principal who published the revision being activated
// ([ErrSameApprover]) -- the distinct-approver control AUTHN-001 requires.
func Activate(store Store, ref Ref, evidence Evidence) (StateEvent, error) {
	return transition(store, ref, evidence, StatusActive, true)
}

// Suspend transitions ref's issuer into SUSPENDED. ref must name the
// revision that is currently ACTIVE -- suspending a DRAFT, an already
// SUSPENDED issuer, or naming a revision other than the active one is
// refused ([ErrInvalidTransition]). A suspended issuer's assertions are
// refused by [Lookup] ([ErrIssuerSuspended]) until a later [Activate] call
// reinstates it.
func Suspend(store Store, ref Ref, evidence Evidence) (StateEvent, error) {
	return transition(store, ref, evidence, StatusSuspended, false)
}

// Retire transitions the revision named by ref into RETIRED, permanently.
// A retired issuer can never be activated or suspended again.
func Retire(store Store, ref Ref, evidence Evidence) (StateEvent, error) {
	return transition(store, ref, evidence, StatusRetired, false)
}

// crossTenantIndex is the optional capability [MemoryStore] provides so
// [Lookup] can distinguish an unknown issuer from one registered for a
// different tenant. See doc.go for why [PGStore] intentionally does not
// implement it.
type crossTenantIndex interface {
	tenantsForIssuer(issuerURL string) []values.TenantId
}

// Lookup returns the currently active [Issuer] for (tenant, issuerURL).
//
// It refuses:
//   - an issuer no tenant has ever published ([ErrUnknownIssuer]);
//   - an issuer published for a different tenant, when store can tell
//     ([ErrWrongTenant] -- see [crossTenantIndex]);
//   - an issuer whose latest lifecycle event is SUSPENDED ([ErrIssuerSuspended]),
//     RETIRED ([ErrIssuerRetired]), or still DRAFT ([ErrIssuerNotActive]).
//
// It never falls back to "latest published" or any other guess: only an
// issuer whose latest event is exactly ACTIVE is ever returned.
func Lookup(store Store, tenant values.TenantId, issuerURL string) (Issuer, error) {
	if store == nil {
		return Issuer{}, fmt.Errorf("%w: no store supplied", ErrInvalidIssuer)
	}
	latest, found, err := store.LatestEvent(tenant, issuerURL)
	if err != nil {
		return Issuer{}, err
	}
	if !found {
		if xt, ok := store.(crossTenantIndex); ok {
			for _, t := range xt.tenantsForIssuer(issuerURL) {
				if t != tenant {
					return Issuer{}, ErrWrongTenant
				}
			}
		}
		return Issuer{}, ErrUnknownIssuer
	}

	switch latest.To {
	case StatusActive:
		// fall through
	case StatusSuspended:
		return Issuer{}, ErrIssuerSuspended
	case StatusRetired:
		return Issuer{}, ErrIssuerRetired
	case StatusDraft:
		return Issuer{}, ErrIssuerNotActive
	default:
		return Issuer{}, fmt.Errorf("%w: unrecognized status %q", ErrInvalidIssuer, latest.To)
	}

	rev, found, err := store.GetIssuer(Ref{Tenant: tenant, IssuerURL: issuerURL, Revision: latest.Revision})
	if err != nil {
		return Issuer{}, err
	}
	if !found {
		return Issuer{}, fmt.Errorf("%w: active revision %d of %q is recorded but no longer published", ErrUnknownRevision, latest.Revision, issuerURL)
	}
	return rev.clone(), nil
}

// MemoryStore is the in-memory [Store] adapter. It is safe for concurrent
// use: every method takes the same mutex, matching internal/authn/federation.Registry's
// own concurrency shape (an update never leaves a reader observing a
// partial write).
type MemoryStore struct {
	mu       sync.RWMutex
	issuers  map[values.TenantId]map[string]map[uint32]Issuer
	events   map[values.TenantId]map[string][]StateEvent
	byIssuer map[string]map[values.TenantId]bool
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		issuers:  make(map[values.TenantId]map[string]map[uint32]Issuer),
		events:   make(map[values.TenantId]map[string][]StateEvent),
		byIssuer: make(map[string]map[values.TenantId]bool),
	}
}

var _ Store = (*MemoryStore)(nil)
var _ crossTenantIndex = (*MemoryStore)(nil)

func (m *MemoryStore) PutIssuer(rev Issuer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	byIssuer := m.issuers[rev.Tenant]
	if byIssuer == nil {
		byIssuer = make(map[string]map[uint32]Issuer)
		m.issuers[rev.Tenant] = byIssuer
	}
	byRevision := byIssuer[rev.IssuerURL]
	if byRevision == nil {
		byRevision = make(map[uint32]Issuer)
		byIssuer[rev.IssuerURL] = byRevision
	}
	if _, exists := byRevision[rev.Revision]; exists {
		return fmt.Errorf("%w: revision %d of %q already published", ErrRevisionConflict, rev.Revision, rev.IssuerURL)
	}
	byRevision[rev.Revision] = rev.clone()

	tenants := m.byIssuer[rev.IssuerURL]
	if tenants == nil {
		tenants = make(map[values.TenantId]bool)
		m.byIssuer[rev.IssuerURL] = tenants
	}
	tenants[rev.Tenant] = true
	return nil
}

func (m *MemoryStore) GetIssuer(ref Ref) (Issuer, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rev, ok := m.issuers[ref.Tenant][ref.IssuerURL][ref.Revision]
	if !ok {
		return Issuer{}, false, nil
	}
	return rev.clone(), true, nil
}

func (m *MemoryStore) LatestRevision(tenant values.TenantId, issuerURL string) (uint32, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byRevision := m.issuers[tenant][issuerURL]
	if len(byRevision) == 0 {
		return 0, false, nil
	}
	var max uint32
	for rev := range byRevision {
		if rev > max {
			max = rev
		}
	}
	return max, true, nil
}

func (m *MemoryStore) PutEvent(evt StateEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	byIssuer := m.events[evt.Tenant]
	if byIssuer == nil {
		byIssuer = make(map[string][]StateEvent)
		m.events[evt.Tenant] = byIssuer
	}
	byIssuer[evt.IssuerURL] = append(byIssuer[evt.IssuerURL], evt)
	return nil
}

func (m *MemoryStore) LatestEvent(tenant values.TenantId, issuerURL string) (StateEvent, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.events[tenant][issuerURL]
	if len(events) == 0 {
		return StateEvent{}, false, nil
	}
	return events[len(events)-1], true, nil
}

func (m *MemoryStore) ListEvents(tenant values.TenantId, issuerURL string) ([]StateEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	events := m.events[tenant][issuerURL]
	out := make([]StateEvent, len(events))
	copy(out, events)
	return out, nil
}

// tenantsForIssuer implements [crossTenantIndex].
func (m *MemoryStore) tenantsForIssuer(issuerURL string) []values.TenantId {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tenants := m.byIssuer[issuerURL]
	out := make([]values.TenantId, 0, len(tenants))
	for t := range tenants {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
