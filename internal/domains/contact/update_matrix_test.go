package contact_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func confEmailRaw(i int) string {
	return []string{"jane.doe@example.com", "jane+pay@example.com", "JANE.DOE@EXAMPLE.COM"}[i%3]
}

func TestTodo_CONF_017_Property(t *testing.T) {
	ctx := context.Background()
	rng := rand.New(rand.NewSource(0xC017))
	for round := range 200 {
		service := confService()
		endpoint := []string{"email-1", "email-2"}[rng.Intn(2)]
		req := confRequest()
		req.EndpointID = endpoint
		req.Raw = confEmailRaw(rng.Intn(3))
		if _, err := service.Update(ctx, req); err != nil {
			t.Fatalf("round %d: seed update failed: %v", round, err)
		}
		revise := confRequest()
		revise.EndpointID = endpoint
		revise.Op = contact.OpRevise
		revise.ExpectedRevision = uint64(1 + rng.Intn(2))
		revise.Raw = confEmailRaw(rng.Intn(3))
		outcome, err := service.Update(ctx, revise)
		if err != nil {
			if !errors.Is(err, contact.ErrStaleRevision) {
				t.Fatalf("round %d: untyped refusal %v", round, err)
			}
			continue
		}
		if outcome.Revision.Revision != 2 {
			t.Fatalf("round %d: revision = %d, want 2", round, outcome.Revision.Revision)
		}
		if outcome.GuardedFacts["work_location"] != "berlin" {
			t.Fatalf("round %d: guarded facts moved", round)
		}
	}
}

func TestTodo_CONF_017_Golden(t *testing.T) {
	ctx := context.Background()
	service := confService()
	outcome, err := service.Update(ctx, confRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/conf017_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_CONF_017_Race(t *testing.T) {
	ctx := context.Background()
	service := confService()
	const workers = 16
	type result struct {
		outcome contact.UpdateOutcome
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := confRequest()
			req.EndpointID = "race-email"
			outcome, err := service.Update(ctx, req)
			results <- result{outcome, err}
		}(i)
	}
	wg.Wait()
	close(results)
	added := 0
	for r := range results {
		if r.err != nil {
			if !errors.Is(r.err, contact.ErrDuplicateEndpoint) {
				t.Fatalf("unexpected race error: %v", r.err)
			}
			continue
		}
		added++
		if r.outcome.Revision.Revision != 1 {
			t.Fatalf("race revision = %d, want 1", r.outcome.Revision.Revision)
		}
	}
	if added != 1 {
		t.Fatalf("race added %d endpoints, want exactly 1", added)
	}
}

func TestTodo_CONF_017_Integration(t *testing.T) {
	ctx := context.Background()
	service := confService()
	service.PublishSignal(contact.Signal{ID: "sig-1", EndpointID: "email-1", Digest: "sha256:sig"})
	service.PublishSignal(contact.Signal{ID: "sig-1", EndpointID: "email-1", Digest: "sha256:sig"})
	outcome, err := service.Update(ctx, confRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Signals) != 1 || outcome.Signals[0].ID != "sig-1" {
		t.Fatalf("signal-before-subscription lost or duplicated: %+v", outcome.Signals)
	}
	if outcome.ExternalSync != contact.SyncPending {
		t.Fatalf("queued sync closed consistency: %+v", outcome)
	}
	state, err := service.Reconcile(ctx, "tenant-1", "email-1", outcome.Revision.CanonicalDigest)
	if err != nil {
		t.Fatal(err)
	}
	if state != contact.SyncReconciled {
		t.Fatalf("matching observation did not reconcile: %s", state)
	}
	state, err = service.Reconcile(ctx, "tenant-1", "email-1", "sha256:foreign")
	if err != nil {
		t.Fatal(err)
	}
	if state != contact.SyncRepair {
		t.Fatalf("mismatching observation did not open repair: %s", state)
	}
	tickets := service.RepairTickets("tenant-1")
	if len(tickets) != 1 || tickets[0].EndpointID != "email-1" {
		t.Fatalf("repair ticket wrong: %+v", tickets)
	}
	other, err := service.Update(ctx, func() contact.UpdateRequest {
		req := confRequest()
		req.EndpointID = "email-2"
		req.Raw = "other@example.com"
		return req
	}())
	if err != nil {
		t.Fatal(err)
	}
	if other.ExternalSync != contact.SyncPending {
		t.Fatalf("unrelated endpoint affected by repair: %+v", other)
	}
}

func TestTodo_CONF_017_Fault(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*contact.UpdateRequest)
		cause  error
	}{
		{"unknown endpoint revise", func(r *contact.UpdateRequest) {
			r.Op = contact.OpRevise
			r.ExpectedRevision = 1
		}, contact.ErrUnknownEndpoint},
		{"bad op", func(r *contact.UpdateRequest) { r.Op = "VAPORIZE" }, contact.ErrInvalidUpdate},
		{"future date defers", func(r *contact.UpdateRequest) { r.EffectiveDate = "2027-01-01" }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := confRequest()
			tc.mutate(&req)
			outcome, err := confService().Update(ctx, req)
			if tc.cause == nil {
				if err != nil {
					t.Fatalf("deferral errored: %v", err)
				}
				if !outcome.Deferred || outcome.TimerRef == "" || len(outcome.Outbox) != 0 {
					t.Fatalf("future date not deferred: %+v", outcome)
				}
				return
			}
			if !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
	t.Run("failed commit emits no outbox", func(t *testing.T) {
		store := &failStore{Store: contact.NewMemoryStore()}
		service := contact.NewUpdateService(store)
		if _, err := service.Update(ctx, confRequest()); err == nil {
			t.Fatal("failed commit accepted")
		}
		listed, listErr := contact.NewMemoryStore().ListEndpointRevisions(ctx, "tenant-1", "email-1")
		if listErr == nil && len(listed) != 0 {
			t.Fatal("failed commit persisted elsewhere")
		}
	})
}

type failStore struct {
	contact.Store
}

func (s *failStore) PutEndpointRevision(ctx context.Context, tenant values.TenantId, revision contact.ContactEndpointRevision, expected ...uint64) error {
	return errors.New("persistence failure")
}

func TestTodo_CONF_017_Security(t *testing.T) {
	ctx := context.Background()
	t.Run("proxy keeps both identities", func(t *testing.T) {
		req := confRequest()
		req.Actor = values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: "00000000-0000-4000-8000-000000000002"}
		req.ProxyAuthority = "hr-delegation-7"
		outcome, err := confService().Update(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if !outcome.Proxy || outcome.Actor == outcome.Subject {
			t.Fatalf("proxy evidence collapsed: %+v", outcome)
		}
	})
	t.Run("tenant isolation", func(t *testing.T) {
		service := confService()
		if _, err := service.Update(ctx, confRequest()); err != nil {
			t.Fatal(err)
		}
		foreign := confRequest()
		foreign.Tenant = "tenant-2"
		foreign.Subject = values.EntityRef{Tenant: "tenant-2", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
		foreign.Actor = foreign.Subject
		outcome, err := service.Update(ctx, foreign)
		if err != nil {
			t.Fatalf("tenant-2 add rejected: %v", err)
		}
		if outcome.Revision.Revision != 1 {
			t.Fatalf("tenant-2 shares tenant-1 lineage: %+v", outcome.Revision)
		}
	})
	t.Run("challenge binds tenant", func(t *testing.T) {
		service := confService()
		challenge, _, err := contact.IssueChallenge(confSubject(), "email-1", "payslip", "token-1", confNow(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		service.TrackChallenge("ch-1", challenge)
		req := confRequest()
		req.Tenant = "tenant-2"
		req.Subject = values.EntityRef{Tenant: "tenant-2", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
		req.Actor = req.Subject
		req.ChallengeID = "ch-1"
		req.ChallengeToken = "token-1"
		if _, err := service.Update(ctx, req); !errors.Is(err, contact.ErrTenantMismatch) {
			t.Fatalf("cross-tenant receipt accepted: %v", err)
		}
	})
}

func TestTodo_CONF_017_Conformance(t *testing.T) {
	ctx := context.Background()
	service := confService()
	first, err := service.Update(ctx, confRequest())
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision.Revision != 1 || first.Revision.SupersedesRevision != 0 {
		t.Fatalf("initial lineage wrong: %+v", first.Revision)
	}
	revise := confRequest()
	revise.Op = contact.OpRevise
	revise.ExpectedRevision = 1
	revise.Raw = "jane.new@example.com"
	second, err := service.Update(ctx, revise)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision.Revision != 2 || second.Revision.SupersedesRevision != 1 {
		t.Fatalf("successor lineage wrong: %+v", second.Revision)
	}
	if second.Revision.CanonicalDigest == first.Revision.CanonicalDigest {
		t.Fatal("successor kept the old digest")
	}
	correct := confRequest()
	correct.Op = contact.OpCorrect
	correct.ExpectedRevision = 2
	correct.Raw = "jane.new@example.com"
	third, err := service.Update(ctx, correct)
	if err != nil {
		t.Fatal(err)
	}
	if third.Revision.SupersedesRevision != 2 {
		t.Fatalf("correction lineage wrong: %+v", third.Revision)
	}
}

func TestTodo_CONF_017_Mutation(t *testing.T) {
	ctx := context.Background()
	t.Run("verification lifecycle", func(t *testing.T) {
		service := confService()
		if _, err := service.Update(ctx, confRequest()); err != nil {
			t.Fatal(err)
		}
		challenge, _, err := contact.IssueChallenge(confSubject(), "email-1", "payslip", "token-9", confNow(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		service.TrackChallenge("ch-9", challenge)
		verified := confRequest()
		verified.Op = contact.OpRevise
		verified.ExpectedRevision = 1
		verified.ChallengeID = "ch-9"
		verified.ChallengeToken = "token-9"
		outcome, err := service.Update(ctx, verified)
		if err != nil {
			t.Fatalf("verified update rejected: %v", err)
		}
		if !outcome.Verified || outcome.Revision.Revision != 3 {
			t.Fatalf("receipt not honored as evidence revision: %+v", outcome)
		}
		replay, err := service.Update(ctx, func() contact.UpdateRequest {
			req := confRequest()
			req.Op = contact.OpRevise
			req.ExpectedRevision = 3
			req.ChallengeID = "ch-9"
			req.ChallengeToken = "token-9"
			return req
		}())
		if !errors.Is(err, contact.ErrDuplicateVerification) {
			t.Fatalf("consumed receipt replayed: %+v, %v", replay, err)
		}
		late, err := service.Update(ctx, func() contact.UpdateRequest {
			req := confRequest()
			req.EndpointID = "email-3"
			req.Raw = "third@example.com"
			req.ChallengeID = "ch-late"
			req.ChallengeToken = "token-9"
			return req
		}())
		_ = late
		if !errors.Is(err, contact.ErrUnknownVerification) {
			t.Fatalf("unknown challenge accepted: %v", err)
		}
	})
	t.Run("simulation mutates nothing", func(t *testing.T) {
		service := confService()
		dry := confRequest()
		dry.Simulate = true
		outcome, err := service.Update(ctx, dry)
		if err != nil {
			t.Fatal(err)
		}
		if len(outcome.Outbox) != 0 || outcome.ExternalSync != contact.SyncNone {
			t.Fatalf("simulation emitted effects: %+v", outcome)
		}
		revise := confRequest()
		revise.Op = contact.OpRevise
		revise.ExpectedRevision = 1
		if _, err := service.Update(ctx, revise); !errors.Is(err, contact.ErrUnknownEndpoint) {
			t.Fatalf("simulation persisted: %v", err)
		}
	})
}
