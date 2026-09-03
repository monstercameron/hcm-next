package capability

import "testing"

func TestDoc_PackageExists(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("nil registry")
	}
}

func TestDoc_BootstrapNotNil(t *testing.T) {
	r, err := NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if r == nil {
		t.Fatal("nil")
	}
}
