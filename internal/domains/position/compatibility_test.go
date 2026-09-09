package position_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// memoryPositionFacts is an in-memory position.PositionFacts test double. It
// answers exactly the revision it was seeded with (or none), which is what
// lets a test prove CheckCompatibility never invents a revision the reader
// did not return.
type memoryPositionFacts struct {
	revisions map[string]position.PositionRevision
	// Answer, when set, overrides the seeded lookup entirely - used to prove a
	// reader that answers about the wrong subject is rejected rather than
	// trusted.
	Answer *position.PositionRevision
	Fail   error
}

func newMemoryPositionFacts(revs ...position.PositionRevision) *memoryPositionFacts {
	m := &memoryPositionFacts{revisions: map[string]position.PositionRevision{}}
	for _, r := range revs {
		m.revisions[r.Position.Id] = r
	}
	return m
}

func (m *memoryPositionFacts) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	if m.Fail != nil {
		return position.PositionRevision{}, false, m.Fail
	}
	if err := q.Validate(); err != nil {
		return position.PositionRevision{}, false, err
	}
	if m.Answer != nil {
		return *m.Answer, true, nil
	}
	rev, ok := m.revisions[q.Position.Id]
	if !ok {
		return position.PositionRevision{}, false, nil
	}
	return rev, true, nil
}

// TestTodo_POSITION_001 is the registry's exact primary matrix symbol.
//
// It proves the GREEN clause: a current/as-of/known-at query returns the
// effective revision, lifecycle, capacity policy, source authority,
// provenance and compatibility findings for a matching request, and reports
// a non-existent position without inventing one.
func TestTodo_POSITION_001(t *testing.T) {
	ctx := context.Background()
	rev := validRevision(t, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)

	result, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
		Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
		DesiredJobCode: rev.JobCode, DesiredOrgUnit: rev.OrgUnit, DesiredLegalEntity: rev.LegalEntity,
	})
	if err != nil {
		t.Fatalf("CheckCompatibility: %v", err)
	}
	if !result.Exists {
		t.Fatal("existing position reported as not found")
	}
	if !result.Compatible {
		t.Fatalf("compatible position reported incompatible: findings=%v", result.Findings)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("compatible result must carry no findings, got %v", result.Findings)
	}
	if !result.Revision.Equal(rev.Revision) {
		t.Errorf("revision = %s, want %s", result.Revision, rev.Revision)
	}
	if result.Lifecycle != rev.Lifecycle {
		t.Errorf("lifecycle = %s, want %s", result.Lifecycle, rev.Lifecycle)
	}
	if !result.Capacity.CapacityFTE.Equal(rev.Capacity.CapacityFTE) {
		t.Errorf("capacity fte = %s, want %s", result.Capacity.CapacityFTE, rev.Capacity.CapacityFTE)
	}
	if result.Authority.Validate() != nil {
		t.Error("compatible result must carry source authority")
	}
	if result.Provenance.Validate() != nil {
		t.Error("compatible result must carry provenance")
	}

	notFound, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
		Tenant: "tenant-a", Position: positionRef(t, "tenant-a", "pos-missing"), AsOf: testAsOf(t),
	})
	if err != nil {
		t.Fatalf("CheckCompatibility (missing): %v", err)
	}
	if notFound.Exists {
		t.Fatal("a position the reader never seeded must not be reported as existing")
	}
	if notFound.Compatible {
		t.Fatal("a not-found position must never be reported compatible")
	}
	if len(notFound.Findings) != 1 || notFound.Findings[0].Code != position.FindingNotFound {
		t.Fatalf("findings = %v, want exactly [POSITION_NOT_FOUND]", notFound.Findings)
	}
}

// TestTodo_POSITION_001_Conformance is the registry's exact conformance
// matrix symbol. It enumerates the acceptance corpus named directly by the
// RED clause - closed, frozen, wrong job, wrong org, wrong legal entity, and
// stale - proving each is reported incompatible with exactly its finding.
func TestTodo_POSITION_001_Conformance(t *testing.T) {
	ctx := context.Background()
	base := validRevision(t, "tenant-a", "pos-1")

	staleBaseline, err := values.NewSequenceRevision(base.Revision.Stream(), 99)
	if err != nil {
		t.Fatalf("stale baseline revision: %v", err)
	}

	cases := []struct {
		name        string
		mutate      func(*position.PositionRevision)
		want        []position.FindingCode
		minRevision values.RevisionToken
	}{
		{"closed", func(r *position.PositionRevision) { r.Lifecycle = position.LifecycleClosed }, []position.FindingCode{position.FindingClosed}, values.RevisionToken{}},
		{"frozen", func(r *position.PositionRevision) { r.Lifecycle = position.LifecycleFrozen }, []position.FindingCode{position.FindingFrozen}, values.RevisionToken{}},
		{"wrong job", func(*position.PositionRevision) {}, []position.FindingCode{position.FindingJobMismatch}, values.RevisionToken{}},
		{"wrong org", func(*position.PositionRevision) {}, []position.FindingCode{position.FindingOrgMismatch}, values.RevisionToken{}},
		{"wrong legal entity", func(*position.PositionRevision) {}, []position.FindingCode{position.FindingLegalEntityMismatch}, values.RevisionToken{}},
		{"stale revision", func(*position.PositionRevision) {}, []position.FindingCode{position.FindingStaleRevision}, staleBaseline},
		{"fully compatible", func(*position.PositionRevision) {}, nil, values.RevisionToken{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := base
			tc.mutate(&rev)
			reader := newMemoryPositionFacts(rev)

			req := position.CompatibilityRequest{
				Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
				DesiredJobCode:     rev.JobCode,
				DesiredOrgUnit:     rev.OrgUnit,
				DesiredLegalEntity: rev.LegalEntity,
				MinRevision:        tc.minRevision,
			}
			switch tc.name {
			case "wrong job":
				req.DesiredJobCode = "OTHER-JOB"
			case "wrong org":
				req.DesiredOrgUnit = "other-org"
			case "wrong legal entity":
				req.DesiredLegalEntity = "Other Legal Entity LLC"
			}

			result, err := position.CheckCompatibility(ctx, reader, req)
			if err != nil {
				t.Fatalf("CheckCompatibility: %v", err)
			}
			if len(tc.want) == 0 {
				if !result.Compatible || len(result.Findings) != 0 {
					t.Fatalf("expected compatible with no findings, got compatible=%v findings=%v", result.Compatible, result.Findings)
				}
				return
			}
			if result.Compatible {
				t.Fatal("expected incompatible result")
			}
			if len(result.Findings) != len(tc.want) {
				t.Fatalf("findings = %v, want %v", result.Findings, tc.want)
			}
			for i, code := range tc.want {
				if result.Findings[i].Code != code {
					t.Errorf("finding[%d] = %s, want %s", i, result.Findings[i].Code, code)
				}
			}
		})
	}
}

// TestTodo_POSITION_001_Property is the registry's exact property matrix
// symbol: for any combination of match/mismatch dimensions, Compatible must
// be exactly the absence of findings, and every finding code produced must be
// one this package defines.
func TestTodo_POSITION_001_Property(t *testing.T) {
	ctx := context.Background()
	base := validRevision(t, "tenant-a", "pos-1")

	lifecycles := []position.Lifecycle{position.LifecycleOpen, position.LifecycleFrozen, position.LifecycleClosed, position.LifecycleVacant}
	jobs := []string{base.JobCode, "OTHER-JOB"}
	orgs := []string{base.OrgUnit, "other-org"}
	legalEntities := []string{base.LegalEntity, "Other Legal Entity LLC"}

	for _, lifecycle := range lifecycles {
		for _, job := range jobs {
			for _, org := range orgs {
				for _, entity := range legalEntities {
					rev := base
					rev.Lifecycle = lifecycle
					reader := newMemoryPositionFacts(rev)
					result, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
						Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
						DesiredJobCode: job, DesiredOrgUnit: org, DesiredLegalEntity: entity,
					})
					if err != nil {
						t.Fatalf("CheckCompatibility(lifecycle=%s job=%s org=%s entity=%s): %v", lifecycle, job, org, entity, err)
					}
					if result.Compatible != (len(result.Findings) == 0) {
						t.Fatalf("Compatible=%v but findings=%v (lifecycle=%s job=%s org=%s entity=%s)",
							result.Compatible, result.Findings, lifecycle, job, org, entity)
					}
					for _, f := range result.Findings {
						if !f.Code.Valid() {
							t.Fatalf("undefined finding code %q", f.Code)
						}
					}
				}
			}
		}
	}
}

// TestTodo_POSITION_001_Security is the registry's exact security matrix
// symbol.
//
// It proves: a cross-tenant position reference is refused before the reader
// is even asked, a reader that answers about a different position than the
// one requested is rejected rather than trusted, and a caller-supplied
// Authorize gate that refuses a position discloses nothing about it.
func TestTodo_POSITION_001_Security(t *testing.T) {
	ctx := context.Background()
	rev := validRevision(t, "tenant-a", "pos-1")

	t.Run("cross-tenant position is refused without touching the reader", func(t *testing.T) {
		reader := newMemoryPositionFacts(rev)
		foreign := values.EntityRef{Tenant: "tenant-b", Kind: position.KindPosition, Id: rev.Position.Id}
		_, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
			Tenant: "tenant-a", Position: foreign, AsOf: testAsOf(t),
		})
		if !errors.Is(err, position.ErrInvalidRequest) {
			t.Fatalf("error = %v, want ErrInvalidRequest", err)
		}
	})

	t.Run("a reader that answers about a different position is rejected", func(t *testing.T) {
		other := validRevision(t, "tenant-a", "pos-other")
		reader := newMemoryPositionFacts(rev)
		reader.Answer = &other
		_, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
		})
		if !errors.Is(err, position.ErrSubjectMismatch) {
			t.Fatalf("error = %v, want ErrSubjectMismatch", err)
		}
	})

	t.Run("an unauthorized scope is refused and discloses nothing", func(t *testing.T) {
		reader := newMemoryPositionFacts(rev)
		result, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
			Authorize: func(position.PositionRevision) bool { return false },
		})
		if !errors.Is(err, position.ErrUnauthorized) {
			t.Fatalf("error = %v, want ErrUnauthorized", err)
		}
		if result.Exists || result.Compatible || len(result.Findings) != 0 {
			t.Fatalf("an unauthorized read must return a zero-value result, got %+v", result)
		}
	})

	t.Run("an authorized scope proceeds normally", func(t *testing.T) {
		reader := newMemoryPositionFacts(rev)
		result, err := position.CheckCompatibility(ctx, reader, position.CompatibilityRequest{
			Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(t),
			DesiredJobCode: rev.JobCode, DesiredOrgUnit: rev.OrgUnit, DesiredLegalEntity: rev.LegalEntity,
			Authorize: func(position.PositionRevision) bool { return true },
		})
		if err != nil {
			t.Fatalf("CheckCompatibility: %v", err)
		}
		if !result.Compatible {
			t.Fatalf("expected compatible, findings=%v", result.Findings)
		}
	})
}

// TestTodo_POSITION_001_Mutation is the registry's exact mutation matrix
// symbol: every material change to a position revision must change its
// canonical encoding.
func TestTodo_POSITION_001_Mutation(t *testing.T) {
	base := validRevision(t, "tenant-a", "pos-1")
	baseCanonical := string(base.Canonical())
	if baseCanonical == "" {
		t.Fatal("valid revision has no canonical encoding")
	}

	cases := []struct {
		name   string
		mutate func(*position.PositionRevision)
	}{
		{"lifecycle", func(r *position.PositionRevision) { r.Lifecycle = position.LifecycleClosed }},
		{"job code", func(r *position.PositionRevision) { r.JobCode = "OTHER-JOB" }},
		{"org unit", func(r *position.PositionRevision) { r.OrgUnit = "other-org" }},
		{"legal entity", func(r *position.PositionRevision) { r.LegalEntity = "Other Legal Entity LLC" }},
		{"capacity heads", func(r *position.PositionRevision) { r.Capacity.CapacityHeads = 5 }},
		{"overfill allowed", func(r *position.PositionRevision) { r.Capacity.OverfillAllowed = true }},
		{"authority system", func(r *position.PositionRevision) { r.Authority.System = "other.system" }},
		{"provenance evidence", func(r *position.PositionRevision) { r.Provenance.EvidenceRef = "evd_other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rev := base
			tc.mutate(&rev)
			if got := string(rev.Canonical()); got == baseCanonical {
				t.Fatal("material revision mutation did not change canonical encoding")
			}
		})
	}
}

// BenchmarkTodo_POSITION_001 is the registry's required benchmark matrix
// symbol.
func BenchmarkTodo_POSITION_001(b *testing.B) {
	rev := validRevision(b, "tenant-a", "pos-1")
	reader := newMemoryPositionFacts(rev)
	req := position.CompatibilityRequest{
		Tenant: "tenant-a", Position: rev.Position, AsOf: testAsOf(b),
		DesiredJobCode: rev.JobCode, DesiredOrgUnit: rev.OrgUnit, DesiredLegalEntity: rev.LegalEntity,
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := position.CheckCompatibility(ctx, reader, req); err != nil {
			b.Fatalf("CheckCompatibility: %v", err)
		}
	}
}
