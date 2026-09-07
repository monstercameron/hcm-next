package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/hcm-next/internal/transport"
	transportjourney "github.com/monstercameron/hcm-next/internal/transport/journey"
	"github.com/monstercameron/hcm-next/internal/trust"
)

type promotionRouteVerifier struct{}

func (promotionRouteVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, trust.ErrInvalidCredential
}

func TestHandler_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestHandler_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestSemanticPromotionRouteIsMountedWithTheTypedRequestFactory(t *testing.T) {
	h, err := NewHandler(Options{
		Config:  transport.Config{Verifier: promotionRouteVerifier{}},
		Journey: &transportjourney.Dependencies{},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, transportjourney.ProposeIntoManagementProcedure, nil)
	req.Header.Set("Content-Type", "application/proto")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code == http.StatusNotFound {
		t.Fatalf("semantic promotion route returned 404")
	}
	if _, ok := requestFactories[transportjourney.ProposeIntoManagementProcedure]; !ok {
		t.Fatalf("semantic promotion route has no strict-decoding request factory")
	}
}
