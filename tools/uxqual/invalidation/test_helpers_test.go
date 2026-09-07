package invalidation

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport/productquery"
)

const testTenant = values.TenantId("acme")

func testSubject(id string) values.EntityRef {
	return values.EntityRef{Tenant: testTenant, Kind: values.Kind("worker"), Id: id}
}

func testScope(subjects ...values.EntityRef) Scope {
	return Scope{Tenant: testTenant, Projection: "worker_summary", Watermark: 10, SourceSequence: 10, Subjects: subjects}
}

func testMessage(t testing.TB, sequence uint64, subjects ...values.EntityRef) []byte {
	return testMessageRevision(t, testTenant, "worker_summary", sequence, sequence, subjects...)
}

func testMessageRevision(t testing.TB, tenant values.TenantId, projection string, sequence, revision uint64, subjects ...values.EntityRef) []byte {
	t.Helper()
	items := make([]productquery.InvalidationItem, 0, len(subjects))
	for _, subject := range subjects {
		items = append(items, productquery.InvalidationItem{Subject: subject, Revision: revision})
	}
	message := productquery.InvalidationMessage{ContractVersion: 1, Tenant: tenant, Projection: projection, SourceSequence: sequence, Watermark: sequence, Items: items}
	b, err := message.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type sliceStream struct {
	mu     sync.Mutex
	values [][]byte
}

func (s *sliceStream) Recv() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.values) == 0 {
		return nil, io.EOF
	}
	value := append([]byte(nil), s.values[0]...)
	s.values = s.values[1:]
	return value, nil
}

func (*sliceStream) Close() error { return nil }

type errorStream struct{ err error }

func (s *errorStream) Recv() ([]byte, error) { return nil, s.err }
func (*errorStream) Close() error            { return nil }

type nonCloseStream struct{}

func (*nonCloseStream) Recv() ([]byte, error) { select {} }

type blockingStream struct {
	closed     chan struct{}
	closeCalls atomic.Int32
}

func newBlockingStream() *blockingStream { return &blockingStream{closed: make(chan struct{})} }

func (s *blockingStream) Recv() ([]byte, error) {
	<-s.closed
	return nil, io.EOF
}

func (s *blockingStream) Close() error {
	if s.closeCalls.Add(1) != 1 {
		panic("transport closed twice")
	}
	close(s.closed)
	return nil
}

type channelStream struct {
	messages   chan []byte
	closed     chan struct{}
	closeCalls atomic.Int32
}

func newChannelStream(capacity int) *channelStream {
	return &channelStream{messages: make(chan []byte, capacity), closed: make(chan struct{})}
}

func (s *channelStream) Recv() ([]byte, error) {
	select {
	case raw := <-s.messages:
		return append([]byte(nil), raw...), nil
	case <-s.closed:
		return nil, io.EOF
	}
}

func (s *channelStream) Close() error {
	if s.closeCalls.Add(1) != 1 {
		panic("transport closed twice")
	}
	close(s.closed)
	return nil
}

func receiveEvents(t *testing.T, events <-chan Event, count int) []Event {
	t.Helper()
	got := make([]Event, 0, count)
	for len(got) < count {
		select {
		case event := <-events:
			got = append(got, event)
		case <-time.After(time.Second):
			t.Fatalf("received %d events, want %d", len(got), count)
		}
	}
	return got
}

func waitSnapshot(t *testing.T, client *Client, ready func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := client.Snapshot()
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	snapshot := client.Snapshot()
	t.Fatalf("snapshot condition not reached: %+v", snapshot)
	return Snapshot{}
}
