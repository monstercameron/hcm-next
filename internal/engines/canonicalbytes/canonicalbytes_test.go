package canonicalbytes_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestWriterRefusesAStreamWithoutASchema(t *testing.T) {
	_, err := canonicalbytes.New("", 1).String("a", "b").Bytes()
	if !errors.Is(err, canonicalbytes.ErrSchemaRequired) {
		t.Fatalf("error = %v, want ErrSchemaRequired", err)
	}
}

func TestWriterAbandonsTheStreamWhenAValueCannotEncode(t *testing.T) {
	// The zero Money is unset, so its Canonical() is nil. The writer must
	// refuse rather than emit a stream that silently omits the amount.
	_, err := canonicalbytes.New("test", 1).
		String("before", "ok").
		Value("amount", values.Money{}).
		String("after", "ok").
		Bytes()
	if !errors.Is(err, canonicalbytes.ErrUnencodable) {
		t.Fatalf("error = %v, want ErrUnencodable", err)
	}
	if _, err := canonicalbytes.New("test", 1).String("", "v").Bytes(); !errors.Is(err, canonicalbytes.ErrTagRequired) {
		t.Fatalf("error = %v, want ErrTagRequired", err)
	}
}

func TestWriterDistinguishesFieldsThatWouldOtherwiseCollide(t *testing.T) {
	// Length-prefixing every tag and payload is what stops "ab"+"c" from
	// encoding the same bytes as "a"+"bc".
	left, err := canonicalbytes.New("test", 1).String("k", "ab").String("k", "c").Bytes()
	if err != nil {
		t.Fatalf("left: %v", err)
	}
	right, err := canonicalbytes.New("test", 1).String("k", "a").String("k", "bc").Bytes()
	if err != nil {
		t.Fatalf("right: %v", err)
	}
	if bytes.Equal(left, right) {
		t.Fatal("two different field sets produced the same bytes")
	}

	// A different schema over the same payload must not collide either.
	other, err := canonicalbytes.New("other", 1).String("k", "ab").String("k", "c").Bytes()
	if err != nil {
		t.Fatalf("other schema: %v", err)
	}
	if bytes.Equal(left, other) {
		t.Fatal("two schemas produced the same bytes for the same payload")
	}
}

func TestWriterSortsStringSetsSoMapOrderCannotLeakIn(t *testing.T) {
	forward, err := canonicalbytes.New("test", 1).SortedStrings("f", []string{"a", "b", "c"}).Bytes()
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	reversed, err := canonicalbytes.New("test", 1).SortedStrings("f", []string{"c", "b", "a"}).Bytes()
	if err != nil {
		t.Fatalf("reversed: %v", err)
	}
	if !bytes.Equal(forward, reversed) {
		t.Fatal("the input order changed the encoding of a set")
	}
}

func TestWriterEncodesPresenceAndAbsenceDistinctly(t *testing.T) {
	amount, err := values.NewMoney("1.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	present, err := canonicalbytes.New("test", 1).Optional("m", true, amount).Bytes()
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	absent, err := canonicalbytes.New("test", 1).Optional("m", false, values.Money{}).Bytes()
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if bytes.Equal(present, absent) {
		t.Fatal("a present value and an absent one encoded identically")
	}
}

func TestDigestIsStableAndAlgorithmTagged(t *testing.T) {
	build := func() *canonicalbytes.Writer {
		return canonicalbytes.New("test", 1).String("a", "1").Int("b", -2).Bool("c", true)
	}
	first, err := build().Digest()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := build().Digest()
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Fatalf("digest drifted: %s vs %s", first, second)
	}
	if len(first) != len(canonicalbytes.DigestAlgorithm)+1+64 {
		t.Fatalf("digest %q is not %s plus 64 hex characters", first, canonicalbytes.DigestAlgorithm)
	}
	different, err := canonicalbytes.New("test", 1).String("a", "1").Int("b", -3).Bool("c", true).Digest()
	if err != nil {
		t.Fatalf("different: %v", err)
	}
	if first == different {
		t.Fatal("a changed field left the digest unchanged")
	}
}

func TestNestedWriterPropagatesItsFailure(t *testing.T) {
	broken := canonicalbytes.New("inner", 1).Value("amount", values.Money{})
	_, err := canonicalbytes.New("outer", 1).Nested("inner", broken).Bytes()
	if !errors.Is(err, canonicalbytes.ErrUnencodable) {
		t.Fatalf("error = %v, want ErrUnencodable", err)
	}
}
