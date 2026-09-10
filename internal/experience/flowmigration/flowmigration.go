// Package flowmigration governs evolution of participant-facing user flows.
//
// A published definition is an immutable artifact.  Running work is pinned
// to that artifact unless a reviewed, explicit mapping authorizes migration.
// Deep links and action requests therefore fail closed when they are stale;
// they never reinterpret an old action against a new definition.
package flowmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalidDefinition = errors.New("flowmigration: invalid definition")
	ErrUnknownVersion    = errors.New("flowmigration: unknown version")
	ErrStaleLink         = errors.New("flowmigration: stale link")
	ErrReviewRequired    = errors.New("flowmigration: reviewed migration required")
	ErrActionUnavailable = errors.New("flowmigration: action unavailable")
	ErrDuplicateAction   = errors.New("flowmigration: duplicate action")
)

type Action struct{ ID, Semantic string }

// FlowVersion and FlowAction are descriptive aliases for callers that model
// the artifact as a version rather than a definition.
type FlowVersion = Definition
type FlowAction = Action

// Definition is a published flow contract. Digest is content identity and
// must never be supplied or changed by callers.
type Definition struct {
	FlowID  string   `json:"flow_id"`
	Version uint32   `json:"version"`
	States  []string `json:"states"`
	Actions []Action `json:"actions"`
	Digest  string   `json:"digest"`
}

func (d Definition) clone() Definition {
	d.States = append([]string(nil), d.States...)
	d.Actions = append([]Action(nil), d.Actions...)
	return d
}

func digestDefinition(d Definition) string {
	type content struct {
		FlowID  string   `json:"flow_id"`
		Version uint32   `json:"version"`
		States  []string `json:"states"`
		Actions []Action `json:"actions"`
	}
	b, _ := json.Marshal(content{d.FlowID, d.Version, d.States, d.Actions})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validate(d Definition) error {
	if d.FlowID == "" || d.Version == 0 {
		return ErrInvalidDefinition
	}
	seen := map[string]bool{}
	for _, a := range d.Actions {
		if a.ID == "" || a.Semantic == "" {
			return ErrInvalidDefinition
		}
		if seen[a.ID] {
			return fmt.Errorf("%w: %s", ErrDuplicateAction, a.ID)
		}
		seen[a.ID] = true
	}
	return nil
}

type Review struct {
	Reviewer   string
	ReviewedAt time.Time
	// Mapping maps each action in the source to the semantically equivalent
	// action in the successor. Unmapped actions are intentionally not callable.
	Mapping map[string]string
	Replan  bool
}

func (r Review) clone() Review { r.Mapping = cloneMap(r.Mapping); return r }
func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	n := make(map[string]string, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}

type Registry struct {
	mu       sync.RWMutex
	versions map[string]Definition
	active   map[string]string
	reviews  map[string]Review
}

// Manager is an alias retained for application code that calls this boundary
// a migration manager.
type Manager = Registry

func NewRegistry() *Registry {
	return &Registry{versions: map[string]Definition{}, active: map[string]string{}, reviews: map[string]Review{}}
}

func NewManager() *Manager                                          { return NewRegistry() }
func (r *Registry) PublishVersion(d Definition) (Definition, error) { return r.Publish(d) }

// Publish computes identity and stores a copy. Re-publishing identical
// content is idempotent; the same flow/version cannot have two contents.
func (r *Registry) Publish(d Definition) (Definition, error) {
	if err := validate(d); err != nil {
		return Definition{}, err
	}
	d.Digest = digestDefinition(d)
	r.mu.Lock()
	defer r.mu.Unlock()
	key := d.FlowID + fmt.Sprintf("/%d", d.Version)
	if old, ok := r.versions[key]; ok {
		if old.Digest != d.Digest {
			return Definition{}, fmt.Errorf("%w: version already published", ErrInvalidDefinition)
		}
		return old.clone(), nil
	}
	r.versions[key] = d.clone()
	return d.clone(), nil
}

func (r *Registry) Definition(flowID, digest string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.versions[flowID+"/"+digest]
	if ok {
		return d.clone(), true
	}
	for _, v := range r.versions {
		if v.FlowID == flowID && v.Digest == digest {
			return v.clone(), true
		}
	}
	return Definition{}, false
}

// Activate pins new starts. It does not alter running work. A successor must
// carry a review mapping when replacing an existing active version.
func (r *Registry) Activate(flowID, digest string, review Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var target Definition
	var ok bool
	for _, d := range r.versions {
		if d.FlowID == flowID && d.Digest == digest {
			target = d
			ok = true
			break
		}
	}
	if !ok {
		return ErrUnknownVersion
	}
	if prior := r.active[flowID]; prior != "" && prior != digest {
		if review.Reviewer == "" || review.ReviewedAt.IsZero() || !review.Replan {
			return ErrReviewRequired
		}
		r.reviews[prior+"->"+digest] = review.clone()
	}
	_ = target
	r.active[flowID] = digest
	return nil
}
func (r *Registry) Active(flowID string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dig := r.active[flowID]
	for _, d := range r.versions {
		if d.FlowID == flowID && d.Digest == dig {
			return d.clone(), true
		}
	}
	return Definition{}, false
}

type Link struct{ FlowID, VersionDigest, Stage, ActionID string }
type LinkResult struct {
	Mode       string
	Definition Definition
	Link       Link
	ReadOnly   bool
	Replan     bool
	Reason     string
}

const (
	LinkCurrent  = "CURRENT"
	LinkReadOnly = "READ_ONLY"
	LinkReplan   = "REPLAN"
)

// ResolveLink never executes an action. A link to an inactive version is
// safely inspectable, and explicitly requests replan only when a reviewed
// successor mapping exists.
func (r *Registry) ResolveLink(l Link) (LinkResult, error) {
	d, ok := r.Definition(l.FlowID, l.VersionDigest)
	if !ok {
		return LinkResult{}, ErrUnknownVersion
	}
	active, _ := r.Active(l.FlowID)
	if active.Digest == d.Digest {
		return LinkResult{Mode: LinkCurrent, Definition: d, Link: l}, nil
	}
	r.mu.RLock()
	rev, reviewed := r.reviews[d.Digest+"->"+active.Digest]
	r.mu.RUnlock()
	if reviewed && rev.Replan {
		return LinkResult{Mode: LinkReplan, Definition: d, Link: l, Replan: true, Reason: "reviewed successor requires replan"}, nil
	}
	return LinkResult{Mode: LinkReadOnly, Definition: d, Link: l, ReadOnly: true, Reason: "version is not active"}, nil
}

type Work struct {
	ID, FlowID, VersionDigest string
	Draft                     map[string]string
	TaskIDs, ApprovalIDs      []string
	Completed                 map[string]bool
}

func (w Work) clone() Work {
	w.Draft = cloneMap(w.Draft)
	w.TaskIDs = append([]string(nil), w.TaskIDs...)
	w.ApprovalIDs = append([]string(nil), w.ApprovalIDs...)
	w.Completed = cloneMapBool(w.Completed)
	return w
}
func cloneMapBool(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	n := map[string]bool{}
	for k, v := range m {
		n[k] = v
	}
	return n
}

type MigrationReceipt struct {
	WorkID, FromDigest, ToDigest                       string
	Migrated                                           bool
	Replanned                                          bool
	PreservedDraft, PreservedTasks, PreservedApprovals bool
	ActionMap                                          map[string]string
	ReceiptDigest                                      string
}

// Migrate returns a new pinned work value; the source is never rewritten.
func (r *Registry) Migrate(w Work, targetDigest string, review Review) (Work, MigrationReceipt, error) {
	from, ok := r.Definition(w.FlowID, w.VersionDigest)
	if !ok {
		return Work{}, MigrationReceipt{}, ErrUnknownVersion
	}
	to, ok := r.Definition(w.FlowID, targetDigest)
	if !ok {
		return Work{}, MigrationReceipt{}, ErrUnknownVersion
	}
	if from.Digest == to.Digest {
		return w.clone(), receipt(w, to, false, false, nil), nil
	}
	if review.Reviewer == "" || review.ReviewedAt.IsZero() || !review.Replan {
		return Work{}, MigrationReceipt{}, ErrReviewRequired
	}
	valid := map[string]bool{}
	for _, a := range to.Actions {
		valid[a.ID] = true
	}
	mapping := map[string]string{}
	for _, a := range from.Actions {
		if dst := review.Mapping[a.ID]; dst != "" {
			if !valid[dst] {
				return Work{}, MigrationReceipt{}, ErrActionUnavailable
			}
			var target Action
			for _, candidate := range to.Actions {
				if candidate.ID == dst {
					target = candidate
					break
				}
			}
			if target.Semantic != a.Semantic {
				return Work{}, MigrationReceipt{}, fmt.Errorf("%w: semantic contract changed for %s", ErrReviewRequired, a.ID)
			}
			mapping[a.ID] = dst
		}
	}
	n := w.clone()
	n.VersionDigest = to.Digest
	rec := receipt(w, to, true, true, mapping)
	return n, rec, nil
}
func receipt(w Work, to Definition, migrated, replanned bool, m map[string]string) MigrationReceipt {
	rec := MigrationReceipt{WorkID: w.ID, FromDigest: w.VersionDigest, ToDigest: to.Digest, Migrated: migrated, Replanned: replanned, PreservedDraft: true, PreservedTasks: true, PreservedApprovals: true, ActionMap: cloneMap(m)}
	b, _ := json.Marshal(rec)
	h := sha256.Sum256(b)
	rec.ReceiptDigest = hex.EncodeToString(h[:])
	return rec
}

type ActionReceipt struct {
	WorkID, ActionID, ActionDigest string
	Duplicate                      bool
}

func (r *Registry) Execute(w Work, link Link, idempotencyKey string, seen map[string]ActionReceipt) (ActionReceipt, error) {
	if idempotencyKey == "" {
		return ActionReceipt{}, ErrActionUnavailable
	}
	lr, err := r.ResolveLink(link)
	if err != nil {
		return ActionReceipt{}, err
	}
	if lr.ReadOnly || lr.Replan {
		return ActionReceipt{}, ErrStaleLink
	}
	if link.ActionID == "" {
		return ActionReceipt{}, ErrActionUnavailable
	}
	key := w.ID + "/" + idempotencyKey
	if prior, ok := seen[key]; ok {
		prior.Duplicate = true
		return prior, nil
	}
	found := false
	for _, a := range lr.Definition.Actions {
		if a.ID == link.ActionID {
			found = true
			break
		}
	}
	if !found {
		return ActionReceipt{}, ErrActionUnavailable
	}
	h := sha256.Sum256([]byte(lr.Definition.Digest + ":" + link.ActionID))
	rec := ActionReceipt{WorkID: w.ID, ActionID: link.ActionID, ActionDigest: hex.EncodeToString(h[:])}
	seen[key] = rec
	return rec, nil
}

// Versions returns deterministic snapshots for audit and tests.
func (r *Registry) Versions(flowID string) []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Definition{}
	for _, d := range r.versions {
		if d.FlowID == flowID {
			out = append(out, d.clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}
