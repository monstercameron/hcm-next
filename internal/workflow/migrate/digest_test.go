package migrate

import "testing"

func TestFrontierDigest_OrderIndependentAndSensitive(t *testing.T) {
	a := FrontierDigest([]string{"node-a", "node-b"})
	b := FrontierDigest([]string{"node-b", "node-a"})
	if a != b {
		t.Fatalf("FrontierDigest is order-sensitive: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("FrontierDigest length = %d, want 64 (hex-encoded sha256)", len(a))
	}

	c := FrontierDigest([]string{"node-a", "node-c"})
	if a == c {
		t.Fatal("two different frontiers digested identically")
	}

	empty := FrontierDigest(nil)
	if empty == a {
		t.Fatal("an empty frontier digested the same as a non-empty one")
	}
}

func TestFrontierDigest_DoesNotMutateItsInput(t *testing.T) {
	frontier := []string{"node-b", "node-a"}
	original := append([]string(nil), frontier...)
	_ = FrontierDigest(frontier)
	for i := range frontier {
		if frontier[i] != original[i] {
			t.Fatalf("FrontierDigest mutated its input: %v, want %v", frontier, original)
		}
	}
}
