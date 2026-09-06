package snapshot_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/snapshot"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// mustInstant builds a values.Instant at the given UTC clock reading, failing
// the test on any construction error.
func mustInstant(t *testing.T, year int, month time.Month, day, hour, min, sec int) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(year, month, day, hour, min, sec, 0, time.UTC))
}

// mustKnownAt wraps an instant as a KnownAt.
func mustKnownAt(t *testing.T, at values.Instant) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(at)
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return k
}

// mustRecordedAt wraps an instant as a RecordedAt.
func mustRecordedAt(t *testing.T, at values.Instant) values.RecordedAt {
	t.Helper()
	r, err := values.NewRecordedAt(at)
	if err != nil {
		t.Fatalf("NewRecordedAt: %v", err)
	}
	return r
}

// mustRevision builds a specified sequence revision token.
func mustRevision(t *testing.T, stream string, seq uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision(stream, seq)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	return r
}

// mustLocalDate builds a calendar date.
func mustLocalDate(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("NewLocalDate: %v", err)
	}
	return d
}

// fixtureTenant is a canonical tenant slug used across this package's tests.
const fixtureTenant values.TenantId = "harborcare-demo"

// fixtureHorizon is the shared known-at horizon most fixtures resolve under.
func fixtureHorizon(t *testing.T) values.KnownAt {
	t.Helper()
	return mustKnownAt(t, mustInstant(t, 2026, time.March, 1, 12, 0, 0))
}

// validEntry returns a fully valid InputEntry for name/tenant/knownAt/
// authority, using otherwise fixed, deterministic evidence so tests can
// tweak exactly the one field under test.
func validEntry(t *testing.T, name string, tenant values.TenantId, knownAt values.KnownAt, authority snapshot.AuthorityClass) snapshot.InputEntry {
	t.Helper()
	return snapshot.InputEntry{
		Name:      name,
		Owner:     "people",
		Tenant:    tenant,
		Authority: authority,
		Source: snapshot.SourceRef{
			System:     "hcmnext.people",
			Connection: "primary",
			Ref:        name,
		},
		EffectiveAt:    mustLocalDate(t, 2026, time.January, 1),
		KnownAt:        knownAt,
		Revision:       mustRevision(t, "stream."+name, 1),
		Head:           "head-1",
		Watermark:      mustRevision(t, "watermark."+name, 1),
		Freshness:      mustRecordedAt(t, mustInstant(t, 2026, time.February, 28, 0, 0, 0)),
		Classification: snapshot.ClassificationInternal,
		Provenance: snapshot.Provenance{
			Source:      "hcmnext.people",
			EvidenceRef: "evd_" + name + "_r1",
			RecordedAt:  mustRecordedAt(t, mustInstant(t, 2026, time.February, 28, 0, 0, 0)),
		},
		ReferenceVersion: "v1",
	}
}
