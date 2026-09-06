// Command frontenddev is a same-origin development gateway to a live HCM
// Next cell. It contains no fixtures and renders no alternate application:
// HTML, WASM, gRPC-over-WebSocket traffic, authentication, and business data
// all come from the production server behind it.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultListen   = "127.0.0.1:8768"
	defaultUpstream = "http://127.0.0.1:8080"
	envDevBearer    = "HCMNEXT_DEV_BEARER"
)

func main() {
	listen := flag.String("listen", defaultListen, "local address for the frontend development gateway")
	upstreamText := flag.String("upstream", defaultUpstream, "live HCM Next HTTP cell origin")
	flag.Parse()
	upstream, err := url.Parse(*upstreamText)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		log.Fatalf("frontenddev: invalid upstream %q", *upstreamText)
	}
	server := &http.Server{Addr: *listen, Handler: frontendHandler(upstream, os.Getenv(envDevBearer)), ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(os.Stdout, "HCM Next live gateway: http://%s/ -> %s\n", *listen, upstream)
	log.Fatal(server.ListenAndServe())
}

func frontendHandler(upstream *url.URL, bearer string) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		if request.Header.Get("Authorization") == "" && strings.TrimSpace(bearer) != "" {
			request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "live HCM Next cell unavailable", http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			http.Redirect(w, r, "/workspace/app/home", http.StatusTemporaryRedirect)
			return
		case strings.HasPrefix(r.URL.Path, "/app/"):
			target := "/workspace" + r.URL.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}
