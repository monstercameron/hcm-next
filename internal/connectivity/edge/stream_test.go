package edge

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func edge009Policy() StreamPolicy {
	return StreamPolicy{ContractVersion: edge009Version, MaxStreams: 2, MaxMessageBytes: 1024, MaxMessagesPerMinute: 3, MaxReplayMessages: 2, MaxBufferedMessages: 2, MaxLifetime: time.Hour, MaxIdle: 10 * time.Minute, ReauthInterval: 5 * time.Minute}
}

func edge009Session() StreamSession {
	issued := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	return StreamSession{ID: "session-1", TenantID: "tenant-1", IssuedAt: issued, ExpiresAt: issued.Add(time.Hour)}
}

func edge009Request(at time.Time) StreamRequest {
	return StreamRequest{SessionID: "session-1", TenantID: "tenant-1", ContractVersion: edge009Version, At: at, MessageBytes: 100, BufferedMessages: 1}
}

func TestTodo_EDGE_009(t *testing.T) {
	gate, err := NewStreamGate(edge009Policy())
	if err != nil {
		t.Fatal(err)
	}
	session := edge009Session()
	if err := gate.RegisterSession(session); err != nil {
		t.Fatal(err)
	}
	opened, err := gate.Open(edge009Request(session.IssuedAt))
	if err != nil || !opened.Allowed || opened.ActiveStreams != 1 {
		t.Fatalf("open failed: decision=%+v err=%v", opened, err)
	}
	accepted, err := gate.Accept(edge009Request(session.IssuedAt.Add(time.Second)))
	if err != nil || !accepted.Allowed || accepted.Sequence != 1 {
		t.Fatalf("message failed: decision=%+v err=%v", accepted, err)
	}
	tooLarge := edge009Request(session.IssuedAt.Add(2 * time.Second))
	tooLarge.MessageBytes = edge009Policy().MaxMessageBytes + 1
	decision, err := gate.Accept(tooLarge)
	var rejection *StreamRejection
	if !errors.As(err, &rejection) || rejection.Field != "message_bytes" || rejection.State != "oversized" {
		t.Fatalf("oversized message was not bounded: decision=%+v err=%v", decision, err)
	}
	if err := gate.Close(session.ID); err != nil {
		t.Fatal(err)
	}
}

func FuzzTodo_EDGE_009(f *testing.F) {
	f.Add(int64(100), 0, 1)
	f.Add(int64(2048), 0, 1)
	f.Fuzz(func(t *testing.T, messageBytes int64, replayMessages, buffered int) {
		gate, err := NewStreamGate(edge009Policy())
		if err != nil {
			t.Fatal(err)
		}
		session := edge009Session()
		if err := gate.RegisterSession(session); err != nil {
			t.Fatal(err)
		}
		req := edge009Request(session.IssuedAt)
		req.MessageBytes, req.ReplayMessages, req.BufferedMessages = messageBytes, replayMessages, buffered
		decision, err := gate.Open(req)
		if messageBytes > edge009Policy().MaxMessageBytes && err == nil && decision.Allowed {
			t.Fatal("oversized stream input admitted")
		}
		if replayMessages > edge009Policy().MaxReplayMessages && err == nil && decision.Allowed {
			t.Fatal("unbounded replay admitted")
		}
	})
}

func TestTodo_EDGE_009_Security(t *testing.T) {
	gate, err := NewStreamGate(edge009Policy())
	if err != nil {
		t.Fatal(err)
	}
	session := edge009Session()
	if err := gate.RegisterSession(session); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Open(edge009Request(session.IssuedAt)); err != nil {
		t.Fatal(err)
	}
	if err := gate.Revoke(session.ID); err != nil {
		t.Fatal(err)
	}
	decision, err := gate.Accept(edge009Request(session.IssuedAt.Add(time.Second)))
	var rejection *StreamRejection
	if !errors.As(err, &rejection) || rejection.Field != "session" || rejection.State != "revoked" || decision.Allowed {
		t.Fatalf("revoked stream continued: decision=%+v err=%v", decision, err)
	}
	wrongTenant := edge009Request(session.IssuedAt)
	wrongTenant.TenantID = "tenant-2"
	decision, err = gate.Open(wrongTenant)
	if !errors.As(err, &rejection) || rejection.Field != "tenant_id" || rejection.State != "cross_tenant" || decision.Allowed {
		t.Fatalf("cross-tenant stream admitted: decision=%+v err=%v", decision, err)
	}
}

func TestTodo_EDGE_009_Recovery(t *testing.T) {
	gate, err := NewStreamGate(edge009Policy())
	if err != nil {
		t.Fatal(err)
	}
	session := edge009Session()
	if err := gate.RegisterSession(session); err != nil {
		t.Fatal(err)
	}
	reconnect := edge009Request(session.IssuedAt.Add(time.Minute))
	reconnect.Reconnect, reconnect.ReplayMessages = true, 2
	decision, err := gate.Open(reconnect)
	if err != nil || !decision.Allowed {
		t.Fatalf("bounded reconnect replay rejected: decision=%+v err=%v", decision, err)
	}
	if err := gate.Close(session.ID); err != nil {
		t.Fatal(err)
	}
	reconnect.ReplayMessages = edge009Policy().MaxReplayMessages + 1
	decision, err = gate.Open(reconnect)
	var rejection *StreamRejection
	if !errors.As(err, &rejection) || rejection.Field != "replay_messages" || rejection.State != "over_limit" || decision.Allowed {
		t.Fatalf("unbounded reconnect replay admitted: decision=%+v err=%v", decision, err)
	}
	if err := gate.RegisterSession(StreamSession{ID: "session-2", TenantID: "tenant-1", IssuedAt: session.IssuedAt, ExpiresAt: session.ExpiresAt}); err != nil {
		t.Fatal(err)
	}
	reauth := edge009Request(session.IssuedAt.Add(6 * time.Minute))
	reauth.SessionID = "session-2"
	decision, err = gate.Open(reauth)
	if !errors.As(err, &rejection) || rejection.Field != "reauthorization" || rejection.State != "required" || decision.Allowed {
		t.Fatalf("reauthorization was not required: decision=%+v err=%v", decision, err)
	}
	reauth.Reauthorized = true
	if decision, err = gate.Open(reauth); err != nil || !decision.Allowed {
		t.Fatalf("reauthorized reconnect rejected: decision=%+v err=%v", decision, err)
	}
}

func TestStreamGateSerializesConcurrentAdmission(t *testing.T) {
	gate, err := NewStreamGate(edge009Policy())
	if err != nil {
		t.Fatal(err)
	}
	session := edge009Session()
	if err := gate.RegisterSession(session); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan StreamDecision, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, _ := gate.Open(edge009Request(session.IssuedAt))
			results <- decision
		}()
	}
	wg.Wait()
	close(results)
	var opened int
	for decision := range results {
		if decision.Allowed {
			opened++
		}
	}
	if opened != 1 {
		t.Fatalf("concurrent opens admitted %d streams for one session", opened)
	}
}
