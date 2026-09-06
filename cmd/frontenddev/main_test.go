package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFrontendDevForwardsToLiveCellWithoutRenderingFixtures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Live-Cell", "true")
		_, _ = w.Write([]byte(r.URL.RequestURI() + " " + r.Header.Get("Authorization")))
	}))
	t.Cleanup(upstream.Close)
	target, _ := url.Parse(upstream.URL)
	server := httptest.NewServer(frontendHandler(target, "live-token"))
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/workspace/app/work?nav=collapsed")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.Header.Get("X-Live-Cell") != "true" || string(body) != "/workspace/app/work?nav=collapsed Bearer live-token" {
		t.Fatalf("gateway did not return the live cell response: header=%q body=%q", response.Header.Get("X-Live-Cell"), body)
	}
	if strings.Contains(string(body), "Maya Chen") {
		t.Fatal("development gateway rendered fixture content")
	}
}

func TestFrontendDevRedirectsLegacyPreviewRoutesToProductionWorkspace(t *testing.T) {
	target, _ := url.Parse("http://cell.invalid")
	request := httptest.NewRequest(http.MethodGet, "/app/home?nav=collapsed", nil)
	recorder := httptest.NewRecorder()
	frontendHandler(target, "").ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Location") != "/workspace/app/home?nav=collapsed" {
		t.Fatalf("legacy redirect = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}
