package grpcserver_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestNewServerRefusesAnUnsafeConfiguration proves that a server which could
// not enforce the trusted request boundary never starts.
//
// The failure mode this prevents is the dangerous one: a process that comes up
// happily with no verifier and serves every request as some default identity.
// Refusing at construction means such a server cannot exist even briefly.
func TestNewServerRefusesAnUnsafeConfiguration(t *testing.T) {
	verifier, err := transporttest.NewVerifier(func() time.Time { return time.Unix(1_800_000_000, 0) })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	valid := transport.Config{Verifier: verifier}

	t.Run("no verifier", func(t *testing.T) {
		_, err := grpcserver.NewServer(grpcserver.Options{
			Intent: &transporttest.IntentHandler{},
		})
		if !errors.Is(err, grpcserver.ErrNoVerifier) {
			t.Fatalf("NewServer without a verifier = %v, want ErrNoVerifier", err)
		}
	})

	t.Run("no handler ports", func(t *testing.T) {
		_, err := grpcserver.NewServer(grpcserver.Options{Config: valid})
		if !errors.Is(err, grpcserver.ErrNoHandlers) {
			t.Fatalf("NewServer without handlers = %v, want ErrNoHandlers", err)
		}
	})

	t.Run("either handler port alone is enough", func(t *testing.T) {
		for name, opts := range map[string]grpcserver.Options{
			"intent only":   {Config: valid, Intent: &transporttest.IntentHandler{}},
			"registry only": {Config: valid, Registry: &transporttest.RegistryHandler{}},
			"both":          {Config: valid, Intent: &transporttest.IntentHandler{}, Registry: &transporttest.RegistryHandler{}},
		} {
			server, err := grpcserver.NewServer(opts)
			if err != nil {
				t.Fatalf("NewServer(%s) = %v", name, err)
			}
			server.Stop()
		}
	})
}

// TestUnaryInterceptorRejectsAnUnauthenticatedCall exercises the exported
// interceptor directly, without a listener, so the chain's own behavior is
// pinned independently of grpc-go's plumbing.
func TestUnaryInterceptorRejectsAnUnauthenticatedCall(t *testing.T) {
	verifier, err := transporttest.NewVerifier(func() time.Time { return time.Unix(1_800_000_000, 0) })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	interceptor := grpcserver.UnaryInterceptor(transport.Config{Verifier: verifier})

	called := false
	_, err = interceptor(context.Background(), nil,
		&grpcServerInfo, func(context.Context, any) (any, error) {
			called = true
			return nil, nil
		})
	if err == nil {
		t.Fatal("the interceptor admitted a call with no credential")
	}
	if called {
		t.Fatal("the handler ran for an unauthenticated call")
	}
	if _, ok := trust.FromContext(context.Background()); ok {
		t.Fatal("a bare context should not carry a principal")
	}
}

// grpcServerInfo is the method descriptor the interceptor test passes in.
var grpcServerInfo = grpc.UnaryServerInfo{FullMethod: "/hcmnext.intents.v1.IntentService/GetIntent"}
