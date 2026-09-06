package rules

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func rule001Expression(t *testing.T, source string) Expression {
	t.Helper()
	expr, err := ParseTyped(source, map[string]ExpressionType{
		"country":          ExpressionTypeString,
		"increase_percent": ExpressionTypeDecimal,
	})
	if err != nil {
		t.Fatalf("ParseTyped: %v", err)
	}
	expr.UnknownSemantics = UnknownSemanticsPropagate
	expr.Dependencies = []Dependency{{Name: "reference.country_codes", Version: "2026.1", Digest: "sha256:country"}}
	return expr
}

func rule001Inputs(t *testing.T, pct, country string) map[string]Value {
	t.Helper()
	d, err := values.NewDecimal(pct, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	return map[string]Value{"increase_percent": DecimalValue(d), "country": StringValue(country)}
}

// TestTodo_RULE_001 is the primary bounded-expression contract test.
func TestTodo_RULE_001(t *testing.T) {
	compiled, err := CompileExpression(rule001Expression(t, `increase_percent > 10.0000 && country == "US"`))
	if err != nil {
		t.Fatalf("CompileExpression: %v", err)
	}
	if compiled.Digest == "" || !strings.HasPrefix(compiled.Digest, "sha256:") || compiled.DependencyDigest == "" {
		t.Fatalf("missing digests: %+v", compiled)
	}
	if compiled.Cost.Nodes != 7 || compiled.Cost.Depth != 3 {
		t.Fatalf("cost = %+v, want 7 nodes/depth 3", compiled.Cost)
	}
	result, err := compiled.Evaluate(rule001Inputs(t, "12.0000", "US"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.State != EvalStatePresent || result.Value.Kind() != KindBool {
		t.Fatalf("result = %+v, want present bool", result)
	}
	b, _ := result.Value.Bool()
	if !b {
		t.Fatal("expression result = false, want true")
	}
	if !strings.Contains(compiled.Explain(), "backend=owned-evaluator") {
		t.Fatalf("Explain = %q", compiled.Explain())
	}

	t.Run("refusals are typed", func(t *testing.T) {
		cases := []struct {
			name       string
			expression Expr
			want       error
		}{
			{"arbitrary", Call("exec", StringLiteral("x")), ErrExpressionArbitrary},
			{"io", Call("os.ReadFile", StringLiteral("x")), ErrExpressionIO},
			{"time", Call("now"), ErrExpressionTime},
			{"random", Call("random"), ErrExpressionRandom},
			{"type", Binary(BinaryOpAdd, StringLiteral("x"), IntLiteral(1)), ErrExpressionType},
			{"iteration", Iterate(IterationOpAll, Variable("items", ExpressionTypeList), BoolLiteral(true), 0), ErrExpressionIteration},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				expr := Expression{Root: tc.expression, Version: "1", UnknownSemantics: UnknownSemanticsPropagate, Inputs: []Input{{Name: "items", Type: ExpressionTypeList, ElementType: ExpressionTypeString}}}
				_, err := CompileExpression(expr)
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want errors.Is(..., %v)", err, tc.want)
				}
			})
		}
		_, err := CompileExpression(Expression{Root: BoolLiteral(true), Version: "1"})
		if !errors.Is(err, ErrExpressionUnknown) {
			t.Fatalf("absent unknown semantics error = %v", err)
		}
	})

	t.Run("missing input is unknown, not false", func(t *testing.T) {
		result, err := compiled.Evaluate(map[string]Value{"country": StringValue("US")})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.State != EvalStateUnknown {
			t.Fatalf("state = %s, want UNKNOWN", result.State)
		}
	})
}

// TestTodo_RULE_001_Property proves digest identity is independent of map or
// declaration ordering while dependency versions remain part of identity.
func TestTodo_RULE_001_Property(t *testing.T) {
	a := rule001Expression(t, `increase_percent > 10.0000 && country == "US"`)
	b := rule001Expression(t, `increase_percent > 10.0000 && country == "US"`)
	b.Inputs[0], b.Inputs[1] = b.Inputs[1], b.Inputs[0]
	b.Dependencies = []Dependency{{Name: "reference.country_codes", Version: "2026.1", Digest: "sha256:country"}}
	one, err := CompileExpression(a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := CompileExpression(b)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest || one.DependencyDigest != two.DependencyDigest {
		t.Fatalf("equal expressions differ: %s/%s vs %s/%s", one.Digest, one.DependencyDigest, two.Digest, two.DependencyDigest)
	}
	b.Dependencies[0].Version = "2026.2"
	three, err := CompileExpression(b)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest == three.Digest {
		t.Fatal("dependency version change did not change digest")
	}
}

// TestTodo_RULE_001_Golden pins the IR rendering for a fixture set. The
// digest property is tested separately so this fixture remains readable.
func TestTodo_RULE_001_Golden(t *testing.T) {
	raw, err := os.ReadFile("testdata/rule_001_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Source string `json:"source"`
		IR     string `json:"ir"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Source, func(t *testing.T) {
			compiled, err := CompileExpression(rule001Expression(t, fixture.Source))
			if err != nil {
				t.Fatal(err)
			}
			if got := compiled.IR.Canonical(); got != fixture.IR {
				t.Fatalf("IR = %q, want %q", got, fixture.IR)
			}
		})
	}
}

// FuzzTodo_RULE_001 ensures arbitrary parser input returns an error or a
// finite owned AST and never invokes a runtime or panics.
func FuzzTodo_RULE_001(f *testing.F) {
	for _, seed := range []string{`true`, `a > 1`, `!(country == "US")`, `lower(country) == "us"`, ``, `os.ReadFile("x")`, `((a`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		expr, err := Parse(source)
		if err != nil {
			return
		}
		if expr.Root.Kind == ExprKindUnspecified {
			t.Fatal("parser returned an unspecified root")
		}
	})
}

func TestRule001ParserAndTypedDateLiteral(t *testing.T) {
	expr, err := ParseTyped(`start_date >= "2026-01-01"`, map[string]ExpressionType{"start_date": ExpressionTypeDate})
	if err != nil {
		t.Fatal(err)
	}
	expr.UnknownSemantics = UnknownSemanticsPropagate
	// A quoted date is a string in the source grammar; callers use DateLiteral
	// when they want the date-specific type, so this parser expression is
	// correctly refused instead of silently converting a string.
	if _, err := CompileExpression(expr); !errors.Is(err, ErrExpressionType) {
		t.Fatalf("date/string comparison error = %v", err)
	}
	good := Expression{Root: Binary(BinaryOpGreaterOrEqual, Variable("start_date", ExpressionTypeDate), DateLiteral("2026-01-01")), Inputs: []Input{{Name: "start_date", Type: ExpressionTypeDate}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
	if _, err := CompileExpression(good); err != nil {
		t.Fatalf("DateLiteral compile: %v", err)
	}
}

var _ = values.MustDecimal
