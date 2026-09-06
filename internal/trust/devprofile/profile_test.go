package devprofile

import "testing"

func TestLoopbackBoundary(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "127.2.3.4:8443", "[::1]:8768", "localhost:8080"} {
		if !IsLoopbackAddress(addr) {
			t.Errorf("IsLoopbackAddress(%q) = false", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", "[::]:8443", "dev.example:8768", "127.0.0.1"} {
		if IsLoopbackAddress(addr) {
			t.Errorf("IsLoopbackAddress(%q) = true", addr)
		}
	}
}
