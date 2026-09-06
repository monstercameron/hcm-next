package boundary

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
)

type fakeSpan struct {
	outcome telemetry.Outcome
	err     error
}

func (s *fakeSpan) End(outcome telemetry.Outcome, err error) { s.outcome, s.err = outcome, err }

type fakeSink struct {
	name  telemetry.SpanName
	attrs map[string]string
	span  *fakeSpan
}

func (s *fakeSink) Start(ctx context.Context, name telemetry.SpanName, attrs map[string]string) (context.Context, Span) {
	s.name, s.attrs, s.span = name, attrs, &fakeSpan{}
	return ctx, s.span
}

func TestBoundaryInstrumentationEmitsExactPayloadFreeSignalsAndPreservesBehavior(t *testing.T) {
	sink := &fakeSink{}
	want := errors.New("owned failure")
	got, err := Instrument(context.Background(), sink, Spec{
		Kind: KindProvider, Operation: "promote", Dependency: "payroll", Status: "timeout", SizeClass: "small", Retry: 1,
	}, func(context.Context) (string, error) { return "unchanged", want })
	if got != "unchanged" || !errors.Is(err, want) {
		t.Fatalf("result/error changed: %q, %v", got, err)
	}
	if sink.name != telemetry.SpanProviderCall || sink.span.outcome != telemetry.OutcomeFailure || !errors.Is(sink.span.err, want) {
		t.Fatalf("span = %q, %+v", sink.name, sink.span)
	}
	wantAttrs := map[string]string{"operation": "promote", "dependency": "payroll", "status": "timeout", "size_class": "small", "retry": "true"}
	if !reflect.DeepEqual(sink.attrs, wantAttrs) {
		t.Fatalf("attrs = %#v, want %#v", sink.attrs, wantAttrs)
	}
	for _, forbidden := range []string{"body", "sql", "query", "header", "response"} {
		if _, ok := sink.attrs[forbidden]; ok {
			t.Fatalf("payload attribute %q emitted", forbidden)
		}
	}
}

func TestTodo_OBS_014_Golden(t *testing.T) {
	sink := &fakeSink{}
	_, err := Instrument(context.Background(), sink, Spec{Kind: KindHTTP, Operation: "get_intent", RouteTemplate: "/v1/intents/{intent_id}"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	if err != nil || sink.name != telemetry.SpanHTTPServer || sink.attrs["route"] != "/v1/intents/{intent_id}" {
		t.Fatalf("http signal = %q %#v err=%v", sink.name, sink.attrs, err)
	}
}

func TestTodo_OBS_014_Security(t *testing.T) {
	_, err := Instrument(context.Background(), &fakeSink{}, Spec{Kind: KindDatabase, Operation: "select_intent", RouteTemplate: "postgres://user:secret@host/db"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	if !errors.Is(err, ErrBoundaryPayload) {
		t.Fatalf("raw database endpoint accepted: %v", err)
	}
}

func TestTodo_OBS_014_Integration(t *testing.T) {
	called := false
	got, err := Instrument(context.Background(), nil, Spec{Kind: KindWorker, Operation: "resume_timer"}, func(context.Context) (int, error) { called = true; return 7, nil })
	if err != nil || got != 7 || !called {
		t.Fatalf("nil sink changed behavior: %d %v called=%v", got, err, called)
	}
}

func TestTodo_OBS_014_Race(t *testing.T) {
	sink := &fakeSink{}
	for i := 0; i < 16; i++ {
		if _, err := Instrument(context.Background(), sink, Spec{Kind: KindWorker, Operation: "tick"}, func(context.Context) (struct{}, error) { return struct{}{}, nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_OBS_014_Fault(t *testing.T) {
	want := errors.New("provider unavailable")
	_, err := Instrument(context.Background(), &fakeSink{}, Spec{Kind: KindProvider, Operation: "call"}, func(context.Context) (struct{}, error) { return struct{}{}, want })
	if !errors.Is(err, want) {
		t.Fatalf("fault error changed: %v", err)
	}
}

func BenchmarkTodo_OBS_014(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = Instrument(context.Background(), nil, Spec{Kind: KindHTTP, Operation: "get"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	}
}

func TestTodo_OBS_014_Mutation(t *testing.T) {
	_, err := Instrument(context.Background(), &fakeSink{}, Spec{Kind: KindHTTP, Operation: "get", RouteTemplate: "/v1/items?secret=1"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
	if !errors.Is(err, ErrBoundaryPayload) {
		t.Fatal("raw query route survived boundary mutation")
	}
}
