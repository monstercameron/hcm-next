package workspace

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssetNegotiationAndConditionalRequestHelpers(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{"gzip, deflate", true}, {"br, gzip;q=1", true}, {"*;q=0.5", true},
		{"gzip;q=0", false}, {"br", false}, {"", false},
	} {
		if got := acceptsGzip(test.value); got != test.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", test.value, got, test.want)
		}
	}
	etag := `"digest"`
	for _, value := range []string{etag, `W/"digest"`, `"other", "digest"`, "*"} {
		if !matchesETag(value, etag) {
			t.Errorf("matchesETag(%q, %q) = false", value, etag)
		}
	}
	if matchesETag(`"other"`, etag) {
		t.Fatal("unrelated entity tag matched")
	}
}

func TestBuiltWASMHasAValidPrecompressedRepresentation(t *testing.T) {
	raw, rawOK := asset(assetJourneyWasm)
	compressed, compressedOK := compressedAsset(assetJourneyWasm)
	if !rawOK || !compressedOK {
		t.Skip("generated browser bundle is not present in this source checkout")
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatal("precompressed WASM is not the transfer representation of the embedded module")
	}
	if len(compressed) >= len(raw) {
		t.Fatalf("compressed WASM is not smaller: compressed=%d raw=%d", len(compressed), len(raw))
	}
}

func TestAssetHandlerServesCompressedConditionalWASM(t *testing.T) {
	if _, ok := compressedAsset(assetJourneyWasm); !ok {
		t.Skip("generated browser bundle is not present in this source checkout")
	}
	handler, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathJourneyWasm, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("compressed asset = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Encoding") != "gzip" || recorder.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("compression headers = %v", recorder.Header())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, max-age=0, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	etag := recorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("asset has no validator")
	}

	revalidate := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathJourneyWasm, nil)
	revalidate.Header.Set("Authorization", "Bearer "+token)
	revalidate.Header.Set("Accept-Encoding", "gzip")
	revalidate.Header.Set("If-None-Match", etag)
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, revalidate)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional asset = %d with %d body bytes", notModified.Code, notModified.Body.Len())
	}
}
