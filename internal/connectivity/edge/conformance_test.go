package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func edge008Limits() IngressLimits {
	return IngressLimits{ContractVersion: edge008Version, MaxBodyBytes: 1024, MaxHeaderBytes: 2048, MaxHeaderRead: 2 * time.Second, MaxBodyRead: 5 * time.Second, MaxActiveConnections: 4}
}

func edge008Attempt() IngressAttempt {
	return IngressAttempt{RequestID: "req-1", TenantID: "tenant-1", ContractVersion: edge008Version, BodyBytes: 128, HeaderBytes: 512, HeaderRead: time.Millisecond, BodyRead: time.Second, ActiveConnections: 1, TrustState: "VERIFIED"}
}

func TestTodo_EDGE_008(t *testing.T) {
	plan := EffectPlan{AuthoritativeRows: 3, BusinessEvents: 2, OutboxEntries: 2, HumanWork: 1, ProviderRequests: 1}
	attempt := edge008Attempt()
	attempt.BodyBytes = edge008Limits().MaxBodyBytes + 1
	decision, err := EvaluateIngress(edge008Limits(), attempt, plan)
	var rejection *Rejection
	if !errors.As(err, &rejection) || decision.Code != "EDGE_008_REJECTED" || rejection.Field != "body_bytes" || rejection.State != "oversized" || rejection.Version != edge008Version {
		t.Fatalf("unexpected oversized rejection: decision=%+v err=%v", decision, err)
	}
	if decision.Effects != (EffectPlan{}) {
		t.Fatalf("rejected request planned side effects: %+v", decision.Effects)
	}
	attempt = edge008Attempt()
	attempt.BodyRead = edge008Limits().MaxBodyRead + time.Nanosecond
	decision, err = EvaluateIngress(edge008Limits(), attempt, plan)
	if !errors.As(err, &rejection) || rejection.Field != "body_read" || rejection.State != "slow" || decision.Effects != (EffectPlan{}) {
		t.Fatalf("unexpected slow-client rejection: decision=%+v err=%v", decision, err)
	}
}

func FuzzTodo_EDGE_008(f *testing.F) {
	f.Add(int64(0), int64(100), int64(0))
	f.Add(int64(2048), int64(100), int64(0))
	f.Fuzz(func(t *testing.T, body, header, delayNanos int64) {
		attempt := edge008Attempt()
		attempt.BodyBytes, attempt.HeaderBytes = body, header
		if delayNanos < 0 {
			delayNanos = -delayNanos
		}
		attempt.BodyRead = time.Duration(delayNanos)
		decision, err := EvaluateIngress(edge008Limits(), attempt, EffectPlan{ProviderRequests: 1})
		if err != nil && decision.Effects != (EffectPlan{}) {
			t.Fatalf("rejected fuzz input retained effects: %+v", decision.Effects)
		}
		if body > edge008Limits().MaxBodyBytes && err == nil {
			t.Fatal("oversized body admitted")
		}
	})
}

func TestTodo_EDGE_008_Race(t *testing.T) {
	limits := edge008Limits()
	attempt := edge008Attempt()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := EvaluateIngress(limits, attempt, EffectPlan{BusinessEvents: 1})
			if err != nil || !decision.Allowed || decision.Effects.BusinessEvents != 1 {
				t.Errorf("concurrent evaluation failed: decision=%+v err=%v", decision, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_EDGE_008_Security(t *testing.T) {
	attempt := edge008Attempt()
	attempt.TrustState = "UNVERIFIED"
	decision, err := EvaluateIngress(edge008Limits(), attempt, EffectPlan{AuthoritativeRows: 1})
	var rejection *Rejection
	if !errors.As(err, &rejection) || rejection.Field != "trust_state" || decision.Effects != (EffectPlan{}) {
		t.Fatalf("unverified request was not fenced: decision=%+v err=%v", decision, err)
	}
	attempt = edge008Attempt()
	attempt.ContractVersion++
	decision, err = EvaluateIngress(edge008Limits(), attempt, EffectPlan{ProviderRequests: 1})
	if !errors.As(err, &rejection) || rejection.Field != "contract_version" || rejection.Version != edge008Version || decision.Effects != (EffectPlan{}) {
		t.Fatalf("version-skewed request was not fenced: decision=%+v err=%v", decision, err)
	}
}

func TestTodo_EDGE_008_Conformance(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	corpus := AttackCorpus{Version: "edge-008-corpus-2026-09-06", Cases: []AttackCase{
		{ID: "oversized-body", Category: "oversized", Attempt: func() IngressAttempt { a := edge008Attempt(); a.BodyBytes = 2048; return a }(), ExpectedState: "oversized"},
		{ID: "slow-header", Category: "slow-client", Attempt: func() IngressAttempt { a := edge008Attempt(); a.HeaderRead = 3 * time.Second; return a }(), ExpectedState: "slow"},
		{ID: "slow-body", Category: "slow-client", Attempt: func() IngressAttempt { a := edge008Attempt(); a.BodyRead = 6 * time.Second; return a }(), ExpectedState: "slow"},
		{ID: "unverified", Category: "trust", Attempt: func() IngressAttempt { a := edge008Attempt(); a.TrustState = "UNVERIFIED"; return a }(), ExpectedState: "unverified"},
	}}
	evidence, err := RunConformance(edge008Limits(), corpus, private)
	if err != nil || !evidence.Verify(private.Public().(ed25519.PublicKey)) {
		t.Fatalf("conformance evidence failed: evidence=%+v err=%v", evidence, err)
	}
	if evidence.Status != "READY" || evidence.Cases != 4 || evidence.Rejected != 4 || !strings.HasPrefix(evidence.EvidenceDigest, "sha256:") {
		t.Fatalf("incomplete release evidence: %+v", evidence)
	}
}

func TestTodo_EDGE_008_Mutation(t *testing.T) {
	corpus := AttackCorpus{Version: "corpus-v1", Cases: []AttackCase{{ID: "body", Category: "oversized", Attempt: func() IngressAttempt { a := edge008Attempt(); a.BodyBytes = 2048; return a }(), ExpectedState: "oversized"}}}
	one, err := corpus.Digest()
	if err != nil {
		t.Fatal(err)
	}
	corpus.Cases[0].Attempt.BodyBytes++
	two, err := corpus.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Fatal("corpus digest did not change after input mutation")
	}
}
