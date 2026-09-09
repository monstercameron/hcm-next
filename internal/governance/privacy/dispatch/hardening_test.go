package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type dispatchErrorPolicy struct {
	err      error
	decision dlp.Decision
}

func (p dispatchErrorPolicy) Evaluate(dlp.DecisionRequest) (dlp.Evaluation, error) {
	return dlp.Evaluation{Decision: p.decision}, p.err
}

type dispatchErrorSender struct {
	calls int
	err   error
}

func (s *dispatchErrorSender) Send(context.Context, []byte) error { s.calls++; return s.err }

func TestSnapshot_SealDigestAndValidate_AllBranches(t *testing.T) {
	if Version() != 1 || !strings.Contains(Explain(), "revalidated") {
		t.Fatalf("contract metadata missing: version=%d explain=%q", Version(), Explain())
	}
	if _, err := SealSnapshot(PolicySnapshot{}, " "); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("empty signature error = %v", err)
	}
	base := snapshot(t, SnapshotDLP, "api.example.test", dlp.Allow)
	if err := base.Validate(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	if len(DigestSnapshot(base)) != 64 {
		t.Fatalf("DigestSnapshot length = %d", len(DigestSnapshot(base)))
	}
	cases := []struct {
		name   string
		mutate func(PolicySnapshot) PolicySnapshot
	}{
		{"unknown kind", func(s PolicySnapshot) PolicySnapshot { s.Kind = "OTHER"; return s }},
		{"padded version", func(s PolicySnapshot) PolicySnapshot { s.Version = " policy.v1"; return s }},
		{"missing processor", func(s PolicySnapshot) PolicySnapshot { s.Processor = ""; return s }},
		{"missing destination", func(s PolicySnapshot) PolicySnapshot { s.Destination = ""; return s }},
		{"missing region", func(s PolicySnapshot) PolicySnapshot { s.Region = ""; return s }},
		{"invalid classification", func(s PolicySnapshot) PolicySnapshot { s.Classification = "SECRET"; return s }},
		{"missing purpose", func(s PolicySnapshot) PolicySnapshot { s.Purpose = ""; return s }},
		{"missing transfer assessment", func(s PolicySnapshot) PolicySnapshot { s.TransferAssessment = ""; return s }},
		{"invalid dlp decision", func(s PolicySnapshot) PolicySnapshot { s.DLPDecision = "MAYBE"; return s }},
		{"missing digest", func(s PolicySnapshot) PolicySnapshot { s.Digest = ""; return s }},
		{"missing signature", func(s PolicySnapshot) PolicySnapshot { s.Signature = ""; return s }},
		{"digest mismatch", func(s PolicySnapshot) PolicySnapshot { s.Digest = "tampered"; return s }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.mutate(base).Validate(); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("Validate() = %v, want ErrInvalidSnapshot", err)
			}
		})
	}
	changed := base
	changed.Purpose = "other"
	if DigestSnapshot(changed) == DigestSnapshot(base) {
		t.Fatal("DigestSnapshot ignored policy fields")
	}
}

func TestSnapshotSet_BindAndBindingValidate_AllBranches(t *testing.T) {
	b, set := binding(t, dlp.Allow)
	if err := set.Validate(); err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SnapshotSet)
	}{
		{"processor invalid", func(s *SnapshotSet) { s.Processor.Digest = "bad" }},
		{"transfer invalid", func(s *SnapshotSet) { s.Transfer.Digest = "bad" }},
		{"dlp invalid", func(s *SnapshotSet) { s.DLP.Digest = "bad" }},
		{"role mismatch", func(s *SnapshotSet) { s.Processor.Kind = SnapshotTransfer }},
		{"destination mismatch", func(s *SnapshotSet) { s.Transfer.Destination = "other" }},
		{"purpose mismatch", func(s *SnapshotSet) { s.Transfer.Purpose = "other" }},
		{"region mismatch", func(s *SnapshotSet) { s.Transfer.Region = "other" }},
		{"classification mismatch", func(s *SnapshotSet) { s.Transfer.Classification = dlp.ClassConfidential }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := set
			tc.mutate(&candidate)
			if err := candidate.Validate(); !errors.Is(err, ErrInvalidBinding) {
				t.Fatalf("Validate() = %v, want ErrInvalidBinding", err)
			}
		})
	}
	if _, err := Bind(SnapshotSet{}); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("Bind invalid set error = %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	for _, mutate := range []func(*Binding){func(x *Binding) { x.OperationDigest = "" }, func(x *Binding) { x.OperationDigest = "tampered" }, func(x *Binding) { x.Planned.DLP.Digest = "tampered" }} {
		candidate := b
		mutate(&candidate)
		if err := candidate.Validate(); !errors.Is(err, ErrInvalidBinding) {
			t.Fatalf("binding mutation error = %v, want ErrInvalidBinding", err)
		}
	}
}

func TestRevalidate_DecisionMatrixAndExplain(t *testing.T) {
	b, current := binding(t, dlp.Allow)
	allowed := Revalidate(b, current)
	if !allowed.Allowed || allowed.Code != "DISPATCH_ALLOWED" || !strings.Contains(allowed.Explain(), b.OperationDigest) {
		t.Fatalf("allowed decision = %+v", allowed)
	}
	cases := []struct {
		name   string
		mutate func(Binding, SnapshotSet) (Binding, SnapshotSet)
	}{
		{"invalid binding", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) { x.OperationDigest = "bad"; return x, y }},
		{"invalid current", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) { y.DLP.Digest = "bad"; return x, y }},
		{"processor changed", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.Processor.Purpose = "other"
			y.Processor.Digest = DigestSnapshot(y.Processor)
			return x, y
		}},
		{"processor signature changed", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.Processor.Signature = "new-signature"
			return x, y
		}},
		{"transfer changed", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.Transfer.Region = "eu-west"
			y.Transfer.Digest = DigestSnapshot(y.Transfer)
			return x, y
		}},
		{"dlp changed", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.DLP.Purpose = "other"
			y.DLP.Digest = DigestSnapshot(y.DLP)
			return x, y
		}},
		{"processor revoked", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.Processor.Revoked = true
			y.Processor.Digest = DigestSnapshot(y.Processor)
			return x, y
		}},
		{"transfer revoked", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.Transfer.Revoked = true
			y.Transfer.Digest = DigestSnapshot(y.Transfer)
			return x, y
		}},
		{"dlp revoked", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.DLP.Revoked = true
			y.DLP.Digest = DigestSnapshot(y.DLP)
			return x, y
		}},
		{"dlp refuses", func(x Binding, y SnapshotSet) (Binding, SnapshotSet) {
			y.DLP.DLPDecision = dlp.Refuse
			y.DLP.Digest = DigestSnapshot(y.DLP)
			return x, y
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y := tc.mutate(b, current)
			got := Revalidate(x, y)
			if got.Allowed || got.Code != ErrEGRESSBlocked.Error() || got.OperationDigest != x.OperationDigest {
				t.Fatalf("decision = %+v, want blocked", got)
			}
		})
	}
}

func TestDispatch_RequestPolicySenderAndStateGuards(t *testing.T) {
	b, current := binding(t, dlp.Allow)
	valid := Request{Principal: "principal-1", Destination: "api.example.test", Purpose: "promotion.dispatch", DataClasses: []dlp.DataClass{dlp.ClassPublic}, Payload: []byte("payload")}
	sender := &fakeSender{}
	receipt, err := Dispatch(context.Background(), b, current, valid, fakePolicy{decision: dlp.Allow}, sender)
	if err != nil || sender.calls != 1 || receipt.PayloadDigest != dlp.DigestPayload(valid.Payload) || receipt.FindingsDigest != valid.Inspection.Digest() {
		t.Fatalf("valid dispatch = receipt=%+v err=%v calls=%d", receipt, err, sender.calls)
	}
	if len(receipt.PolicyDigests) != 3 {
		t.Fatalf("policy digests = %v", receipt.PolicyDigests)
	}
	invalidRequests := []struct {
		name    string
		request Request
		want    error
	}{
		{"nil context", valid, ErrInvalidRequest},
		{"canceled context", valid, context.Canceled},
		{"nil sender", valid, ErrInvalidRequest},
		{"nil policy", valid, ErrInvalidRequest},
		{"empty principal", func() Request { r := valid; r.Principal = ""; return r }(), ErrInvalidRequest},
		{"padded destination", func() Request { r := valid; r.Destination = " api.example.test"; return r }(), ErrInvalidRequest},
		{"empty purpose", func() Request { r := valid; r.Purpose = ""; return r }(), ErrInvalidRequest},
	}
	for _, tc := range invalidRequests {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeSender{}
			ctx := context.Background()
			if tc.name == "nil context" {
				ctx = nil
			}
			if tc.name == "canceled context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			var p DLPDecisioner = fakePolicy{decision: dlp.Allow}
			var send Sender = s
			if tc.name == "nil sender" {
				send = nil
			}
			if tc.name == "nil policy" {
				p = nil
			}
			_, err := Dispatch(ctx, b, current, tc.request, p, send)
			if tc.name == "canceled context" {
				if !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("error = %v, want ErrInvalidRequest", err)
				}
			} else if !errors.Is(err, tc.want) || s.calls != 0 {
				t.Fatalf("error=%v calls=%d, want %v and zero calls", err, s.calls, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Request)
	}{{"route", func(r *Request) { r.Destination = "other" }}, {"purpose", func(r *Request) { r.Purpose = "other" }}} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.mutate(&r)
			s := &fakeSender{}
			_, err := Dispatch(context.Background(), b, current, r, fakePolicy{decision: dlp.Allow}, s)
			if !errors.Is(err, ErrEGRESSBlocked) || s.calls != 0 {
				t.Fatalf("error=%v calls=%d, want blocked", err, s.calls)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		policy DLPDecisioner
		sender Sender
		want   error
	}{{"policy error", dispatchErrorPolicy{err: fmt.Errorf("policy failed"), decision: dlp.Allow}, &fakeSender{}, ErrEGRESSBlocked}, {"policy refuses", dispatchErrorPolicy{decision: dlp.Refuse}, &fakeSender{}, ErrEGRESSBlocked}, {"sender error", fakePolicy{decision: dlp.Allow}, &dispatchErrorSender{err: errors.New("send failed")}, errors.New("send failed")}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Dispatch(context.Background(), b, current, valid, tc.policy, tc.sender)
			if err == nil || (tc.name != "sender error" && !errors.Is(err, tc.want)) || (tc.name == "sender error" && err.Error() != tc.want.Error()) {
				t.Fatalf("Dispatch error = %v, want %v", err, tc.want)
			}
		})
	}
}
