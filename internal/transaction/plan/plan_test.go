package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/governance/decision"
	"github.com/monstercameron/hcm-next/internal/governance/revalidate"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type headBook struct {
	mu    sync.RWMutex
	heads map[string]Head
}

func (b *headBook) CurrentHead(_ context.Context, tenant values.TenantId, stream string) (Head, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	head, ok := b.heads[stream]
	if !ok {
		return Head{}, fmt.Errorf("head %s not found", stream)
	}
	head.Tenant = tenant
	head.StreamKey = stream
	return head, nil
}

func (b *headBook) set(stream string, sequence int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	head := b.heads[stream]
	head.Sequence = sequence
	head.StreamKey = stream
	b.heads[stream] = head
}

type promotionFixture struct {
	tenant  values.TenantId
	streams []string
	book    *headBook
	request PrepareRequest
}

func newPromotionFixture(t *testing.T) promotionFixture {
	t.Helper()
	tenant := values.TenantId("00000000-0000-4000-8000-000000000001")
	streams := []string{"promotion:job", "promotion:grade", "promotion:position", "promotion:compensation"}
	sequences := []int64{7, 3, 11, 29}
	book := &headBook{heads: make(map[string]Head, len(streams))}
	writes := make([]intent.PlannedWrite, 0, len(streams))
	events := make([]PlannedEvent, 0, len(streams))
	baselines := make([]intent.SourceBaseline, 0, len(streams))
	for i, stream := range streams {
		resource, err := values.NewResourceKey(tenant, values.Kind([]string{"job", "grade", "position", "compensation"}[i]), "promotion-resource")
		if err != nil {
			t.Fatal(err)
		}
		revision, err := values.NewSequenceRevision(stream, uint64(sequences[i]))
		if err != nil {
			t.Fatal(err)
		}
		book.heads[stream] = Head{Tenant: tenant, StreamKey: stream, Sequence: sequences[i], Digest: fmt.Sprintf("%064x", i+1), DigestAlgorithm: "sha256"}
		writes = append(writes, intent.PlannedWrite{
			Subject:     intent.SubjectReference{Kind: "worker", SubjectID: "worker-jane", AuthorityDomain: "people"},
			ResourceKey: resource, FieldPath: "current_value", CurrentCanonicalText: "old", ProposedCanonicalText: "new",
			SourceAuthorityDecision: "authority:promotion/v1", ExpectedRevision: revision,
		})
		events = append(events, PlannedEvent{StreamKey: stream, EventType: "PromotionFact", SchemaRef: "hcmnext.promotion." + stream[stringsIndex(stream, ':')+1:] + "/v1", Digest: fmt.Sprintf("%064x", i+100)})
		baselines = append(baselines, intent.SourceBaseline{StreamID: stream, ExpectedRevision: revision})
	}
	proposalDigest := "sha256:" + repeatHex('a')
	proposal := intent.ProposalRevision{
		ProposalRevisionID: "proposal:promotion:1", IntentID: "intent:promotion:1", Revision: 1,
		Tenant: tenant, OrganizationScopeID: "org:engineering", Subjects: []intent.SubjectReference{{Kind: "worker", SubjectID: "worker-jane", AuthorityDomain: "people"}},
		MaterialDigest: digest.Reference{ProfileID: "PROPOSAL", ProfileVersion: 1, SchemaID: "proposal", SchemaVersion: 1, AlgorithmID: "sha256", Digest: proposalDigest},
		Writes:         writes, SourceBaselines: baselines,
		Effects: []intent.PlannedEffect{{EffectID: "payroll", Kind: "external", DestinationRef: "payroll", ObservationRef: "payroll-observation"}, {EffectID: "access", Kind: "external", DestinationRef: "access", ObservationRef: "access-observation"}},
	}
	governance, err := decision.Compose(decision.Inputs{
		ProposalRevisionDigest: proposalDigest,
		Context:                decision.Context{Principal: "principal:hr", Delegation: "none", Capability: "promotion.execute", Resource: "worker-jane", Fields: []string{"current_value"}, CurrentOrganization: "org:engineering", TargetOrganization: "org:engineering", Purpose: "promotion", Risk: "low", Authority: "people", Legal: "legal:promotion"},
		ControlSnapshot:        decision.ControlSnapshot{Digest: "sha256:" + repeatHex('b')},
		Subdecisions:           []decision.Subdecision{{ID: "all", Source: "all", State: decision.Allow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := PrepareRequest{
		Proposal: proposal, GovernanceDecision: governance, Events: events,
		OutboxEffects:  []OutboxEffect{{EffectID: "access", DestinationRef: "access", SchemaRef: "hcmnext.access.recalculate/v1", PayloadDigest: "sha256:" + repeatHex('c'), IdempotencyKey: "effect:access:1"}, {EffectID: "payroll", DestinationRef: "payroll", SchemaRef: "hcmnext.payroll.sync/v1", PayloadDigest: "sha256:" + repeatHex('d'), IdempotencyKey: "effect:payroll:1"}},
		IdempotencyKey: "promotion:execute:1", Now: instant(t, "2026-09-05T12:00:00Z"), ExpiresAt: instant(t, "2026-09-05T13:00:00Z"), PlanID: "tx:promotion:1",
	}
	return promotionFixture{tenant: tenant, streams: streams, book: book, request: request}
}

func instant(t *testing.T, text string) values.Instant {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(at)
}

func repeatHex(ch byte) string {
	return stringRepeat(ch, 64)
}

func stringRepeat(ch byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = ch
	}
	return string(buf)
}

func stringsIndex(s string, sep byte) int {
	for i := range s {
		if s[i] == sep {
			return i
		}
	}
	return 0
}

func TestTodo_TX_003(t *testing.T) {
	f := newPromotionFixture(t)
	prepared, err := Prepare(context.Background(), f.book, f.request)
	if err != nil {
		t.Fatalf("prepare promotion plan: %v", err)
	}
	if len(prepared.Streams) != 4 || len(prepared.Events) != 4 || len(prepared.Writes) != 4 || len(prepared.OutboxEffects) != 2 {
		t.Fatalf("prepared dimensions = streams %d/events %d/writes %d/effects %d, want 4/4/4/2", len(prepared.Streams), len(prepared.Events), len(prepared.Writes), len(prepared.OutboxEffects))
	}
	if prepared.Digest == "" || prepared.GovernanceDecisionDigest != f.request.GovernanceDecision.Digest {
		t.Fatalf("prepared digest/governance binding = %q/%q", prepared.Digest, prepared.GovernanceDecisionDigest)
	}
	if err := prepared.Verify(context.Background(), f.book); err != nil {
		t.Fatalf("verify unchanged prepared plan: %v", err)
	}
	bound := revalidate.Result{PlanDigest: prepared.Digest}
	if err := VerifyBoundPlan(bound, prepared); err != nil {
		t.Fatalf("bind GOVERN-003 result: %v", err)
	}
	f.book.set(f.streams[2], 12)
	var stale StaleHeadError
	if err := prepared.Verify(context.Background(), f.book); !errors.As(err, &stale) || stale.StreamKey != f.streams[2] || stale.Code() != "STALE_HEAD" {
		t.Fatalf("stale verify error = %v, want STALE_HEAD for %s", err, f.streams[2])
	}
}

func TestTodo_TX_003_Golden(t *testing.T) {
	first := newPromotionFixture(t)
	second := newPromotionFixture(t)
	a, err := Prepare(context.Background(), first.book, first.request)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Prepare(context.Background(), second.book, second.request)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || string(a.CanonicalBytes()) != string(b.CanonicalBytes()) {
		t.Fatalf("equivalent promotion preparations differ: %s vs %s", a.Digest, b.Digest)
	}
	if got, want := a.Digest, "sha256:73117b5c129f4e4c0e82268ae5390742d38ff9dbf3a1c9748a936bb0ee7a7758"; got != want {
		t.Fatalf("promotion plan golden digest = %s, want %s", got, want)
	}
}

func TestTodo_TX_003_Race(t *testing.T) {
	f := newPromotionFixture(t)
	const workers = 16
	results := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prepared, err := Prepare(context.Background(), f.book, f.request)
			if err != nil {
				results <- err.Error()
				return
			}
			results <- prepared.Digest
		}()
	}
	wg.Wait()
	close(results)
	want := ""
	for got := range results {
		if len(got) < len("sha256:") || got[:len("sha256:")] != "sha256:" {
			t.Fatalf("concurrent preparation returned malformed result %q", got)
		}
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("concurrent preparation returned differing digests %q and %q", want, got)
		}
	}
}

func TestTodo_TX_003_Mutation(t *testing.T) {
	f := newPromotionFixture(t)
	prepared, err := Prepare(context.Background(), f.book, f.request)
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := prepared.Digest
	f.request.Proposal.Writes[0].ResourceKey.Segments[0] = "tampered"
	if prepared.Digest != originalDigest || prepared.Writes[0].ResourceKey.Segments[0] == "tampered" {
		t.Fatal("prepared plan shares mutable proposal storage")
	}
	prepared.Events[0].Digest = "tampered"
	if err := prepared.VerifyDigest(); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("tampered plan verification = %v, want ErrInvalidPlan", err)
	}
}

func TestTodo_TX_003_Integration(t *testing.T) {
	if os.Getenv(pgtest.EnvDatabaseURL) == "" {
		t.Skip("embedded PostgreSQL is unavailable in this environment; the adapter is exercised with an external pgtest server in CI")
	}
	db := pgtest.New(t)
	tenantID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, $2, 'cell-local', 'Plan test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID, "plan-test-tenant")
	for _, stream := range []string{"promotion:job", "promotion:grade", "promotion:position", "promotion:compensation"} {
		db.Exec(t, `INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref) VALUES ($1, $2, 'WORKER', 'worker-jane')`, tenantID, stream)
		db.Exec(t, `INSERT INTO stream_head (tenant_id, stream_key, head_sequence, head_digest, head_digest_algorithm) VALUES ($1, $2, 4, repeat('a', 64), 'sha256')`, tenantID, stream)
	}
	reader := NewLedgerHeadReader(db.Conn)
	head, err := reader.CurrentHead(context.Background(), values.TenantId(tenantID.String()), "promotion:position")
	if err != nil {
		t.Fatal(err)
	}
	if head.Sequence != 4 || head.DigestAlgorithm != "sha256" {
		t.Fatalf("ledger head = %+v, want sequence 4/sha256", head)
	}
}
