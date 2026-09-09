package snapshot

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Resolve errors. All are matchable with errors.Is; each names the exact
// refusal SNAPSHOT-001's RED/GREEN clauses require rather than folding every
// defect into one generic failure.
var (
	// ErrNoInputsRequested is returned for a Resolve call naming no inputs.
	ErrNoInputsRequested = errors.New("snapshot: resolve requires at least one input")
	// ErrDuplicateInputRequest is returned when the same input name is
	// requested more than once.
	ErrDuplicateInputRequest = errors.New("snapshot: input requested more than once")
	// ErrSourceFailed wraps a failure returned by the configured Source.
	ErrSourceFailed = errors.New("snapshot: source failed to resolve inputs")
	// ErrDuplicateEntry is returned when the source answers one input name
	// more than once.
	ErrDuplicateEntry = errors.New("snapshot: source answered one input more than once")
	// ErrMissingEntry is returned when the source does not answer a
	// requested input, or a watermark floor names an input nothing resolved.
	ErrMissingEntry = errors.New("snapshot: source did not answer a requested input")
	// ErrUnrequestedEntry is returned when the source answers an input that
	// was never requested.
	ErrUnrequestedEntry = errors.New("snapshot: source answered an input that was not requested")
	// ErrTenantMismatch is returned when a resolved entry belongs to a
	// tenant other than the requirement's own tenant.
	ErrTenantMismatch = errors.New("snapshot: entry belongs to a different tenant than the requirement")
	// ErrKnownAtHorizonMismatch is returned when a resolved entry's known-at
	// does not equal the requirement's known-at horizon.
	ErrKnownAtHorizonMismatch = errors.New("snapshot: entry known-at does not match the requirement's known-at horizon")
	// ErrAuthorityMismatch is returned when a resolved entry's authority
	// class does not equal the input's pinned RequiredAuthority -- the
	// refusal that keeps an external observation from being presented as
	// native state (or the reverse).
	ErrAuthorityMismatch = errors.New("snapshot: entry authority does not match the input's required authority class")
	// ErrWatermarkBelowMinimum is returned when a resolved entry's watermark
	// is below its declared minimum.
	ErrWatermarkBelowMinimum = errors.New("snapshot: entry watermark is below the required minimum")
	// ErrWatermarkIncomparable is returned when a resolved entry's watermark
	// cannot be ordered against its declared minimum (different streams, or
	// either side unspecified) -- refused rather than assumed satisfied.
	ErrWatermarkIncomparable = errors.New("snapshot: entry watermark cannot be compared against the required minimum")
)

// Source is the port a source-neutral read resolves through: given a tenant
// and the exact inputs asked for, it returns the typed entries answering
// them. A Source must never widen the requested set on its own initiative,
// and it must never collapse the distinction between authority classes --
// both are things Resolve verifies, but a Source that tried to hide either
// would defeat the whole contract.
type Source interface {
	// Resolve returns the entries answering inputs for tenant. It returns an
	// error only for a read failure; an input this source has no fixture or
	// record for is simply absent from the result, which Resolve's own
	// completeness check turns into ErrMissingEntry.
	Resolve(ctx context.Context, tenant values.TenantId, inputs []InputRequest) ([]InputEntry, error)
}

const readSnapshotSchema = "hcmnext.engines.snapshot.ReadSnapshot"

// ReadSnapshot is an immutable, source-neutral read across however many
// business inputs a caller asked for, all resolved to one consistent
// tenant/known-at horizon and bound to Digest, the canonical sha256 digest
// over the requirement and the exact resolved entries.
//
// ReadSnapshot is a plain value: copying it copies every field (InputEntry
// itself holds no pointers or slices), so altering a copy's entry can never
// alter the snapshot Resolve returned, and recomputing a digest over an
// altered entry always yields a different digest -- see
// TestTodo_SNAPSHOT_001_Mutation.
type ReadSnapshot struct {
	// Tenant is the requirement's tenant, echoed here for convenience.
	Tenant values.TenantId
	// KnownAtHorizon is the requirement's known-at horizon, echoed here for
	// convenience.
	KnownAtHorizon values.KnownAt
	// Requirement is the exact ConsistencyRequirement this snapshot
	// satisfied.
	Requirement ConsistencyRequirement
	// Entries are the resolved inputs, sorted by Name.
	Entries []InputEntry
	// Digest is "sha256:<hex>" over Requirement and Entries. Two resolves
	// over byte-identical inputs always share it; any change to any entry
	// or to the requirement always changes it.
	Digest string
}

// Lookup returns the resolved entry for a named input, if present.
func (s ReadSnapshot) Lookup(name string) (InputEntry, bool) {
	for _, e := range s.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return InputEntry{}, false
}

// computeDigest returns the canonical sha256 digest over requirement and the
// exact set of entries. Both requirement.MinWatermarks and entries are
// sorted by their own name before writing, so the digest depends only on
// content, never on caller- or source-supplied ordering.
func computeDigest(requirement ConsistencyRequirement, entries []InputEntry) (string, error) {
	floors := append([]InputWatermarkFloor(nil), requirement.MinWatermarks...)
	sort.Slice(floors, func(i, j int) bool { return floors[i].InputName < floors[j].InputName })

	sorted := append([]InputEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	w := canonicalbytes.New(readSnapshotSchema, snapshotSchemaVer).
		String("tenant", string(requirement.Tenant)).
		Value("known_at_horizon", requirement.KnownAtHorizon).
		Count("min_watermarks", len(floors))
	for _, f := range floors {
		w.String("min_watermark.input", f.InputName).Value("min_watermark.minimum", f.Minimum)
	}
	w.Count("entries", len(sorted))
	for _, e := range sorted {
		w.Value("entry", e)
	}
	return w.Digest()
}

// findEntry returns the entry named name, if present.
func findEntry(entries []InputEntry, name string) (InputEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return InputEntry{}, false
}

// Resolve asks source for exactly the named inputs, under requirement, and
// returns one immutable ReadSnapshot -- or refuses with a typed error naming
// exactly which contract the request, the source's answer, or the
// requirement's consistency check broke.
//
// Every requested input is mandatory: a source answer missing one, adding an
// unrequested one, duplicating one, or answering one with an incomplete
// entry, a mismatched tenant, a known-at outside the horizon, an authority
// that does not match what the request pinned, or a watermark below its
// declared floor all refuse the entire resolve. There is no partial
// ReadSnapshot; see SNAPSHOT-003 for the domain-facing completeness verdict
// this baseline does not attempt.
func Resolve(
	ctx context.Context,
	source Source,
	requirement ConsistencyRequirement,
	inputs []InputRequest,
) (ReadSnapshot, error) {
	if source == nil {
		return ReadSnapshot{}, fmt.Errorf("%w: no source configured", ErrSourceFailed)
	}
	if err := requirement.Validate(); err != nil {
		return ReadSnapshot{}, err
	}
	if len(inputs) == 0 {
		return ReadSnapshot{}, ErrNoInputsRequested
	}

	requested := make(map[string]InputRequest, len(inputs))
	for _, in := range inputs {
		if err := in.Validate(); err != nil {
			return ReadSnapshot{}, err
		}
		if _, dup := requested[in.Name]; dup {
			return ReadSnapshot{}, fmt.Errorf("%w: %q", ErrDuplicateInputRequest, in.Name)
		}
		requested[in.Name] = in
	}

	entries, err := source.Resolve(ctx, requirement.Tenant, inputs)
	if err != nil {
		return ReadSnapshot{}, fmt.Errorf("%w: %w", ErrSourceFailed, err)
	}

	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		req, wasRequested := requested[e.Name]
		if !wasRequested {
			return ReadSnapshot{}, fmt.Errorf("%w: %q", ErrUnrequestedEntry, e.Name)
		}
		if _, dup := seen[e.Name]; dup {
			return ReadSnapshot{}, fmt.Errorf("%w: %q", ErrDuplicateEntry, e.Name)
		}
		seen[e.Name] = struct{}{}

		if err := e.Validate(); err != nil {
			return ReadSnapshot{}, err
		}
		if e.Tenant != requirement.Tenant {
			return ReadSnapshot{}, fmt.Errorf("%w: input %q tenant %q, requirement tenant %q",
				ErrTenantMismatch, e.Name, e.Tenant, requirement.Tenant)
		}
		if e.KnownAt.Instant().Compare(requirement.KnownAtHorizon.Instant()) != 0 {
			return ReadSnapshot{}, fmt.Errorf("%w: input %q known-at %s, horizon %s",
				ErrKnownAtHorizonMismatch, e.Name, e.KnownAt, requirement.KnownAtHorizon)
		}
		if req.RequiredAuthority != AuthorityUnspecified && e.Authority != req.RequiredAuthority {
			return ReadSnapshot{}, fmt.Errorf("%w: input %q is %s, required %s",
				ErrAuthorityMismatch, e.Name, e.Authority, req.RequiredAuthority)
		}
	}
	for name := range requested {
		if _, ok := seen[name]; !ok {
			return ReadSnapshot{}, fmt.Errorf("%w: %q", ErrMissingEntry, name)
		}
	}

	for _, floor := range requirement.MinWatermarks {
		entry, ok := findEntry(entries, floor.InputName)
		if !ok {
			return ReadSnapshot{}, fmt.Errorf("%w: watermark floor names unresolved input %q", ErrMissingEntry, floor.InputName)
		}
		cmp, err := entry.Watermark.CompareInStream(floor.Minimum)
		if err != nil {
			return ReadSnapshot{}, fmt.Errorf("%w: input %q: %w", ErrWatermarkIncomparable, floor.InputName, err)
		}
		if cmp < 0 {
			return ReadSnapshot{}, fmt.Errorf("%w: input %q", ErrWatermarkBelowMinimum, floor.InputName)
		}
	}

	sorted := append([]InputEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	digest, err := computeDigest(requirement, sorted)
	if err != nil {
		return ReadSnapshot{}, fmt.Errorf("snapshot: computing digest: %w", err)
	}

	return ReadSnapshot{
		Tenant:         requirement.Tenant,
		KnownAtHorizon: requirement.KnownAtHorizon,
		Requirement:    requirement,
		Entries:        sorted,
		Digest:         digest,
	}, nil
}
