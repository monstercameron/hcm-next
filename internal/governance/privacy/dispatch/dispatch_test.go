package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type fakePolicy struct {
	decision dlp.Decision
}

func (p fakePolicy) Evaluate(dlp.DecisionRequest) (dlp.Evaluation, error) {
	return dlp.Evaluation{Decision: p.decision}, nil
}

type fakeSender struct {
	calls   int
	payload []byte
}

func (s *fakeSender) Send(_ context.Context, payload []byte) error {
	s.calls++
	s.payload = append([]byte(nil), payload...)
	return nil
}

func snapshot(t *testing.T, kind SnapshotKind, destination string, decision dlp.Decision) PolicySnapshot {
	t.Helper()
	sealed, err := SealSnapshot(PolicySnapshot{
		Kind: kind, Version: "policy.v1", Processor: "processor.acme",
		Destination: destination, Region: "us-east", Classification: dlp.ClassPublic,
		Purpose: "promotion.dispatch", TransferAssessment: "transfer.v1",
		DLPDecision: decision,
	}, "sig-"+string(kind))
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func binding(t *testing.T, decision dlp.Decision) (Binding, SnapshotSet) {
	t.Helper()
	set := SnapshotSet{
		Processor: snapshot(t, SnapshotProcessor, "api.example.test", decision),
		Transfer:  snapshot(t, SnapshotTransfer, "api.example.test", decision),
		DLP:       snapshot(t, SnapshotDLP, "api.example.test", decision),
	}
	b, err := Bind(set)
	if err != nil {
		t.Fatal(err)
	}
	return b, set
}

// TestTodo_PRIV_003 proves a dispatch binds the current processor, transfer,
// route, classification, purpose and DLP decisions before it calls the only
// external-effect port.
func TestTodo_PRIV_003(t *testing.T) {
	b, current := binding(t, dlp.Allow)
	sender := &fakeSender{}
	receipt, err := Dispatch(context.Background(), b, current, Request{
		Principal: "principal-1", Destination: "api.example.test", Purpose: "promotion.dispatch",
		DataClasses: []dlp.DataClass{dlp.ClassPublic}, Payload: []byte("safe"),
	}, fakePolicy{decision: dlp.Allow}, sender)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || string(sender.payload) != "safe" {
		t.Fatalf("sender calls=%d payload=%q, want one safe send", sender.calls, sender.payload)
	}
	if receipt.OperationDigest != b.OperationDigest || receipt.PayloadDigest == "" || len(receipt.PolicyDigests) != 3 {
		t.Fatalf("incomplete receipt: %+v", receipt)
	}
}

func TestTodo_PRIV_003_Golden(t *testing.T) {
	b, current := binding(t, dlp.Allow)
	decision := Revalidate(b, current)
	if !decision.Allowed || decision.Code != "DISPATCH_ALLOWED" || decision.OperationDigest != b.OperationDigest {
		t.Fatalf("allowed decision = %+v", decision)
	}
	if Explain() == "" || Version() != 1 || decision.Explain() == "" {
		t.Fatal("missing contract explanation")
	}
}

func TestTodo_PRIV_003_Security(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SnapshotSet)
	}{
		{"destination changed", func(s *SnapshotSet) { s.DLP.Destination = "other.example.test" }},
		{"processor revoked", func(s *SnapshotSet) { s.Processor.Revoked = true }},
		{"transfer changed", func(s *SnapshotSet) { s.Transfer.Region = "eu-west" }},
		{"DLP refused", func(s *SnapshotSet) { s.DLP.DLPDecision = dlp.Refuse }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			planned, current := binding(t, dlp.Allow)
			// Seal the mutated snapshot as a current, internally valid release;
			// the planned binding must still reject its changed digest.
			switch tc.name {
			case "DLP refused":
				current.DLP.DLPDecision = dlp.Refuse
				current.DLP.Digest = DigestSnapshot(current.DLP)
			case "destination changed":
				tc.mutate(&current)
				current.DLP.Digest = DigestSnapshot(current.DLP)
			case "processor revoked":
				tc.mutate(&current)
				current.Processor.Digest = DigestSnapshot(current.Processor)
			case "transfer changed":
				tc.mutate(&current)
				current.Transfer.Digest = DigestSnapshot(current.Transfer)
			}
			sender := &fakeSender{}
			_, err := Dispatch(context.Background(), planned, current, Request{
				Principal: "principal-1", Destination: "api.example.test", Purpose: "promotion.dispatch",
				DataClasses: []dlp.DataClass{dlp.ClassPublic}, Payload: []byte("safe"),
			}, fakePolicy{decision: dlp.Allow}, sender)
			if !errors.Is(err, ErrEGRESSBlocked) || sender.calls != 0 {
				t.Fatalf("Dispatch err=%v calls=%d, want EGRESS_BLOCKED and zero calls", err, sender.calls)
			}
		})
	}
}

func TestTodo_PRIV_003_Mutation(t *testing.T) {
	b, current := binding(t, dlp.Allow)
	for name, mutate := range map[string]func(*Binding){
		"empty operation digest":  func(b *Binding) { b.OperationDigest = "" },
		"tampered planned digest": func(b *Binding) { b.Planned.DLP.Digest = "tampered" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := b
			mutate(&candidate)
			sender := &fakeSender{}
			_, err := Dispatch(context.Background(), candidate, current, Request{
				Principal: "principal-1", Destination: "api.example.test", Purpose: "promotion.dispatch",
				DataClasses: []dlp.DataClass{dlp.ClassPublic}, Payload: []byte("safe"),
			}, fakePolicy{decision: dlp.Allow}, sender)
			if err == nil || sender.calls != 0 {
				t.Fatalf("tampered binding err=%v calls=%d, want refusal before send", err, sender.calls)
			}
		})
	}
}
