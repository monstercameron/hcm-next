package snapshot_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAuthorityClassValidAndString(t *testing.T) {
	t.Parallel()
	for _, a := range []snapshot.AuthorityClass{
		snapshot.AuthorityNativeState, snapshot.AuthorityExternalObservation, snapshot.AuthorityReferenceConfig,
	} {
		if !a.Valid() {
			t.Errorf("%q: Valid() = false, want true", a)
		}
		if a.String() != string(a) {
			t.Errorf("%q: String() = %q", a, a.String())
		}
	}
	if snapshot.AuthorityUnspecified.Valid() {
		t.Error("AuthorityUnspecified.Valid() = true, want false")
	}
	if got := snapshot.AuthorityUnspecified.String(); got != "AUTHORITY_UNSPECIFIED" {
		t.Errorf("AuthorityUnspecified.String() = %q", got)
	}
	if snapshot.AuthorityClass("bogus").Valid() {
		t.Error("bogus authority class Valid() = true, want false")
	}
}

func TestClassificationValidAndString(t *testing.T) {
	t.Parallel()
	for _, c := range []snapshot.Classification{
		snapshot.ClassificationPublic, snapshot.ClassificationInternal,
		snapshot.ClassificationConfidential, snapshot.ClassificationRestricted,
	} {
		if !c.Valid() {
			t.Errorf("%q: Valid() = false, want true", c)
		}
	}
	if snapshot.ClassificationUnspecified.Valid() {
		t.Error("ClassificationUnspecified.Valid() = true, want false")
	}
	if got := snapshot.ClassificationUnspecified.String(); got != "CLASSIFICATION_UNSPECIFIED" {
		t.Errorf("ClassificationUnspecified.String() = %q", got)
	}
}

func TestSourceRefValidate(t *testing.T) {
	t.Parallel()
	valid := snapshot.SourceRef{System: "hcmnext.people", Connection: "primary", Ref: "worker_facts"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid SourceRef: %v", err)
	}
	if valid.Canonical() == nil {
		t.Fatal("valid SourceRef: Canonical() = nil")
	}

	cases := []snapshot.SourceRef{
		{Connection: "primary", Ref: "x"},
		{System: "hcmnext.people", Ref: "x"},
		{System: "hcmnext.people", Connection: "primary"},
	}
	for i, c := range cases {
		if err := c.Validate(); !errors.Is(err, snapshot.ErrEntryIncomplete) {
			t.Errorf("case %d: Validate() = %v, want ErrEntryIncomplete", i, err)
		}
		if c.Canonical() != nil {
			t.Errorf("case %d: Canonical() of an invalid SourceRef is non-nil", i)
		}
	}
}

func TestProvenanceValidate(t *testing.T) {
	t.Parallel()
	valid := snapshot.Provenance{
		Source:      "hcmnext.people",
		EvidenceRef: "evd_1",
		RecordedAt:  mustRecordedAt(t, mustInstant(t, 2026, 1, 1, 0, 0, 0)),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Provenance: %v", err)
	}
	if valid.Canonical() == nil {
		t.Fatal("valid Provenance: Canonical() = nil")
	}

	missingSource := valid
	missingSource.Source = ""
	if err := missingSource.Validate(); !errors.Is(err, snapshot.ErrEntryIncomplete) {
		t.Errorf("missing source: Validate() = %v, want ErrEntryIncomplete", err)
	}

	missingEvidence := valid
	missingEvidence.EvidenceRef = ""
	if err := missingEvidence.Validate(); !errors.Is(err, snapshot.ErrEntryIncomplete) {
		t.Errorf("missing evidence ref: Validate() = %v, want ErrEntryIncomplete", err)
	}

	missingRecordedAt := valid
	missingRecordedAt.RecordedAt = values.RecordedAt{}
	if err := missingRecordedAt.Validate(); !errors.Is(err, snapshot.ErrEntryIncomplete) {
		t.Errorf("missing recorded-at: Validate() = %v, want ErrEntryIncomplete", err)
	}
}

// TestInputEntryValidateRequiresEveryDescriptor is the RED clause of
// SNAPSHOT-001 exercised field by field: an entry missing any one of the
// documented descriptors is refused, never silently accepted or completed.
func TestInputEntryValidateRequiresEveryDescriptor(t *testing.T) {
	t.Parallel()
	horizon := fixtureHorizon(t)
	base := validEntry(t, "people.worker_facts", fixtureTenant, horizon, snapshot.AuthorityNativeState)
	if err := base.Validate(); err != nil {
		t.Fatalf("base fixture must be valid: %v", err)
	}
	if base.Canonical() == nil {
		t.Fatal("base fixture: Canonical() = nil")
	}

	mutate := func(f func(e *snapshot.InputEntry)) snapshot.InputEntry {
		e := base
		f(&e)
		return e
	}

	cases := map[string]snapshot.InputEntry{
		"name":              mutate(func(e *snapshot.InputEntry) { e.Name = "" }),
		"owner":             mutate(func(e *snapshot.InputEntry) { e.Owner = "" }),
		"tenant":            mutate(func(e *snapshot.InputEntry) { e.Tenant = "" }),
		"authority":         mutate(func(e *snapshot.InputEntry) { e.Authority = snapshot.AuthorityUnspecified }),
		"source":            mutate(func(e *snapshot.InputEntry) { e.Source = snapshot.SourceRef{} }),
		"effective_at":      mutate(func(e *snapshot.InputEntry) { e.EffectiveAt = values.LocalDate{} }),
		"known_at":          mutate(func(e *snapshot.InputEntry) { e.KnownAt = values.KnownAt{} }),
		"revision":          mutate(func(e *snapshot.InputEntry) { e.Revision = values.RevisionToken{} }),
		"head":              mutate(func(e *snapshot.InputEntry) { e.Head = "" }),
		"watermark":         mutate(func(e *snapshot.InputEntry) { e.Watermark = values.RevisionToken{} }),
		"freshness":         mutate(func(e *snapshot.InputEntry) { e.Freshness = values.RecordedAt{} }),
		"classification":    mutate(func(e *snapshot.InputEntry) { e.Classification = snapshot.ClassificationUnspecified }),
		"provenance":        mutate(func(e *snapshot.InputEntry) { e.Provenance = snapshot.Provenance{} }),
		"reference_version": mutate(func(e *snapshot.InputEntry) { e.ReferenceVersion = "" }),
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			if err := entry.Validate(); !errors.Is(err, snapshot.ErrEntryIncomplete) {
				t.Fatalf("missing %s: Validate() = %v, want ErrEntryIncomplete", name, err)
			}
			if entry.Canonical() != nil {
				t.Fatalf("missing %s: Canonical() of an invalid entry is non-nil", name)
			}
		})
	}
}
