package exec

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// -- fixtures ---------------------------------------------------------------

func srcPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "people", Field: field, Type: typ}
}
func dstPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "worker", Field: field, Type: typ}
}
func ptr(p transformation.Path) *transformation.Path { return &p }

// fullDefinition exercises every operation kind Compile knows how to emit
// today (project via copy, coerce via convert, map-default, aggregate via
// concat), so the compiled Program has more than one instruction kind.
func fullDefinition() transformation.TransformationDefinition {
	return transformation.TransformationDefinition{
		Version: transformation.ContractVersion, Name: "people-full", Owner: "shared-engines", Phase: "P1A",
		Source: transformation.Schema{Name: "people", Version: 1, Fields: []transformation.Field{
			{Name: "given", Type: transformation.TypeString, Required: true},
			{Name: "family", Type: transformation.TypeString, Required: true},
			{Name: "amount", Type: transformation.TypeString},
		}},
		Destination: transformation.Schema{Name: "worker", Version: 1, Fields: []transformation.Field{
			{Name: "given", Type: transformation.TypeString, Required: true},
			{Name: "quantity", Type: transformation.TypeInt},
			{Name: "note", Type: transformation.TypeString},
			{Name: "full_name", Type: transformation.TypeString},
			{Name: "unmapped", Type: transformation.TypeString},
		}},
		Operations: []transformation.Operation{
			{Kind: transformation.OpCopy, Source: ptr(srcPath("given", transformation.TypeString)), Destination: dstPath("given", transformation.TypeString)},
			{Kind: transformation.OpConvert, Source: ptr(srcPath("amount", transformation.TypeString)), Destination: dstPath("quantity", transformation.TypeInt), TargetType: transformation.TypeInt},
			{Kind: transformation.OpDefault, Destination: dstPath("note", transformation.TypeString), Literal: "n/a"},
			{Kind: transformation.OpConcat, Destination: dstPath("full_name", transformation.TypeString), Sources: []transformation.Path{
				srcPath("given", transformation.TypeString), srcPath("family", transformation.TypeString),
			}},
		},
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits:        transformation.ResourceLimits{MaxOperations: 8, MaxInputBytes: 4096, MaxOutputBytes: 4096, MaxExpansion: 4},
		Failure:       transformation.FailureReject, SideEffects: transformation.SideEffectsNone,
	}
}

func fullProgram(t *testing.T) ir.Program {
	t.Helper()
	p, err := ir.Compile(fullDefinition())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

func fullRow() Record {
	return Record{
		"people.given":  Present(transformation.TypeString, "Ada"),
		"people.family": Present(transformation.TypeString, "Lovelace"),
		"people.amount": Present(transformation.TypeString, "42"),
	}
}

func requireValue(t *testing.T, r Record, key string, typ transformation.Type, want any) {
	t.Helper()
	v, ok := r[key]
	if !ok {
		t.Fatalf("%s: not present in result", key)
	}
	if v.State != values.PresenceValue {
		t.Fatalf("%s: state=%v, want VALUE", key, v.State)
	}
	if v.Type != typ {
		t.Fatalf("%s: type=%v, want %v", key, v.Type, typ)
	}
	if v.Data != want {
		t.Fatalf("%s: data=%v, want %v", key, v.Data, want)
	}
}

// -- XFORM-003 matrix --------------------------------------------------------

// PRIMARY: a full round trip through every operation kind Compile emits
// today, asserting an untended destination field lands as ABSENT -- never
// NULL and never a silent zero value.
func TestTodo_XFORM_003(t *testing.T) {
	p := fullProgram(t)
	limits := Limits{MaxRows: 10, MaxSteps: 100, MaxOutputBytes: 1 << 20}

	out, err := Execute(p, []Record{fullRow()}, limits)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d, want 1", len(out))
	}
	row := out[0]
	requireValue(t, row, "worker.given", transformation.TypeString, "Ada")
	requireValue(t, row, "worker.quantity", transformation.TypeInt, int64(42))
	requireValue(t, row, "worker.note", transformation.TypeString, "n/a")
	requireValue(t, row, "worker.full_name", transformation.TypeString, "AdaLovelace")

	// A row that supplies none of the source fields must propagate ABSENT
	// through project, coerce and aggregate -- never invent NULL or a zero
	// value. The declared-default field still resolves to its literal: an
	// explicit default is not the defect the RED criterion names.
	empty, err := Execute(p, []Record{{}}, limits)
	if err != nil {
		t.Fatalf("Execute(empty row): %v", err)
	}
	for _, key := range []string{"worker.given", "worker.quantity", "worker.full_name"} {
		v := empty[0][key]
		if v.State != values.PresenceAbsent || v.Data != nil {
			t.Fatalf("%s: state=%v data=%v, want ABSENT with no data", key, v.State, v.Data)
		}
	}
	requireValue(t, empty[0], "worker.note", transformation.TypeString, "n/a")
}

// PROPERTY: replay determinism, including across concurrent goroutines. The
// same program and input must encode to the identical canonical digest on
// every run, whether sequential or run from many goroutines at once.
func TestTodo_XFORM_003_Property(t *testing.T) {
	p := fullProgram(t)
	limits := Limits{MaxRows: 10, MaxSteps: 100, MaxOutputBytes: 1 << 20}

	var first string
	for i := 0; i < 5; i++ {
		out, err := Execute(p, []Record{fullRow()}, limits)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		d, err := Digest(out)
		if err != nil {
			t.Fatalf("run %d digest: %v", i, err)
		}
		if i == 0 {
			first = d
		} else if d != first {
			t.Fatalf("replay digest changed: %s vs %s", first, d)
		}
	}

	const goroutines = 32
	digests := make([]string, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			out, err := Execute(p, []Record{fullRow()}, limits)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i], errs[i] = Digest(out)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != first {
			t.Fatalf("goroutine %d digest=%s, want %s", i, digests[i], first)
		}
	}
}

// GOLDEN: fixed coercion vectors, including canonical UTC timestamp
// normalization of an offset input.
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
		{"bool_bad", "maybe", transformation.TypeString, transformation.TypeBool, nil, true},
		{"date_ok", "1815-12-10", transformation.TypeString, transformation.TypeDate, "1815-12-10", false},
		{"date_bad", "13-45-99", transformation.TypeString, transformation.TypeDate, nil, true},
		{"decimal_ok", "1.25", transformation.TypeString, transformation.TypeDecimal, "1.25", false},
		{"decimal_bad", "1.2.3", transformation.TypeString, transformation.TypeDecimal, nil, true},
		{"same_type_passthrough", int64(9), transformation.TypeInt, transformation.TypeInt, int64(9), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := coerce(c.in, c.from, c.to)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %v (%T), want %v (%T)", got, got, c.want, c.want)
			}
		})
	}

	ts, err := coerce("2024-05-06T07:08:09+02:00", transformation.TypeString, transformation.TypeTimestamp)
	if err != nil {
		t.Fatalf("timestamp coerce: %v", err)
	}
	if ts != "2024-05-06T05:08:09Z" {
		t.Errorf("timestamp=%v, want canonical UTC 2024-05-06T05:08:09Z", ts)
	}
}

// FUZZ: replay stability under arbitrary source text. Two independent
// executions of the same input must produce identical digests and
// identical error text -- never a panic.
func FuzzTodo_XFORM_003(f *testing.F) {
	for _, s := range []string{"42", "-3", "abc", "", "0.5", "true", "1990-01-15"} {
		f.Add(s)
	}
	def := transformation.TransformationDefinition{
		Version: transformation.ContractVersion, Name: "fuzz-convert", Owner: "shared-engines", Phase: "P1A",
		Source:      transformation.Schema{Name: "src", Version: 1, Fields: []transformation.Field{{Name: "amount", Type: transformation.TypeString}}},
		Destination: transformation.Schema{Name: "dst", Version: 1, Fields: []transformation.Field{{Name: "quantity", Type: transformation.TypeInt}}},
		Operations: []transformation.Operation{
			{Kind: transformation.OpConvert, Source: &transformation.Path{Schema: "src", Field: "amount", Type: transformation.TypeString}, Destination: transformation.Path{Schema: "dst", Field: "quantity", Type: transformation.TypeInt}, TargetType: transformation.TypeInt},
		},
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits:        transformation.ResourceLimits{MaxOperations: 4, MaxInputBytes: 4096, MaxOutputBytes: 4096, MaxExpansion: 1},
		Failure:       transformation.FailureReject, SideEffects: transformation.SideEffectsNone,
	}
	p, err := ir.Compile(def)
	if err != nil {
		f.Fatalf("compile: %v", err)
	}
	f.Fuzz(func(t *testing.T, amount string) {
		in := []Record{{"src.amount": Present(transformation.TypeString, amount)}}
		limits := Limits{MaxRows: 10, MaxSteps: 10, MaxOutputBytes: 1 << 20}
		o1, e1 := Execute(p, in, limits)
		o2, e2 := Execute(p, in, limits)
		d1, _ := Digest(o1)
		d2, _ := Digest(o2)
		if d1 != d2 {
			t.Fatalf("replay digest changed for %q: %s vs %s", amount, d1, d2)
		}
		if fmt.Sprint(e1) != fmt.Sprint(e2) {
			t.Fatalf("replay error changed for %q: %v vs %v", amount, e1, e2)
		}
	})
}

// RECOVERY: a refused execution returns zero output, and the interpreter is
// otherwise stateless -- a later, valid execution on the same Interpreter
// succeeds cleanly with no residue from the refusal.
func TestTodo_XFORM_003_Recovery(t *testing.T) {
	p := fullProgram(t)
	in, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	limits := Limits{MaxRows: 1, MaxSteps: 100, MaxOutputBytes: 1 << 20}

	// Two rows breach the declared row limit of 1.
	out, err := in.Execute([]Record{fullRow(), fullRow()}, limits)
	if err == nil {
		t.Fatal("wanted a row-limit refusal, got nil")
	}
	if out != nil {
		t.Fatalf("refused execution returned %d rows, want nil", len(out))
	}
	var ref Refusal
	if !errors.As(err, &ref) || ref.Step != "dataset" {
		t.Fatalf("error %v is not a dataset Refusal", err)
	}

	// The same Interpreter, given valid input, is unaffected by the prior
	// refusal.
	out, err = in.Execute([]Record{fullRow()}, limits)
	if err != nil {
		t.Fatalf("Execute after refusal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d, want 1", len(out))
	}
	requireValue(t, out[0], "worker.given", transformation.TypeString, "Ada")
}

// MUTATION: every declared limit dimension (row, fan-out, step budget,
// output size) and a type mismatch each independently produce a typed
// Refusal naming the offending step -- flipping any one guard away would
// let its case silently pass here.
func TestTodo_XFORM_003_Mutation(t *testing.T) {
	p := fullProgram(t)

	t.Run("row_limit", func(t *testing.T) {
		_, err := Execute(p, []Record{fullRow(), fullRow()}, Limits{MaxRows: 1, MaxSteps: 100, MaxOutputBytes: 1 << 20})
		assertRefusal(t, err, "dataset", "max_rows")
	})

	t.Run("step_budget", func(t *testing.T) {
		_, err := Execute(p, []Record{fullRow()}, Limits{MaxRows: 10, MaxSteps: 1, MaxOutputBytes: 1 << 20})
		assertRefusal(t, err, "row 0", "max_steps")
	})

	t.Run("output_size", func(t *testing.T) {
		_, err := Execute(p, []Record{fullRow()}, Limits{MaxRows: 10, MaxSteps: 100, MaxOutputBytes: 1})
		assertRefusal(t, err, "output", "max_output_bytes")
	})

	t.Run("fan_out", func(t *testing.T) {
		// Fan-out is a Program-shape property (the closed IR's own declared
		// bound), so a Program whose Limits no longer cover its own
		// instructions is refused at Interpreter construction -- before any
		// row runs -- rather than mid-execution. It is still a typed,
		// step-naming refusal; just from the IR layer, not this package's
		// own Refusal.
		tight := p
		tight.Limits.MaxFanOut = 1 // the concat instruction has 2 sources
		_, err := Execute(tight, []Record{fullRow()}, Limits{MaxRows: 10, MaxSteps: 100, MaxOutputBytes: 1 << 20})
		if !errors.Is(err, ir.ErrUnboundedProgram) {
			t.Fatalf("error %v does not unwrap to ir.ErrUnboundedProgram", err)
		}
		if !strings.Contains(err.Error(), "max_fan_out") {
			t.Fatalf("error %v does not name max_fan_out", err)
		}
	})

	t.Run("type_mismatch", func(t *testing.T) {
		bad := fullRow()
		bad["people.given"] = Present(transformation.TypeInt, int64(5))
		_, err := Execute(p, []Record{bad}, Limits{MaxRows: 10, MaxSteps: 100, MaxOutputBytes: 1 << 20})
		var ref Refusal
		if !errors.As(err, &ref) {
			t.Fatalf("error %v is not a Refusal", err)
		}
		if !strings.Contains(ref.Reason, "type mismatch") {
			t.Fatalf("reason %q does not name a type mismatch", ref.Reason)
		}
	})
}

func assertRefusal(t *testing.T, err error, stepPrefix, reasonSubstr string) {
	t.Helper()
	var ref Refusal
	if !errors.As(err, &ref) {
		t.Fatalf("error %v is not a Refusal", err)
	}
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("error %v does not unwrap to ErrRefused", err)
	}
	if !strings.HasPrefix(ref.Step, stepPrefix) {
		t.Fatalf("step=%q, want prefix %q", ref.Step, stepPrefix)
	}
	if !strings.Contains(ref.Reason, reasonSubstr) {
		t.Fatalf("reason=%q, want it to name %q", ref.Reason, reasonSubstr)
	}
}
