package snapshot

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// FakeSource is an in-memory Source. It answers exactly the entries it was
// seeded with and nothing else: an input with no fixture is simply absent
// from the result, which Resolve's own completeness check turns into
// ErrMissingEntry rather than FakeSource inventing a value.
//
// It exists both for this package's own tests and for any later package that
// needs a source-neutral resolve without wiring a real per-domain
// repository (mirroring how internal/connectivity/schemasnapshot.
// NewMemoryStore lives in its own package rather than a _test.go file).
type FakeSource struct {
	// byTenant indexes fixtures by tenant, then by input name, so the same
	// fake can serve more than one tenant's fixtures without either leaking
	// into the other's answers.
	byTenant map[values.TenantId]map[string]InputEntry
	// Err, when set, is returned unconditionally instead of resolving.
	Err error
}

var _ Source = (*FakeSource)(nil)

// NewFakeSource builds an empty fake. Use Seed (or SeedRaw) to add fixtures.
func NewFakeSource() *FakeSource {
	return &FakeSource{byTenant: make(map[values.TenantId]map[string]InputEntry)}
}

// Seed records one fixture entry under its own Tenant and Name, and returns
// f so calls can be chained. It panics on an entry that fails its own
// Validate: a broken fixture is a test-setup bug, not a case under test. Use
// SeedRaw to seed a deliberately broken entry for a test that exercises
// Resolve's own validation.
func (f *FakeSource) Seed(entry InputEntry) *FakeSource {
	if err := entry.Validate(); err != nil {
		panic(fmt.Sprintf("snapshot: FakeSource.Seed: invalid fixture entry %q: %v", entry.Name, err))
	}
	return f.SeedRaw(entry)
}

// SeedRaw records entry without validating it.
func (f *FakeSource) SeedRaw(entry InputEntry) *FakeSource {
	byName, ok := f.byTenant[entry.Tenant]
	if !ok {
		byName = make(map[string]InputEntry)
		f.byTenant[entry.Tenant] = byName
	}
	byName[entry.Name] = entry
	return f
}

// Resolve implements Source.
func (f *FakeSource) Resolve(ctx context.Context, tenant values.TenantId, inputs []InputRequest) ([]InputEntry, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	byName := f.byTenant[tenant]
	out := make([]InputEntry, 0, len(inputs))
	for _, in := range inputs {
		if entry, ok := byName[in.Name]; ok {
			out = append(out, entry)
		}
	}
	return out, nil
}
