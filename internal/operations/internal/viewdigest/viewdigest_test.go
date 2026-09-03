package viewdigest

import (
	"strings"
	"testing"
)

func TestDigestDeterministic(t *testing.T) {
	a := New().String("k", "v").Int("n", 42).Digest()
	b := New().String("k", "v").Int("n", 42).Digest()
	if a != b {
		t.Fatalf("not deterministic %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("prefix %q", a)
	}
	if len(a) != 7+64 {
		t.Fatalf("len %d", len(a))
	}
}

func TestDigestOrderSensitive(t *testing.T) {
	a := New().String("a", "1").String("b", "2").Digest()
	b := New().String("b", "2").String("a", "1").Digest()
	if a == b {
		t.Fatal("order should affect digest")
	}
}

func TestBool(t *testing.T) {
	tr := New().Bool("flag", true).Digest()
	fl := New().Bool("flag", false).Digest()
	if tr == fl {
		t.Fatal("bool not distinguished")
	}
}

func TestUintAndInt(t *testing.T) {
	d1 := New().Uint("u", 123).Digest()
	d2 := New().Int("u", 123).Digest()
	if d1 != d2 {
		t.Fatalf("Uint vs Int mismatch %s vs %s", d1, d2)
	}
	d3 := New().Uint("u", 124).Digest()
	if d1 == d3 {
		t.Fatal("different uint same digest")
	}
}

func TestSortedStrings(t *testing.T) {
	a := New().SortedStrings("list", []string{"c", "a", "b"}).Digest()
	b := New().SortedStrings("list", []string{"a", "b", "c"}).Digest()
	if a != b {
		t.Fatal("sorted should be equal")
	}
	c := New().Strings("list", []string{"c", "a", "b"}).Digest()
	d := New().Strings("list", []string{"a", "b", "c"}).Digest()
	if c == d {
		t.Fatal("unsorted order should matter")
	}
}

func TestStringsEmpty(t *testing.T) {
	d := New().Strings("list", nil).Digest()
	if !strings.HasPrefix(d, "sha256:") {
		t.Fatal("bad digest")
	}
}

func TestChaining(t *testing.T) {
	b := New()
	if got := b.String("x", "1"); got != b {
		t.Fatal("chaining failed")
	}
	if got := b.Bool("y", true); got != b {
		t.Fatal("chaining bool")
	}
	if got := b.Int("z", 1); got != b {
		t.Fatal("chaining int")
	}
}
