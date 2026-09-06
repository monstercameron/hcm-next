package otelmw

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry/boundary"
)

func TestTodo_OBS_014_Integration_AdapterPreservesBehavior(t *testing.T) {
	secretErr := errors.New("sql=SELECT secret")
	var called bool
	got, err := boundary.Instrument(context.Background(), nil, boundary.Spec{Kind: boundary.KindDatabase, Operation: "load_intent"}, func(context.Context) (int, error) {
		called = true
		return 42, secretErr
	})
	if got != 42 || !errors.Is(err, secretErr) || !called {
		t.Fatalf("callback contract changed: got=%d err=%v called=%v", got, err, called)
	}
}

func TestTodo_OBS_014_TransportAdapterKinds(t *testing.T) {
	for _, kind := range []boundary.Kind{boundary.KindHTTP, boundary.KindGRPC, boundary.KindDatabase, boundary.KindWorker, boundary.KindProvider} {
		_, err := boundary.Instrument(context.Background(), nil, boundary.Spec{Kind: kind, Operation: "stable_operation"}, func(context.Context) (struct{}, error) { return struct{}{}, nil })
		if err != nil {
			t.Fatalf("kind %q rejected: %v", kind, err)
		}
	}
	if telemetry.SpanHTTPServer == telemetry.SpanGRPCServer {
		t.Fatal("HTTP and gRPC semantic spans collapsed")
	}
}
