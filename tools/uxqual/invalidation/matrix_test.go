package invalidation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport/productquery"
)

func TestTodo_WEB_035(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000001")
	var got Refresh
	client, err := New(testScope(subject), func(_ context.Context, refresh Refresh) error {
		got = refresh
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessage(t, 11, subject)}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Projection != "worker_summary" || got.SourceSequence != 11 || len(got.Subjects) != 1 || got.Subjects[0] != subject {
		t.Fatalf("refresh = %+v, want the authorized hint only", got)
	}
	if snapshot := client.Snapshot(); snapshot.Accepted != 1 || snapshot.Refetched != 1 || snapshot.Running || snapshot.LastSourceSequence != 11 {
		t.Fatalf("snapshot = %+v, want one committed refetch", snapshot)
	}
}

func TestTodo_WEB_035_Golden(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000002")
	message := productquery.InvalidationMessage{
		ContractVersion: 1,
		Tenant:          testTenant,
		Projection:      "worker_summary",
		SourceSequence:  11,
		Watermark:       10,
		Items:           []productquery.InvalidationItem{{Subject: subject, Revision: 11}},
	}
	want := `{"contract_version":1,"tenant":"acme","projection":"worker_summary","source_sequence":11,"watermark":10,"items":[{"subject":"eref:v1:acme:worker:00000000-0000-4000-8000-000000000002","revision":11}]}`
	encoded, err := Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != want {
		t.Fatalf("canonical message = %s, want %s", encoded, want)
	}
	decoded, err := Decode(encoded, len(encoded))
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != want {
		t.Fatalf("round-trip = %s, want %s", reencoded, want)
	}
}

// TestInvalidationRequiresCompleteFrames pins the transport-neutral half of
// the browser boundary. The exact Browser matrix test drives the syscall/js
// adapter under Node.
func TestInvalidationRequiresCompleteFrames(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000003")
	raw := testMessage(t, 11, subject)
	cut := len(raw) / 2
	var calls atomic.Int32
	client, err := New(testScope(subject), func(context.Context, Refresh) error {
		calls.Add(1)
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw[:cut], raw[cut:]}}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || client.Snapshot().Rejected != 2 {
		t.Fatalf("fragmented frame calls=%d snapshot=%+v, want two refusals", calls.Load(), client.Snapshot())
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("complete browser frame calls = %d, want 1", calls.Load())
	}
}

func TestTodo_WEB_035_Conformance(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000004")
	events := make(chan Event, 4)
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{
		MaxMessageBytes: DefaultMaxMessageBytes + 1,
		MaxQueue:        MaxQueue + 1,
		Observe:         func(event Event) { events <- event },
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.options.MaxMessageBytes != DefaultMaxMessageBytes || client.options.MaxQueue != DefaultMaxQueue {
		t.Fatalf("normalized options = %+v, want canonical finite defaults", client.options)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessage(t, 11, subject)}}); err != nil {
		t.Fatal(err)
	}
	got := receiveEvents(t, events, 3)
	want := []EventKind{EventAccepted, EventRefetched, EventClosed}
	for i := range want {
		if got[i].Kind != want[i] {
			t.Fatalf("event[%d] = %+v, want kind %q; all events=%+v", i, got[i], want[i], got)
		}
	}
	if got[0].Items != 1 || got[0].SourceSequence != 11 || got[1].SourceSequence != 11 {
		t.Fatalf("ordered events = %+v, want bounded version/count evidence", got)
	}
}

func TestTodo_WEB_035_Security(t *testing.T) {
	visible := testSubject("00000000-0000-4000-8000-000000000005")
	foreign := values.EntityRef{Tenant: values.TenantId("other"), Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000006"}
	unknown := append([]byte(nil), testMessage(t, 11, visible)...)
	unknown = []byte(strings.Replace(string(unknown), `"items":`, `"secret_member":"`+foreign.Id+`","items":`, 1))
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "foreign", raw: testMessageRevision(t, foreign.Tenant, "worker_summary", 11, 11, foreign)},
		{name: "stale", raw: testMessageRevision(t, testTenant, "worker_summary", 10, 11, visible)},
		{name: "unknown", raw: unknown},
	}
	var baseline []Event
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan Event, 2)
			var calls atomic.Int32
			client, err := New(testScope(visible), func(context.Context, Refresh) error {
				calls.Add(1)
				return nil
			}, Options{Observe: func(event Event) { events <- event }})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Run(context.Background(), &sliceStream{values: [][]byte{tc.raw}}); err != nil {
				t.Fatalf("Run returned a distinguishing error: %v", err)
			}
			got := receiveEvents(t, events, 2)
			if calls.Load() != 0 || client.Snapshot().Accepted != 0 || client.Snapshot().Rejected != 1 {
				t.Fatalf("calls=%d snapshot=%+v, want one generic refusal", calls.Load(), client.Snapshot())
			}
			if got[0].Kind != EventRejected || got[0].SourceSequence != 0 || got[1].Kind != EventClosed {
				t.Fatalf("events = %+v, want identifier-free rejection and closure", got)
			}
			if strings.Contains(fmt.Sprintf("%+v", got), foreign.Id) {
				t.Fatal("foreign identifier appeared in observability")
			}
			if baseline == nil {
				baseline = got
			} else if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", baseline) {
				t.Fatalf("%s events = %+v, want indistinguishable %+v", tc.name, got, baseline)
			}
		})
	}
	for _, raw := range [][]byte{nil, unknown, []byte(`{"tenant":"` + foreign.Id + `"}`)} {
		_, err := Decode(raw, DefaultMaxMessageBytes)
		if !errors.Is(err, ErrInvalidMessage) || err.Error() != ErrInvalidMessage.Error() || strings.Contains(err.Error(), foreign.Id) {
			t.Fatalf("Decode error = %q, want exact identifier-free sentinel", err)
		}
	}
}

func TestInvalidationAppliesOnlyAuthoritativeRefetchResult(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000007")
	authoritative := struct {
		sync.Mutex
		view string
	}{view: "fresh-authorized-view"}
	displayed := "old-view"
	raw := testMessage(t, 11, subject)
	if strings.Contains(string(raw), authoritative.view) {
		t.Fatal("hint unexpectedly carried display data")
	}
	client, err := New(testScope(subject), func(_ context.Context, refresh Refresh) error {
		if refresh.SourceSequence != 11 {
			return errors.New("unexpected refresh cursor")
		}
		authoritative.Lock()
		fresh := authoritative.view
		authoritative.Unlock()
		displayed = fresh
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
		t.Fatal(err)
	}
	if displayed != "fresh-authorized-view" {
		t.Fatalf("displayed = %q, want authoritative refetch result", displayed)
	}
}

func TestTodo_WEB_035_Fault(t *testing.T) {
	t.Run("failed refetch leaves cursor retryable", func(t *testing.T) {
		subject := testSubject("00000000-0000-4000-8000-000000000008")
		var attempts atomic.Int32
		client, err := New(testScope(subject), func(context.Context, Refresh) error {
			if attempts.Add(1) == 1 {
				return errors.New("temporary authoritative read failure")
			}
			return nil
		}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		raw := testMessageRevision(t, testTenant, "worker_summary", 11, 11, subject)
		if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
			t.Fatal(err)
		}
		if got := client.Snapshot(); got.LastSourceSequence != 10 || got.RefetchErrors != 1 || got.Refetched != 0 {
			t.Fatalf("failed-refetch snapshot = %+v, want uncommitted cursor", got)
		}
		if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
			t.Fatal(err)
		}
		if got := client.Snapshot(); got.LastSourceSequence != 11 || got.Accepted != 2 || got.Refetched != 1 {
			t.Fatalf("retry snapshot = %+v, want same sequence committed after success", got)
		}
	})

	t.Run("bounded message", func(t *testing.T) {
		subject := testSubject("00000000-0000-4000-8000-000000000009")
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{MaxMessageBytes: 10})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessage(t, 11, subject)}}); err != nil {
			t.Fatal(err)
		}
		if got := client.Snapshot(); got.Rejected != 1 || got.Accepted != 0 {
			t.Fatalf("oversized snapshot = %+v, want refusal", got)
		}
	})

	t.Run("transport error is redacted", func(t *testing.T) {
		subject := testSubject("00000000-0000-4000-8000-000000000010")
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
		if err != nil {
			t.Fatal(err)
		}
		secret := "subject-00000000-0000-4000-8000-000000000099"
		err = client.Run(context.Background(), &errorStream{err: errors.New(secret)})
		if !errors.Is(err, ErrStreamReceive) || err.Error() != ErrStreamReceive.Error() || strings.Contains(err.Error(), secret) {
			t.Fatalf("terminal error = %q, want exact redacted sentinel", err)
		}
	})
}
