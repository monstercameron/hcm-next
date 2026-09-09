package workspace

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

func TestBearerFromRequestPrefersTheHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
	r.Header.Set("Authorization", "Bearer header-token")
	r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: "cookie-token"})
	if got := BearerFromRequest(r, true); got != "Bearer header-token" {
		t.Fatalf("BearerFromRequest = %q, want the presented header", got)
	}
}

func TestBearerFromRequestReadsTheCookieOnlyWithDevLogin(t *testing.T) {
	build := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
		r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: "cookie-token"})
		return r
	}
	if got := BearerFromRequest(build(), true); got != "Bearer cookie-token" {
		t.Errorf("with dev login on, BearerFromRequest = %q", got)
	}
	if got := BearerFromRequest(build(), false); got != "" {
		t.Errorf("with dev login off, BearerFromRequest = %q, want the empty string", got)
	}
}

func TestBearerFromRequestWithNothingToRead(t *testing.T) {
	if got := BearerFromRequest(nil, true); got != "" {
		t.Errorf("BearerFromRequest(nil) = %q", got)
	}
	r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
	if got := BearerFromRequest(r, true); got != "" {
		t.Errorf("a request with no credential returned %q", got)
	}
	r.Header.Set("Authorization", "   ")
	if got := BearerFromRequest(r, false); got != "" {
		t.Errorf("a blank header returned %q", got)
	}
	r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: ""})
	if got := BearerFromRequest(r, true); got != "" {
		t.Errorf("an empty cookie returned %q", got)
	}
}

// TestAdmissionMetadataLeavesAPresentedHeaderAlone pins the exact rule the
// workspace has always applied: the substitution happens only when the
// request presented no Authorization header of its own, and a header that is
// present but blank still counts as presented - it is a credential the caller
// chose to send, and admission, not this function, decides what it is worth.
func TestAdmissionMetadataLeavesAPresentedHeaderAlone(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
	r.Header.Set("Authorization", "")
	r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: "cookie-token"})
	md := AdmissionMetadata(r, true)
	values := md.Get(transport.AuthorizationMetadataKey)
	if len(values) != 1 || values[0] != "" {
		t.Fatalf("authorization metadata = %#v, want the request's own blank header", values)
	}
}

func TestAdmissionMetadataSubstitutesTheCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
	r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: "cookie-token"})

	md := AdmissionMetadata(r, true)
	values := md.Get(transport.AuthorizationMetadataKey)
	if len(values) != 1 || values[0] != "Bearer cookie-token" {
		t.Fatalf("authorization metadata = %#v, want the cookie's credential", values)
	}

	if got := AdmissionMetadata(r, false).Get(transport.AuthorizationMetadataKey); len(got) != 0 {
		t.Fatalf("with dev login off, authorization metadata = %#v, want none", got)
	}
}

// TestAdmissionMetadataDoesNotMutateTheRequest matters because the same
// request is served afterwards: the substitution is made on a clone, so a
// route that later reads r.Header sees what the caller actually sent.
func TestAdmissionMetadataDoesNotMutateTheRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
	r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: "cookie-token"})
	_ = AdmissionMetadata(r, true)
	if got := r.Header.Get("Authorization"); got != "" {
		t.Fatalf("the request's own headers were mutated: Authorization = %q", got)
	}
}

func TestAdmissionMetadataNilRequest(t *testing.T) {
	if got := AdmissionMetadata(nil, true).Keys(); len(got) != 0 {
		t.Fatalf("AdmissionMetadata(nil).Keys() = %#v", got)
	}
}

// TestAdmissionMetadataCarriesEveryOtherHeader guards against a substitution
// that quietly drops the diagnostic metadata admission also reads.
func TestAdmissionMetadataCarriesEveryOtherHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, PathJourney, nil)
	r.Header.Set(transport.RequestIDMetadataKey, "req-42")
	r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: "cookie-token"})
	md := AdmissionMetadata(r, true)
	if got := md.Get(transport.RequestIDMetadataKey); len(got) != 1 || got[0] != "req-42" {
		t.Fatalf("request-id metadata = %#v", got)
	}
}
