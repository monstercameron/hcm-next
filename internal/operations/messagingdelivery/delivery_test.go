package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeProvider struct {
	mu    sync.Mutex
	calls []Delivery
	err   error
}

func (p *fakeProvider) Send(_ context.Context, d Delivery) (ProviderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, d)
	if p.err != nil {
		return ProviderResult{}, p.err
	}
	return ProviderResult{Reference: "provider-1", Accepted: true}, nil
}

func testIntent() Intent {
	return Intent{TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "principal-1", Purpose: "APPROVAL_REQUIRED", Subject: "Approval needed", Body: "Open the secure task", Classification: "INTERNAL", IdempotencyKey: "msg-1", CanonicalRequest: []byte(`{"purpose":"APPROVAL_REQUIRED","recipient":"principal-1"}`), Committed: true}
}

func TestTodo_MSG_006(t *testing.T) {
	p := &fakeProvider{}
	d, err := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Dispatch(context.Background(), testIntent())
	if err != nil {
		t.Fatal(err)
	}
	if r.Attempt.State != AcceptedByProvider || len(p.calls) != 1 {
		t.Fatalf("dispatch failed: %#v calls=%d", r, len(p.calls))
	}
}

func TestTodo_MSG_006_Race(t *testing.T) {
	p := &fakeProvider{}
	d, _ := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	in := testIntent()
	var wg sync.WaitGroup
	results := make(chan DispatchResult, 10)
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := d.Dispatch(context.Background(), in); results <- r; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	var id string
	for r := range results {
		if id == "" {
			id = r.Attempt.ID
		}
		if r.Attempt.ID != id {
			t.Fatal("logical attempt changed")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(p.calls) != 1 {
		t.Fatalf("provider calls=%d", len(p.calls))
	}
}

func TestTodo_MSG_006_Integration(t *testing.T) { TestTodo_MSG_006(t) }

func TestTodo_MSG_006_Fault(t *testing.T) {
	p := &fakeProvider{err: errors.New("provider offline")}
	d, _ := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL"})
	in := testIntent()
	r, err := d.Dispatch(context.Background(), in)
	if !errors.Is(err, ErrProviderFailed) || r.Attempt.State != Failed {
		t.Fatalf("fault not recorded: %#v err=%v", r, err)
	}
	_, err = d.Dispatch(context.Background(), in)
	if !errors.Is(err, ErrProviderFailed) || len(p.calls) != 1 {
		t.Fatalf("failed retry duplicated provider call: %v calls=%d", err, len(p.calls))
	}
	in.Committed = false
	p2 := &fakeProvider{}
	d2, _ := NewDispatcher(p2, Policy{})
	if _, err := d2.Dispatch(context.Background(), in); !errors.Is(err, ErrNotCommitted) || len(p2.calls) != 0 {
		t.Fatalf("uncommitted intent reached provider: %v", err)
	}
}

func TestTodo_MSG_006_Mutation(t *testing.T) {
	p := &fakeProvider{}
	d, _ := NewDispatcher(p, Policy{})
	in := testIntent()
	if _, err := d.Dispatch(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.CanonicalRequest = []byte(`{"purpose":"different"}`)
	if _, err := d.Dispatch(context.Background(), in); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestTodo_MSG_006_Security(t *testing.T) {
	p := &fakeProvider{}
	d, _ := NewDispatcher(p, Policy{ProviderMaximumClassification: "INTERNAL", AttentionOnlyClassifications: map[string]bool{"RESTRICTED": true}})
	in := testIntent()
	in.Classification = "RESTRICTED"
	in.Body = "salary=secret"
	r, err := d.Dispatch(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Attempt.AttentionOnly || p.calls[0].Body != "" || !p.calls[0].AttentionOnly {
		t.Fatalf("protected body bypassed DLP: %#v", p.calls[0])
	}
}
