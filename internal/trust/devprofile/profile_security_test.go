package devprofile

import "testing"

func TestLoopbackHost_NormalizesLocalhostAndBracketedIPs(t *testing.T) {
	cases := map[string]bool{
		" LOCALHOST ": true, "[::1]": true, "127.0.0.1": true,
		"0.0.0.0": false, "[::]": false, "localhost.example": false, "not-an-ip": false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
	for _, addr := range []string{"localhost:8080", "[::1]:443", "127.0.0.1:1"} {
		if !IsLoopbackAddress(addr) {
			t.Errorf("IsLoopbackAddress(%q) = false", addr)
		}
	}
	for _, addr := range []string{"localhost", ":8080", "localhost:8080:1", "example.com:443", "127.0.0.1"} {
		if IsLoopbackAddress(addr) {
			t.Errorf("IsLoopbackAddress(%q) = true", addr)
		}
	}
}
