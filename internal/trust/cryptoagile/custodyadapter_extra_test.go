package cryptoagile

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

func TestCustodyKeySource_RejectsUnmappedAndProviderErrors(t *testing.T) {
	provider := newFakeCustodyProvider()
	ks, err := NewCustodyKeySource(provider, testRequestContext(), map[string]custody.Handle{"missing-key": testHandle("missing-key")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ks.Sign("missing-key", []byte("m")); err == nil {
		t.Fatal("Sign propagated no provider error for an absent custody key")
	}
	if _, err := ks.Verify("missing-key", []byte("m"), []byte{1}); err == nil {
		t.Fatal("Verify propagated no provider error for an absent custody key")
	}
	if _, err := ks.Verify("unmapped", []byte("m"), []byte{1}); err == nil {
		t.Fatal("Verify accepted an unmapped suite")
	}
}
