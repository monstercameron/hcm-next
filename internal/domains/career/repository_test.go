package career

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func repositoryRef(kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: values.Kind(kind), Id: id}
}

func repositoryRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func repositoryInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func repositoryPreference(t *testing.T, sequence uint64) CareerPreferenceProfileRevision {
	t.Helper()
	revision := CareerPreferenceProfileRevision{
		PreferenceID:   repositoryRef("career_preference", "00000000-0000-0000-0000-000000000001"),
		Revision:       repositoryRevision(t, "preference", sequence),
		Worker:         repositoryRef("worker", "00000000-0000-0000-0000-000000000002"),
		Mobility:       MobilityAny,
		TargetRoleRefs: []values.EntityRef{repositoryRef("career_target_role", "00000000-0000-0000-0000-000000000003")},
		Timeframe:      repositoryInterval(t), Visibility: VisibilityWorkerOnly,
	}
	value, err := NewCareerPreference(revision)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestTodo_PERSIST_CAREER_001_MemoryStore(t *testing.T) {
	store := NewMemoryStore()
	first := repositoryPreference(t, 1)
	if err := store.SavePreference(context.Background(), "tenant-1", first, values.UnspecifiedRevision()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadPreference(context.Background(), "tenant-1", first.PreferenceID.Id, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Revision.Equal(first.Revision) || loaded.CanonicalDigest != first.CanonicalDigest {
		t.Fatalf("loaded=%+v want=%+v", loaded, first)
	}
	if err := store.SavePreference(context.Background(), "tenant-1", first, values.UnspecifiedRevision()); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	second := first
	second.Revision = repositoryRevision(t, "preference", 2)
	second.Supersedes = first.Revision
	second, err = NewCareerPreference(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreference(context.Background(), "tenant-1", second, values.UnspecifiedRevision()); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("missing CAS=%v", err)
	}
	if err := store.SavePreference(context.Background(), "tenant-1", second, first.Revision); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListPreferences(context.Background(), "tenant-1", first.PreferenceID.Id)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%v err=%v", history, err)
	}
}

func TestTodo_PERSIST_CAREER_001_PortIncludesAllRevisionFamilies(t *testing.T) {
	var _ Store = NewMemoryStore()
	if StoreCodeOf(NewStoreError(StoreStaleCASCode, "stale")) != StoreStaleCASCode {
		t.Fatal("store code did not round-trip")
	}
}
