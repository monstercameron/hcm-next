package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// -- definition builders -----------------------------------------------

func newDef(source, dest []transformation.Field, ops []transformation.Operation, failure transformation.FailurePolicy) transformation.TransformationDefinition {
	return transformation.TransformationDefinition{
		Version: 1, Name: "tmap", Owner: "shared-engines", Phase: "P1A",
		Source:        transformation.Schema{Name: "src", Version: 1, Fields: source},
		Destination:   transformation.Schema{Name: "dst", Version: 1, Fields: dest},
		Operations:    ops,
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1, MaximumSourceVersion: 1},
		Limits:        transformation.ResourceLimits{MaxOperations: len(ops) + 4, MaxInputBytes: 4096, MaxOutputBytes: 4096, MaxExpansion: 1},
		Failure:       failure, SideEffects: transformation.SideEffectsNone,
	}
}

func srcPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "src", Field: field, Type: typ}
}
func dstPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "dst", Field: field, Type: typ}
}
func pathP(p transformation.Path) *transformation.Path { return &p }

func fullDef() transformation.TransformationDefinition {
	source := []transformation.Field{{Name: "first", Type: transformation.TypeString}, {Name: "last", Type: transformation.TypeString}, {Name: "amount", Type: transformation.TypeString}, {Name: "flag", Type: transformation.TypeString}, {Name: "born", Type: transformation.TypeString}}
	dest := []transformation.Field{{Name: "name", Type: transformation.TypeString}, {Name: "quantity", Type: transformation.TypeInt}, {Name: "approved", Type: transformation.TypeBool}, {Name: "dob", Type: transformation.TypeDate}, {Name: "note", Type: transformation.TypeString}, {Name: "unmapped", Type: transformation.TypeString}}
	ops := []transformation.Operation{
		{Kind: transformation.OpConcat, Destination: dstPath("name", transformation.TypeString), Sources: []transformation.Path{srcPath("first", transformation.TypeString), srcPath("last", transformation.TypeString)}},
		{Kind: transformation.OpConvert, Source: pathP(srcPath("amount", transformation.TypeString)), Destination: dstPath("quantity", transformation.TypeInt), TargetType: transformation.TypeInt},
		{Kind: transformation.OpConvert, Source: pathP(srcPath("flag", transformation.TypeString)), Destination: dstPath("approved", transformation.TypeBool), TargetType: transformation.TypeBool},
		{Kind: transformation.OpConvert, Source: pathP(srcPath("born", transformation.TypeString)), Destination: dstPath("dob", transformation.TypeDate), TargetType: transformation.TypeDate},
		{Kind: transformation.OpDefault, Destination: dstPath("note", transformation.TypeString), Literal: "n/a"},
	}
	return newDef(source, dest, ops, transformation.FailureReject)
}

func fullInput() Record {
	return Record{
		"first":  Present(transformation.TypeString, "Ada"),
		"last":   Present(transformation.TypeString, "Lovelace"),
		"amount": Present(transformation.TypeString, "42"),
		"flag":   Present(transformation.TypeString, "true"),
		"born":   Present(transformation.TypeString, "1815-12-10"),
	}
}

func convertDef() transformation.TransformationDefinition {
	source := []transformation.Field{{Name: "amount", Type: transformation.TypeString}}
	dest := []transformation.Field{{Name: "quantity", Type: transformation.TypeInt}}
	ops := []transformation.Operation{{Kind: transformation.OpConvert, Source: pathP(srcPath("amount", transformation.TypeString)), Destination: dstPath("quantity", transformation.TypeInt), TargetType: transformation.TypeInt}}
	return newDef(source, dest, ops, transformation.FailureReject)
}

func requireValue(t *testing.T, v Value, typ transformation.Type) {
	t.Helper()
	if v.State != values.PresenceValue {
		t.Fatalf("state=%v, want VALUE", v.State)
	}
	if v.Type != typ {
		t.Fatalf("type=%v, want %v", v.Type, typ)
	}
}

// -- XFORM-003 matrix --------------------------------------------------

// PRIMARY: one pass over the whole operation vocabulary, plus the RED
// criterion that an untended destination field lands as ABSENT, never NULL or
// a silent zero VALUE.
func TestTodo_XFORM_003(t *testing.T) {
	d := fullDef()
	if err := d.Validate(); err != nil {
		t.Fatalf("definition did not validate: %v", err)
	}
	out, err := Execute(d, fullInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	v := out["name"]
	requireValue(t, v, transformation.TypeString)
	if v.Data != "AdaLovelace" {
		t.Errorf("name=%v, want AdaLovelace", v.Data)
	}
	v = out["quantity"]
	requireValue(t, v, transformation.TypeInt)
	if v.Data.(int64) != 42 {
		t.Errorf("quantity=%v, want 42", v.Data)
	}
	v = out["approved"]
	requireValue(t, v, transformation.TypeBool)
	if v.Data != true {
		t.Errorf("approved=%v, want true", v.Data)
	}
	v = out["dob"]
	requireValue(t, v, transformation.TypeDate)
	if v.Data != "1815-12-10" {
		t.Errorf("dob=%v, want 1815-12-10", v.Data)
	}
	v = out["note"]
	requireValue(t, v, transformation.TypeString)
	if v.Data != "n/a" {
		t.Errorf("note=%v, want the declared default \"n/a\"", v.Data)
	}

	v = out["unmapped"]
	if v.State != values.PresenceAbsent || v.Data != nil {
		t.Fatalf("unmapped state=%v data=%v, want ABSENT with no data", v.State, v.Data)
	}
}

// PROPERTY: replay determinism. The same definition and input must encode to
// identical canonical bytes on every run, and the untended field must stay
// ABSENT on the wire (not drift to NULL).
func TestTodo_XFORM_003_Property(t *testing.T) {
	d := fullDef()
	var first []byte
	for i := 0; i < 3; i++ {
		out, err := Execute(d, fullInput())
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		b, err := canonicalRecord(out)
		if err != nil {
			t.Fatalf("run %d encode: %v", i, err)
		}
		if i == 0 {
			first = b
		} else if string(first) != string(b) {
			t.Fatalf("replay bytes changed:\n%s\n%s", first, b)
		}
	}
	s := string(first)
	if !strings.Contains(s, `"unmapped":{"type":"string","state":"ABSENT"}`) {
		t.Errorf("unmapped not encoded as ABSENT: %s", s)
	}
	if strings.Contains(s, `"state":"NULL"`) {
		t.Errorf("ABSENT drifted to NULL on the wire: %s", s)
	}
}

// GOLDEN: fixed conversion vectors, including a real timestamp-normalization
// vector (a +02:00 offset renders as canonical UTC).
func TestTodo_XFORM_003_Golden(t *testing.T) {
	cases := []struct {
		name     string
		in       any
		from, to transformation.Type
		want     any
		wantErr  bool
	}{
		{"int_ok", "42", transformation.TypeString, transformation.TypeInt, int64(42), false},
		{"int_neg", "-7", transformation.TypeString, transformation.TypeInt, int64(-7), false},
		{"int_bad", "abc", transformation.TypeString, transformation.TypeInt, nil, true},
		{"bool_true", "true", transformation.TypeString, transformation.TypeBool, true, false},
		{"bool_false", "FALSE", transformation.TypeString, transformation.TypeBool, false, false},
		{"date_ok", "1815-12-10", transformation.TypeString, transformation.TypeDate, "1815-12-10", false},
		{"date_bad", "13-45-99", transformation.TypeString, transformation.TypeDate, nil, true},
		{"decimal_ok", "1.25", transformation.TypeString, transformation.TypeDecimal, "1.25", false},
		{"decimal_bad", "1.2.3", transformation.TypeString, transformation.TypeDecimal, nil, true},
		{"string_ok", "hello", transformation.TypeString, transformation.TypeString, "hello", false},
		{"same_type_passthrough", int64(9), transformation.TypeInt, transformation.TypeInt, int64(9), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := convert(c.in, c.from, c.to)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v (%T), want %v (%T)", got, got, c.want, c.want)
			}
		})
	}

	ts, err := convert("2024-05-06T07:08:09+02:00", transformation.TypeString, transformation.TypeTimestamp)
	if err != nil {
		t.Fatalf("timestamp convert: %v", err)
	}
	if ts != "2024-05-06T05:08:09Z" {
		t.Errorf("timestamp=%v, want canonical UTC 2024-05-06T05:08:09Z", ts)
	}
}

// FUZZ: replay stability under arbitrary source values. Success must encode
// identically; failure must report an identical rejection.
func FuzzTodo_XFORM_003(f *testing.F) {
	for _, s := range []string{"42", "-3", "abc", "", "0.5", "true", "1990-01-15"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, amount string) {
		d := convertDef()
		in := Record{"amount": Present(transformation.TypeString, amount)}
		o1, e1 := Execute(d, in)
		o2, e2 := Execute(d, in)
		b1, _ := canonicalRecord(o1)
		b2, _ := canonicalRecord(o2)
		if string(b1) != string(b2) {
			t.Fatalf("replay bytes changed for %q: %s vs %s", amount, b1, b2)
		}
		if fmt.Sprint(e1) != fmt.Sprint(e2) {
			t.Fatalf("replay error changed for %q: %v vs %v", amount, e1, e2)
		}
	})
}

// RECOVERY: the declared failure policy is honored. Reject hard-stops with a
// rejection; skip drops the failing operation and still yields the other
// fields, with the untended one filled as ABSENT.
func TestTodo_XFORM_003_Recovery(t *testing.T) {
	source := []transformation.Field{{Name: "first", Type: transformation.TypeString}, {Name: "amount", Type: transformation.TypeString}}
	dest := []transformation.Field{{Name: "name", Type: transformation.TypeString}, {Name: "quantity", Type: transformation.TypeInt}}
	ops := []transformation.Operation{
		{Kind: transformation.OpConvert, Source: pathP(srcPath("amount", transformation.TypeString)), Destination: dstPath("quantity", transformation.TypeInt), TargetType: transformation.TypeInt},
		{Kind: transformation.OpCopy, Source: pathP(srcPath("first", transformation.TypeString)), Destination: dstPath("name", transformation.TypeString)},
	}
	in := Record{"first": Present(transformation.TypeString, "x"), "amount": Present(transformation.TypeString, "not-an-int")}

	reject := newDef(source, dest, ops, transformation.FailureReject)
	if _, err := Execute(reject, in); err == nil {
		t.Fatal("FailureReject: wanted an error, got nil")
	} else if !errors.Is(err, ErrRejected) {
		t.Fatalf("FailureReject: error %v is not ErrRejected", err)
	}

	skip := newDef(source, dest, ops, transformation.FailureSkip)
	out, err := Execute(skip, in)
	if err != nil {
		t.Fatalf("FailureSkip: %v", err)
	}
	v := out["name"]
	requireValue(t, v, transformation.TypeString)
	if v.Data != "x" {
		t.Errorf("name=%v, want x", v.Data)
	}
	v = out["quantity"]
	if v.State != values.PresenceAbsent || v.Data != nil {
		t.Errorf("quantity state=%v data=%v, want ABSENT (skipped op must not invent a value)", v.State, v.Data)
	}
}

// MUTATION: the rejection carries stable, machine-readable metadata and
// unwraps to the sentinel.
func TestTodo_XFORM_003_Mutation(t *testing.T) {
	source := []transformation.Field{{Name: "first", Type: transformation.TypeString}}
	dest := []transformation.Field{{Name: "name", Type: transformation.TypeString}}
	ops := []transformation.Operation{{Kind: transformation.OpCopy, Source: pathP(srcPath("first", transformation.TypeString)), Destination: dstPath("name", transformation.TypeString)}}
	d := newDef(source, dest, ops, transformation.FailureReject)

	// Input value is wrong-typed for the declared path: a mutation of the
	// source that must surface as a typed rejection, not a silent zero.
	in := Record{"first": Present(transformation.TypeInt, 5)}
	_, err := Execute(d, in)
	if err == nil {
		t.Fatal("wanted a type-mismatch rejection, got nil")
	}
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("error %v is not ErrRejected", err)
	}
	var r Rejection
	if !errors.As(err, &r) {
		t.Fatalf("error %T does not expose Rejection", err)
	}
	if r.Code != "XFORM_003_REJECTED" || r.Field != "first" || r.Version != 1 {
		t.Fatalf("rejection metadata wrong: %+v", r)
	}
	msg := r.Error()
	if !strings.Contains(msg, "field=first") || !strings.Contains(msg, "state=PRESENCE_UNSPECIFIED") || !strings.Contains(msg, "version=1") {
		t.Errorf("rejection message missing offenders: %q", msg)
	}
}
