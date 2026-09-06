package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func publicResolver(ip net.IP) DNSResolver {
	return ResolverFunc(func(context.Context, string) ([]net.IP, error) { return []net.IP{ip}, nil })
}

func TestDestinationTrustResolverRejectsRebindingAndAllowsPublicAddress(t *testing.T) {
	trust := DestinationTrust{Schemes: []string{"https"}, Hosts: []string{"api.example.test"}}
	if err := trust.ValidateWithResolver(context.Background(), "https://api.example.test/v1", publicResolver(net.ParseIP("203.0.113.10"))); err != nil {
		t.Fatalf("public destination rejected: %v", err)
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.4", "169.254.169.254", "::1"} {
		t.Run(ip, func(t *testing.T) {
			err := trust.ValidateWithResolver(context.Background(), "https://api.example.test/v1", publicResolver(net.ParseIP(ip)))
			if err == nil {
				t.Fatalf("resolver address %s was accepted", ip)
			}
		})
	}
}

func TestSafeHTTPClientChecksRedirectsAndNormalizationNeverLeaksPayload(t *testing.T) {
	trust := DestinationTrust{Schemes: []string{"https"}, Hosts: []string{"api.example.test"}}
	client := NewSafeHTTPClient(trust, publicResolver(net.ParseIP("203.0.113.10")))
	if client.CheckRedirect == nil || client.Transport == nil {
		t.Fatal("safe client is missing redirect or transport enforcement")
	}
	if err := client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "evil.example.test"}}, nil); err == nil {
		t.Fatal("nil redirect request was accepted")
	}

	got := NormalizeHTTPError(KindREST, 429)
	if got.Kind != ErrRateLimited || got.Status != 429 {
		t.Fatalf("normalized 429 = %+v", got)
	}
	if strings.Contains(got.Error(), "secret") {
		t.Fatalf("normalized error leaked payload: %v", got)
	}
	raw := &Error{Kind: ErrTransport, Operation: KindREST, Cause: errors.New("secret response body")}
	redacted := NormalizeTransportError(raw)
	if strings.Contains(redacted.Error(), "secret") {
		t.Fatalf("redacted error leaked provider detail: %v", redacted)
	}
	if c := (Client{Trust: trust}).WithDNSResolver(publicResolver(net.ParseIP("203.0.113.10"))); c.HTTP == nil {
		t.Fatal("WithDNSResolver did not install the safe client")
	}
}

func TestTransportSecurityHelpersRemainBoundedAndCredentialFree(t *testing.T) {
	if FormatDestination("https://API.EXAMPLE.TEST:443/path?token=secret") != "https://api.example.test:443" {
		t.Fatalf("unexpected destination format: %q", FormatDestination("https://API.EXAMPLE.TEST:443/path?token=secret"))
	}
	if strings.Contains(Explain(), "secret") || strings.Contains(Explain(), "example.test") {
		t.Fatalf("Explain contains endpoint or secret: %q", Explain())
	}
	if AmbiguousTransportError(KindREST).Kind != ErrAmbiguous {
		t.Fatalf("ambiguous error lost its classification")
	}
}
