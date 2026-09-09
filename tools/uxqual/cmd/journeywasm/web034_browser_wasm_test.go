//go:build js && wasm

package main

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type web034WASMConn struct {
	ctx context.Context
}

func (c *web034WASMConn) Invoke(ctx context.Context, _ string, _, _ any, _ ...grpc.CallOption) error {
	c.ctx = ctx
	return nil
}

func (c *web034WASMConn) NewStream(ctx context.Context, _ *grpc.StreamDesc, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	c.ctx = ctx
	return &web034WASMStream{ctx: ctx}, nil
}

type web034WASMStream struct{ ctx context.Context }

func (s *web034WASMStream) Header() (metadata.MD, error) { return nil, nil }
func (s *web034WASMStream) Trailer() metadata.MD         { return nil }
func (s *web034WASMStream) CloseSend() error             { return nil }
func (s *web034WASMStream) Context() context.Context     { return s.ctx }
func (s *web034WASMStream) SendMsg(any) error            { return nil }
func (s *web034WASMStream) RecvMsg(any) error            { return nil }

// TestTodo_WEB_034_Browser proves the production WASM composition uses the
// same bounded adapter as native tests. The Go js/wasm compile below this
// test also links the actual GoGRPCBridge browser dialer in main_wasm.go.
func TestTodo_WEB_034_Browser(t *testing.T) {
	conn := &web034WASMConn{}
	service := newJourneyService(conn, journeyclient.Config{Bearer: "wasm-issued"})
	if _, err := service.ListJourneys(context.Background(), &journeyv1.ListJourneysRequest{}); err != nil {
		t.Fatalf("WASM-composed ListJourneys: %v", err)
	}
	md, ok := metadata.FromOutgoingContext(conn.ctx)
	if !ok || len(md.Get(journeyclient.AuthorizationHeader)) != 1 || md.Get(journeyclient.AuthorizationHeader)[0] != "Bearer wasm-issued" {
		t.Fatalf("WASM adapter authorization = %v, want the configured bearer", md.Get(journeyclient.AuthorizationHeader))
	}
}
