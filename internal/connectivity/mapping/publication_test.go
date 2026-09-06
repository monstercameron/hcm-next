package mapping

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func compiledPublication(t *testing.T, id string, version int) MappingProfileVersion {
	t.Helper()
	profile := validProfile()
	profile.MappingID = id
	profile.Version = version
	compiled, err := Compile(profile)
	if err != nil {
		t.Fatalf("Compile(%s@%d): %v", id, version, err)
	}
	return compiled
}

func publicationEvidence(author, approver string) PublicationEvidence {
	return PublicationEvidence{
		Author: author, Approver: approver, Reason: "approved after impact review",
		EvidenceRef: "evidence://mapping-review/1", PublishedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

func activationEvidence(approver, reason, ref string) ActivationEvidence {
	return ActivationEvidence{
		Approver: approver, Reason: reason, EvidenceRef: ref,
		At: time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC),
	}
}

func publishTwo(t *testing.T) (*PublicationRegistry, MappingProfileVersion, MappingProfileVersion) {
	t.Helper()
	registry := NewPublicationRegistry()
	v1 := compiledPublication(t, "worker", 1)
	v2 := compiledPublication(t, "worker", 2)
	if _, err := registry.Publish(v1, publicationEvidence("author-1", "approver-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(v2, publicationEvidence("author-2", "approver-2")); err != nil {
		t.Fatal(err)
	}
	return registry, v1, v2
}

func TestTodo_INTG_007(t *testing.T) {
	registry, v1, v2 := publishTwo(t)

	first, err := registry.Activate("worker", 1, activationEvidence("operator-1", "initial activation", "evidence://activate/1"))
	if err != nil {
		t.Fatalf("Activate v1: %v", err)
	}
	second, err := registry.Activate("worker", 2, activationEvidence("operator-2", "promote reviewed version", "evidence://activate/2"))
	if err != nil {
		t.Fatalf("Activate v2: %v", err)
	}
	if first.PreviousVersion != 0 || second.PreviousVersion != 1 || second.PreviousPublicationDigest != first.PublicationDigest {
		t.Fatalf("activation lineage = first previous %d, second previous %d/%s", first.PreviousVersion, second.PreviousVersion, second.PreviousPublicationDigest)
	}
	if second.Digest() == "" || second.Digest() == first.Digest() || second.Explain() == "" {
		t.Fatal("activation event is not independently digested and explained")
	}
	if _, err := registry.Activate("worker", 1, activationEvidence("operator-3", "stale activation", "evidence://activate/stale")); !errors.Is(err, ErrSupersededVersion) {
		t.Fatalf("Activate superseded v1 error = %v, want ErrSupersededVersion", err)
	}
	rolledBack, err := registry.Rollback("worker", 1, activationEvidence("rollback-approver", "restore prior compatible mapping", "evidence://rollback/1"))
	if err != nil {
		t.Fatalf("Rollback v1: %v", err)
	}
	if !rolledBack.Rollback || rolledBack.PreviousVersion != 2 || !registry.IsActive("worker", 1) {
		t.Fatalf("rollback = %+v; active v1 = %t", rolledBack, registry.IsActive("worker", 1))
	}
	if got := len(registry.History("worker")); got != 3 {
		t.Fatalf("history length = %d, want 3", got)
	}
	if active, ok := registry.Active("worker"); !ok || active.Version != v1.Version() || active.Compiled.Digest() != v1.Digest() {
		t.Fatalf("active = %+v, want v1 %s", active, v1.Digest())
	}
	if publications := registry.Publications("worker"); len(publications) != 2 || publications[0].Digest() == "" || publications[1].Digest() == "" {
		t.Fatalf("publications = %+v", publications)
	}

	_ = v2
}

func TestTodo_INTG_007_Property(t *testing.T) {
	registry := NewPublicationRegistry()
	for version := 1; version <= 8; version++ {
		compiled := compiledPublication(t, "property", version)
		if _, err := registry.Publish(compiled, publicationEvidence("author-"+string(rune('a'+version)), "approver-"+string(rune('a'+version)))); err != nil {
			t.Fatal(err)
		}
	}
	for version := 1; version <= 8; version++ {
		if _, err := registry.Activate("property", version, activationEvidence("operator", "ordered activation", "evidence://property")); err != nil {
			// A prior version is superseded after the next activation. The
			// active invariant still holds and rollback is the only way back.
			if !errors.Is(err, ErrSupersededVersion) {
				t.Fatalf("Activate v%d: %v", version, err)
			}
		}
		active, ok := registry.Active("property")
		if !ok || active.Version < 1 || active.Version > 8 {
			t.Fatalf("active after v%d = %+v, found=%t", version, active, ok)
		}
	}
	for i, event := range registry.History("property") {
		if event.Sequence != uint64(i+1) || event.Digest() == "" {
			t.Fatalf("history[%d] = %+v", i, event)
		}
	}
}

func TestTodo_INTG_007_Integration(t *testing.T) {
	registry, _, _ := publishTwo(t)
	if _, err := Publish(registry, compiledPublication(t, "worker", 1), publicationEvidence("author-1", "approver-1")); err != nil {
		t.Fatalf("idempotent Publish: %v", err)
	}
	if _, err := Activate(registry, "worker", 1, activationEvidence("operator", "initial", "evidence://integration")); err != nil {
		t.Fatalf("functional Activate: %v", err)
	}
	if active, err := registry.Resolve("worker"); err != nil || active.Version != 1 {
		t.Fatalf("Resolve = %+v, %v", active, err)
	}
	if _, err := Activate(registry, "worker", 2, activationEvidence("operator", "forward", "evidence://integration/forward")); err != nil {
		t.Fatalf("functional forward Activate: %v", err)
	}
	if _, err := Rollback(registry, "worker", 1, activationEvidence("operator", "restore prior", "evidence://integration/rollback")); err != nil {
		t.Fatalf("functional Rollback: %v", err)
	}
}

func TestTodo_INTG_007_Fault(t *testing.T) {
	registry := NewPublicationRegistry()
	compiled := compiledPublication(t, "fault", 1)
	cases := []struct {
		name     string
		evidence PublicationEvidence
		want     error
	}{
		{"missing author", PublicationEvidence{Approver: "approver", Reason: "reason", EvidenceRef: "ref"}, ErrInvalidPublication},
		{"self approval", publicationEvidence("same", "same"), ErrInvalidPublication},
		{"missing reason", PublicationEvidence{Author: "author", Approver: "approver", EvidenceRef: "ref"}, ErrInvalidPublication},
		{"missing evidence", PublicationEvidence{Author: "author", Approver: "approver", Reason: "reason"}, ErrInvalidPublication},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := registry.Publish(compiled, tc.evidence); !errors.Is(err, tc.want) {
				t.Fatalf("Publish error = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := registry.Activate("fault", 99, activationEvidence("operator", "unpublished", "ref")); !errors.Is(err, ErrUnpublishedVersion) {
		t.Fatalf("unpublished activation error = %v", err)
	}
	if _, err := registry.Activate("fault", 1, ActivationEvidence{Approver: "operator", EvidenceRef: "ref"}); !errors.Is(err, ErrInvalidActivation) {
		t.Fatalf("activation without reason error = %v", err)
	}
	if _, ok := registry.Active("fault"); ok || len(registry.History("fault")) != 0 {
		t.Fatal("failed activations changed active state or history")
	}
}

func TestTodo_INTG_007_Conformance(t *testing.T) {
	registry, _, _ := publishTwo(t)
	if _, err := registry.Activate("worker", 1, activationEvidence("operator-1", "initial", "ref-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Activate("worker", 2, activationEvidence("operator-2", "forward", "ref-2")); err != nil {
		t.Fatal(err)
	}
	event := registry.History("worker")[1]
	if event.PreviousVersion != 1 || event.PreviousPublicationDigest == "" || event.PublicationDigest == "" {
		t.Fatalf("event lacks lineage: %+v", event)
	}
	if !strings.Contains(Explain(event), "worker@2") || !strings.Contains(event.Explain(), event.Digest()) {
		t.Fatalf("explanation = %q", event.Explain())
	}
}

func TestTodo_INTG_007_Mutation(t *testing.T) {
	registry := NewPublicationRegistry()
	compiled := compiledPublication(t, "mutation", 1)
	published, err := registry.Publish(compiled, publicationEvidence("author", "approver"))
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := published.Digest()
	profile := published.Compiled.Profile()
	profile.TargetFields.([]TargetField)[0].Name = "mutated"
	profile.Mappings[0].SourceField = "mutated"
	again, ok := registry.Active("mutation")
	if ok || again.Digest() != "" {
		t.Fatal("unactivated publication appeared active")
	}
	readBack := registry.Publications("mutation")[0]
	if readBack.Digest() != originalDigest || readBack.Compiled.Profile().TargetFields.([]TargetField)[0].Name == "mutated" {
		t.Fatal("caller mutation changed immutable publication")
	}
}

func TestTodo_INTG_007_Race(t *testing.T) {
	registry, _, _ := publishTwo(t)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		version := 1
		if i%2 == 1 {
			version = 2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = registry.Activate("worker", version, activationEvidence("operator", "concurrent activation", "evidence://race"))
		}()
	}
	wg.Wait()
	active, ok := registry.Active("worker")
	if !ok || !registry.IsActive("worker", active.Version) {
		t.Fatalf("concurrent activations left no unique active version: %+v, %t", active, ok)
	}
	if active.Version != 1 && active.Version != 2 {
		t.Fatalf("unexpected active version %d", active.Version)
	}
}

func TestTodo_INTG_007_Recovery(t *testing.T) {
	registry, _, _ := publishTwo(t)
	if _, err := registry.Activate("worker", 1, activationEvidence("operator", "initial", "ref-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Activate("worker", 99, activationEvidence("operator", "failed candidate", "ref-failed")); !errors.Is(err, ErrUnpublishedVersion) {
		t.Fatalf("failed activation error = %v", err)
	}
	active, err := registry.Resolve("worker")
	if err != nil || active.Version != 1 {
		t.Fatalf("active after failed activation = %+v, %v", active, err)
	}
	rollback, err := registry.Rollback("worker", 1, activationEvidence("rollback-approver", "reassert prior active after failed activation", "ref-recovery"))
	if err != nil {
		t.Fatalf("recovery rollback: %v", err)
	}
	if !rollback.Rollback || rollback.PreviousVersion != 1 {
		t.Fatalf("recovery event = %+v", rollback)
	}
	active, err = registry.Resolve("worker")
	if err != nil || active.Version != 1 || len(registry.History("worker")) != 2 {
		t.Fatalf("recovered active/history = %+v, %v, %d", active, err, len(registry.History("worker")))
	}
}

func FuzzTodo_INTG_007(f *testing.F) {
	f.Add("fuzz", 1)
	f.Add("", 0)
	f.Fuzz(func(t *testing.T, mappingID string, version int) {
		registry := NewPublicationRegistry()
		profile := validProfile()
		profile.MappingID = mappingID
		profile.Version = version
		compiled, err := Compile(profile)
		if err != nil {
			return
		}
		_, _ = registry.Publish(compiled, publicationEvidence("author", "approver"))
	})
}
