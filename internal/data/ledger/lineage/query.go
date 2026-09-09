package lineage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// Querier is the minimal database capability a lineage read needs. It is
// internal/data/ledger.Querier itself, so whatever handle a caller already
// reads the ledger through satisfies it without adaptation.
type Querier = datalogger.Querier

// get reads one event's identity and correction target. ok is false when no
// such event exists for this tenant - including when it exists only for a
// different tenant, since every query here is scoped by the tenant argument.
func get(ctx context.Context, q Querier, tenant uuid.UUID, ref EventRef) (Node, bool, error) {
	var (
		node           Node
		assertionClass string
		correctsStream *string
		correctsSeq    *int64
	)
	node.Ref = ref
	row := q.QueryRow(ctx, `
		SELECT event_id, assertion_class, recorded_at, corrects_stream_key, corrects_sequence
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		tenant, ref.StreamKey, ref.Sequence)
	if err := row.Scan(&node.EventID, &assertionClass, &node.RecordedAt, &correctsStream, &correctsSeq); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Node{}, false, nil
		}
		return Node{}, false, fmt.Errorf("lineage: read %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}
	node.AssertionClass = datalogger.AssertionClass(assertionClass)
	if correctsStream != nil && correctsSeq != nil {
		node.Corrects = &EventRef{StreamKey: *correctsStream, Sequence: *correctsSeq}
	}
	return node, true, nil
}

// children returns every event that directly corrects or supersedes ref,
// ordered deterministically by (sequence, stream key) rather than by
// recorded_at: sequence is this platform's ordering authority, wall clock is
// only ever evidence (specs/transaction-ledger-reconciliation-and-repair.md
// 16).
func children(ctx context.Context, q Querier, tenant uuid.UUID, ref EventRef) ([]Node, error) {
	rows, err := q.Query(ctx, `
		SELECT stream_key, sequence, event_id, assertion_class, recorded_at
		FROM ledger_event
		WHERE tenant_id = $1 AND corrects_stream_key = $2 AND corrects_sequence = $3
		ORDER BY sequence ASC, stream_key ASC`,
		tenant, ref.StreamKey, ref.Sequence)
	if err != nil {
		return nil, fmt.Errorf("lineage: read children of %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}
	defer rows.Close()

	var out []Node
	for rows.Next() {
		var (
			node           Node
			assertionClass string
		)
		if err := rows.Scan(&node.Ref.StreamKey, &node.Ref.Sequence, &node.EventID, &assertionClass, &node.RecordedAt); err != nil {
			return nil, fmt.Errorf("lineage: scan child of %s@%d: %w", ref.StreamKey, ref.Sequence, err)
		}
		node.AssertionClass = datalogger.AssertionClass(assertionClass)
		target := ref
		node.Corrects = &target
		out = append(out, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lineage: read children of %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}
	return out, nil
}

// ValidateCorrectionTarget resolves corrects within tenant's scope and fails
// with [ErrCorrectionTargetNotFound] when it cannot be found - which is
// exactly what happens for a target that belongs to a different tenant,
// since every lookup here is scoped by tenant. Call it before submitting a
// CORRECTION request to internal/data/ledger.Append (or use [Append], which
// does so automatically) so a dangling or cross-tenant reference is refused
// before anything is written, not discovered later by a lineage query that
// cannot resolve it.
func ValidateCorrectionTarget(ctx context.Context, q Querier, tenant uuid.UUID, corrects EventRef) (Node, error) {
	node, ok, err := get(ctx, q, tenant, corrects)
	if err != nil {
		return Node{}, err
	}
	if !ok {
		return Node{}, ErrCorrectionTargetNotFound{Tenant: tenant, StreamKey: corrects.StreamKey, Sequence: corrects.Sequence}
	}
	return node, nil
}

// Ancestors walks backward from ref through successive Corrects links and
// returns them in order: ref's immediate correction target first, then its
// target's target, and so on to the origin (the first node with no
// correction target of its own). ref itself is not included.
//
// The walk fails closed with [ErrLineageCycle] if it ever revisits a
// (stream, sequence) it has already seen, or exceeds [maxLineageDepth] -
// see the package doc for why a well-formed ledger can never trigger either
// - and with [ErrCorrectionTargetNotFound] if a Corrects link names an event
// that does not exist for tenant.
func Ancestors(ctx context.Context, q Querier, tenant uuid.UUID, ref EventRef) ([]Node, error) {
	node, ok, err := get(ctx, q, tenant, ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrCorrectionTargetNotFound{Tenant: tenant, StreamKey: ref.StreamKey, Sequence: ref.Sequence}
	}

	visited := map[EventRef]bool{ref: true}
	var out []Node
	for i := 0; node.Corrects != nil; i++ {
		if i >= maxLineageDepth {
			return nil, ErrLineageCycle{Path: append(refsOf(out), *node.Corrects)}
		}
		parent := *node.Corrects
		if visited[parent] {
			return nil, ErrLineageCycle{Path: append(refsOf(out), parent)}
		}
		visited[parent] = true

		parentNode, ok, err := get(ctx, q, tenant, parent)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCorrectionTargetNotFound{Tenant: tenant, StreamKey: parent.StreamKey, Sequence: parent.Sequence}
		}
		out = append(out, parentNode)
		node = parentNode
	}
	return out, nil
}

// Descendants returns every event that corrects or supersedes ref,
// transitively, as a breadth-first flattening of the (possibly branching)
// supersession graph rooted at ref: ref's direct correctors first, then
// theirs, and so on. Order within and across levels is deterministic - see
// [children] - so two calls against the same data always return the same
// slice.
//
// It fails closed exactly as [Ancestors] does when the graph would not
// terminate.
func Descendants(ctx context.Context, q Querier, tenant uuid.UUID, ref EventRef) ([]Node, error) {
	if _, ok, err := get(ctx, q, tenant, ref); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrCorrectionTargetNotFound{Tenant: tenant, StreamKey: ref.StreamKey, Sequence: ref.Sequence}
	}

	visited := map[EventRef]bool{ref: true}
	queue := []EventRef{ref}
	var out []Node
	steps := 0

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		kids, err := children(ctx, q, tenant, current)
		if err != nil {
			return nil, err
		}
		for _, kid := range kids {
			steps++
			if steps > maxLineageDepth {
				return nil, ErrLineageCycle{Path: append(refsOf(out), kid.Ref)}
			}
			if visited[kid.Ref] {
				return nil, ErrLineageCycle{Path: append(refsOf(out), kid.Ref)}
			}
			visited[kid.Ref] = true
			out = append(out, kid)
			queue = append(queue, kid.Ref)
		}
	}
	return out, nil
}

// EffectiveCurrent resolves the terminal node of ref's supersession chain:
// starting from ref, it repeatedly follows the *latest* direct corrector -
// ordered by (sequence, stream key) descending, never by recorded_at, so the
// choice among branches is deterministic even when more than one event
// corrects the same target - until a node with no corrector remains. It
// returns that terminal node together with the full path from ref to it
// (excluding ref).
//
// A ref with no correctors at all is already its own effective-current node:
// EffectiveCurrent returns it with an empty path.
func EffectiveCurrent(ctx context.Context, q Querier, tenant uuid.UUID, ref EventRef) (Node, []Node, error) {
	current, ok, err := get(ctx, q, tenant, ref)
	if err != nil {
		return Node{}, nil, err
	}
	if !ok {
		return Node{}, nil, ErrCorrectionTargetNotFound{Tenant: tenant, StreamKey: ref.StreamKey, Sequence: ref.Sequence}
	}

	visited := map[EventRef]bool{ref: true}
	var path []Node
	for i := 0; ; i++ {
		if i >= maxLineageDepth {
			return Node{}, nil, ErrLineageCycle{Path: append(refsOf(path), current.Ref)}
		}
		kids, err := children(ctx, q, tenant, current.Ref)
		if err != nil {
			return Node{}, nil, err
		}
		if len(kids) == 0 {
			return current, path, nil
		}
		next := latest(kids)
		if visited[next.Ref] {
			return Node{}, nil, ErrLineageCycle{Path: append(refsOf(path), next.Ref)}
		}
		visited[next.Ref] = true
		path = append(path, next)
		current = next
	}
}

// latest picks the deterministic "current" branch among sibling correctors:
// the highest sequence, tie-broken by stream key. children already orders
// its result this way; latest takes the last element for clarity at the call
// site rather than relying on that ordering silently.
func latest(nodes []Node) Node {
	best := nodes[0]
	for _, n := range nodes[1:] {
		if n.Ref.Sequence > best.Ref.Sequence ||
			(n.Ref.Sequence == best.Ref.Sequence && n.Ref.StreamKey > best.Ref.StreamKey) {
			best = n
		}
	}
	return best
}

func refsOf(nodes []Node) []EventRef {
	out := make([]EventRef, len(nodes))
	for i, n := range nodes {
		out[i] = n.Ref
	}
	return out
}
