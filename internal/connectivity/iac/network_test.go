package iac

import "testing"

func validNetwork() Network {
	return Network{
		CellID: "cell-a", TenantID: "tenant-a",
		Ingress:      []IngressRule{{Host: "api.example.test", PathPrefix: "/v1", Service: "api", CellID: "cell-a", TLS: true, TrustedProxyCIDRs: []string{"10.0.0.0/8"}, TrustedProxyHeaders: map[string]string{"X-Cell": "cell-a"}}},
		Dependencies: []ServiceDependency{{FromCell: "cell-a", FromService: "worker", ToCell: "cell-a", ToService: "api"}},
		DNSPins:      []DNSPin{{Name: "api.example.test", PinnedIPs: []string{"192.0.2.10"}, ObservedIPs: []string{"192.0.2.10"}}, {Name: "provider.example.test", PinnedIPs: []string{"192.0.2.20"}, ObservedIPs: []string{"192.0.2.20"}}},
		Egress:       []EgressRule{{CellID: "cell-a", TenantID: "tenant-a", Destination: "provider.example.test", Port: 443, Purpose: "read"}},
	}
}

func TestTodo_IAC_004(t *testing.T) {
	n := validNetwork()
	if err := Check(n); err != nil {
		t.Fatal(err)
	}
	if decision := Evaluate(n, Request{FromCell: "cell-a", FromService: "worker", ToCell: "cell-a", ToService: "api"}); !decision.Allowed {
		t.Fatalf("explicit dependency denied: %+v", decision)
	}
	if decision := Evaluate(n, Request{FromCell: "cell-a", FromService: "other", ToCell: "cell-b", ToService: "api"}); decision.Allowed {
		t.Fatalf("undeclared cross-cell route allowed: %+v", decision)
	}
}

func FuzzTodo_IAC_004(f *testing.F) {
	f.Add("api.example.test", "/v1", "cell-a")
	f.Fuzz(func(t *testing.T, host, path, cell string) {
		n := validNetwork()
		decision := Evaluate(n, Request{Host: host, Path: path, ToCell: cell, ToService: "api", TLS: true})
		if decision.Allowed && (host != "api.example.test" || cell != "cell-a") {
			t.Fatalf("fuzz admitted an unpinned route: %+v", decision)
		}
	})
}

func TestTodo_IAC_004_Integration(t *testing.T) {
	n := validNetwork()
	decision := Evaluate(n, Request{Host: "api.example.test", Path: "/v1/health", ToCell: "cell-a", ToService: "api", TLS: true, ProxyAddress: "10.1.2.3", ProxyHeaders: map[string]string{"X-Cell": "cell-a"}})
	if !decision.Allowed || decision.Code != "INGRESS_ALLOWED" {
		t.Fatalf("explicit ingress denied: %+v", decision)
	}
}

func TestTodo_IAC_004_Fault(t *testing.T) {
	n := validNetwork()
	n.DNSPins[0].ObservedIPs = []string{"192.0.2.11"}
	if err := Check(n); err == nil {
		t.Fatal("DNS rebinding policy defect was accepted")
	}
}

func TestTodo_IAC_004_Security(t *testing.T) {
	n := validNetwork()
	for _, req := range []Request{
		{Host: "api.example.test", Path: "/v1", ToCell: "cell-a", ToService: "api", TLS: true, ProxyAddress: "198.51.100.4", ProxyHeaders: map[string]string{"X-Cell": "cell-a"}},
		{Host: "api.example.test", Path: "/v1", ToCell: "cell-a", ToService: "api", TLS: true, ProxyAddress: "10.1.2.3", ProxyHeaders: map[string]string{"X-Cell": "cell-b"}},
		{FromCell: "cell-a", Destination: "*", Port: 443},
	} {
		if decision := Evaluate(n, req); decision.Allowed {
			t.Fatalf("security-negative request allowed: %+v", decision)
		}
	}
}
