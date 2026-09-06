package subscription

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type deliveryProvider struct {
	mu       sync.Mutex
	attempts []DeliveryAttempt
	next     uint64
	err      error
}

func (p *deliveryProvider) Deliver(_ context.Context, attempt DeliveryAttempt) (ProviderReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts = append(p.attempts, attempt)
	if p.err != nil {
		return ProviderReceipt{}, p.err
	}
	p.next++
	return ProviderReceipt{ReceiptID: fmt.Sprintf("receipt-%d", p.next), Provider: "fixture", Accepted: true}, nil
}

type ambiguousDeliveryError struct{}

func (ambiguousDeliveryError) Error() string   { return "timeout after provider acceptance" }
func (ambiguousDeliveryError) Ambiguous() bool { return true }

func deliveryEnvelope(sequence uint64) CanonicalEnvelope {
	return CanonicalEnvelope{Tenant: "tenant-a", Kind: EventWorkerChanged, SchemaVersion: 1, SubjectRefs: []string{"worker:1"}, EffectiveAt: time.Unix(10, 0), KnownAt: time.Unix(11, 0), PayloadDigest: fmt.Sprintf("sha256:event-%d", sequence), ProvenanceRef: fmt.Sprintf("event:%d", sequence), Sequence: sequence}
}

func deliveryRequest(sequence uint64) DeliveryRequest {
	return DeliveryRequest{SubscriptionID: "sub-delivery", SubscriptionRevision: 2, Envelope: deliveryEnvelope(sequence), Destination: "endpoint:webhook", OrderingKey: "worker:1"}
}

func TestTodo_SUB_004(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	first, err := journal.DeliverNow(provider, deliveryRequest(1))
	if err != nil || first.Operation.State != OperationAcked || len(first.Operation.Attempts) != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	duplicate, err := journal.DeliverNow(provider, deliveryRequest(1))
	if err != nil || !duplicate.Duplicate || len(provider.attempts) != 1 {
		t.Fatalf("duplicate=%+v provider_attempts=%d err=%v", duplicate, len(provider.attempts), err)
	}
	second, err := journal.DeliverNow(provider, deliveryRequest(2))
	if err != nil || second.Operation.Request.Envelope.Sequence != 2 || len(provider.attempts) != 2 {
		t.Fatalf("second=%+v provider_attempts=%d err=%v", second, len(provider.attempts), err)
	}
	if got := len(journal.Operations("sub-delivery")); got != 2 {
		t.Fatalf("journal operations=%d", got)
	}
}

func TestTodo_SUB_004_Race(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	request := deliveryRequest(1)
	var wg sync.WaitGroup
	results := make(chan DeliveryResult, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := journal.DeliverNow(provider, request)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	accepted := 0
	for result := range results {
		if result.Operation.State == OperationAcked {
			accepted++
		}
	}
	for err := range errs {
		if err != nil && !errors.Is(err, ErrDeliveryInFlight) {
			t.Fatalf("concurrent delivery error=%v", err)
		}
	}
	if accepted == 0 || len(provider.attempts) != 1 {
		t.Fatalf("accepted=%d provider_attempts=%d", accepted, len(provider.attempts))
	}
}

func TestTodo_SUB_004_Integration(t *testing.T) {
	provider := &deliveryProvider{}
	journal := NewDeliveryJournal()
	if _, err := journal.DeliverNow(provider, deliveryRequest(2)); !errors.Is(err, ErrDeliveryGap) {
		t.Fatalf("gap error=%v", err)
	}
	if len(provider.attempts) != 0 {
		t.Fatal("gap called provider")
	}
	if _, err := journal.DeliverNow(provider, deliveryRequest(1)); err != nil {
		t.Fatal(err)
	}
	queued, err := journal.DeliverNow(provider, deliveryRequest(2))
	if err != nil || queued.Operation.State != OperationAcked || len(provider.attempts) != 2 {
		t.Fatalf("queued=%+v attempts=%d err=%v", queued, len(provider.attempts), err)
	}
}

func TestTodo_SUB_004_Fault(t *testing.T) {
	provider := &deliveryProvider{err: ambiguousDeliveryError{}}
	journal := NewDeliveryJournal()
	request := deliveryRequest(1)
	result, err := journal.DeliverNow(provider, request)
	if !errors.Is(err, ErrDeliveryAmbiguous) || !result.Operation.Ambiguous || len(result.Operation.Attempts) != 1 {
		t.Fatalf("ambiguous result=%+v err=%v", result, err)
	}
	provider.err = nil
	retry, err := journal.DeliverNow(provider, request)
	if err != nil || retry.Operation.State != OperationAcked || len(retry.Operation.Attempts) != 2 {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	conflict := request
	conflict.Envelope = deliveryEnvelope(2)
	conflict.IdempotencyKey = result.Operation.OperationID
	if _, err := journal.DeliverNow(provider, conflict); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("idempotency conflict=%v", err)
	}
}
