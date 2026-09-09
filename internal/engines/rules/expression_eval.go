package rules

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EvalState is the result-presence state of an expression evaluation.
type EvalState uint8

const (
	EvalStateUnspecified EvalState = iota
	EvalStatePresent
	EvalStateUnknown
)

func (s EvalState) String() string {
	switch s {
	case EvalStatePresent:
		return "PRESENT"
	case EvalStateUnknown:
		return "UNKNOWN"
	default:
		return "EVAL_STATE_UNSPECIFIED"
	}
}

// EvaluationStep is a bounded trace entry. It carries operation shape and
// status without exposing protected input values.
type EvaluationStep struct {
	Index  int
	Opcode Opcode
	Type   ExpressionType
	State  EvalState
}

// ExpressionResult is the pure evaluator's typed result and trace.
type ExpressionResult struct {
	State  EvalState
	Type   ExpressionType
	Value  Value
	Digest string
	Steps  []EvaluationStep
}

// Evaluate is the method form for a compiled expression. Missing map entries
// are UNKNOWN, never a zero value.
func (c CompiledExpression) Evaluate(inputs map[string]Value) (ExpressionResult, error) {
	if c.Digest == "" || c.IR.Root < 0 || c.IR.Root >= len(c.IR.Nodes) {
		return ExpressionResult{}, fmt.Errorf("%w: compiled expression is invalid", ErrExpressionEvaluation)
	}
	inputTypes := make(map[string]Input, len(c.Inputs))
	for _, input := range c.Inputs {
		inputTypes[input.Name] = input
	}
	steps := make([]EvaluationStep, 0, len(c.IR.Nodes))
	cache := make(map[int]runtimeValue, len(c.IR.Nodes))
	value, err := evaluateNode(c.IR, c.IR.Root, inputs, inputTypes, cache, &steps)
	if err != nil {
		return ExpressionResult{}, err
	}
	if value.state == EvalStateUnknown && c.UnknownSemantics == UnknownSemanticsReject {
		return ExpressionResult{}, ErrExpressionUnknown
	}
	result := ExpressionResult{State: value.state, Type: c.IR.Nodes[c.IR.Root].Type, Digest: c.Digest, Steps: steps}
	if value.state == EvalStatePresent {
		result.Value = value.value
	}
	return result, nil
}

// EvaluateExpression compiles and evaluates an owned expression in one pure
// operation. Call CompileExpression directly when the compiled artifact must
// be persisted or reused.
func EvaluateExpression(expr Expression, inputs map[string]Value, limits ...CostLimit) (ExpressionResult, error) {
	compiled, err := CompileExpression(expr, limits...)
	if err != nil {
		return ExpressionResult{}, err
	}
	return compiled.Evaluate(inputs)
}

// Explain reports the result without printing input values.
func (r ExpressionResult) Explain() string {
	return fmt.Sprintf("expression digest=%s -> %s type=%s steps=%d", r.Digest, r.State, r.Type, len(r.Steps))
}

type runtimeValue struct {
	state EvalState
	value Value
}

func evaluateNode(ir IR, index int, inputs map[string]Value, inputTypes map[string]Input, cache map[int]runtimeValue, steps *[]EvaluationStep) (runtimeValue, error) {
	if value, ok := cache[index]; ok {
		return value, nil
	}
	n := ir.Nodes[index]
	unknown := func() runtimeValue { return runtimeValue{state: EvalStateUnknown} }
	var result runtimeValue
	var err error
	switch n.Opcode {
	case OpcodeLiteral:
		result = runtimeValue{state: EvalStatePresent, value: n.Value}
	case OpcodeUnknown:
		result = unknown()
	case OpcodeVariable:
		input, ok := inputTypes[n.Name]
		if !ok {
			return runtimeValue{}, fmt.Errorf("%w: %q", ErrExpressionVariable, n.Name)
		}
		value, present := inputs[n.Name]
		if !present {
			result = unknown()
			break
		}
		if err := value.Validate(); err != nil {
			return runtimeValue{}, fmt.Errorf("%w: input %q: %v", ErrExpressionEvaluation, n.Name, err)
		}
		if !runtimeValueMatches(input.Type, value) {
			return runtimeValue{}, fmt.Errorf("%w: input %q declares %s, carries %s", ErrExpressionType, n.Name, input.Type, value.Kind())
		}
		result = runtimeValue{state: EvalStatePresent, value: value}
	case OpcodeUnary:
		child, err := evaluateNode(ir, n.Children[0], inputs, inputTypes, cache, steps)
		if err != nil {
			return runtimeValue{}, err
		}
		if child.state == EvalStateUnknown {
			result = unknown()
			break
		}
		switch n.Unary {
		case UnaryOpNot:
			b, ok := child.value.Bool()
			if !ok {
				return runtimeValue{}, fmt.Errorf("%w: NOT operand", ErrExpressionType)
			}
			result = runtimeValue{state: EvalStatePresent, value: BoolValue(!b)}
		case UnaryOpNegate:
			result, err = negateRuntime(child)
		default:
			return runtimeValue{}, fmt.Errorf("%w: unary operator", ErrExpressionEvaluation)
		}
	case OpcodeBinary:
		left, err := evaluateNode(ir, n.Children[0], inputs, inputTypes, cache, steps)
		if err != nil {
			return runtimeValue{}, err
		}
		// Short-circuiting also preserves Kleene logic and avoids evaluating an
		// irrelevant right branch with a missing input.
		if n.Binary == BinaryOpAnd && left.state == EvalStatePresent {
			b, _ := left.value.Bool()
			if !b {
				result = runtimeValue{state: EvalStatePresent, value: BoolValue(false)}
				break
			}
		}
		if n.Binary == BinaryOpOr && left.state == EvalStatePresent {
			b, _ := left.value.Bool()
			if b {
				result = runtimeValue{state: EvalStatePresent, value: BoolValue(true)}
				break
			}
		}
		right, err := evaluateNode(ir, n.Children[1], inputs, inputTypes, cache, steps)
		if err != nil {
			return runtimeValue{}, err
		}
		result, err = evaluateBinary(n.Binary, left, right)
		if err != nil {
			return runtimeValue{}, err
		}
	case OpcodeCall:
		args := make([]runtimeValue, len(n.Children))
		for i, childIndex := range n.Children {
			args[i], err = evaluateNode(ir, childIndex, inputs, inputTypes, cache, steps)
			if err != nil {
				return runtimeValue{}, err
			}
		}
		result, err = evaluateCall(n.Function, args)
		if err != nil {
			return runtimeValue{}, err
		}
	case OpcodeIterate:
		// LIST is deliberately not represented by rules.Value yet. The compiler
		// still owns and bounds the shape; evaluation remains honest until a
		// typed list snapshot is supplied by the model layer.
		result = unknown()
	default:
		return runtimeValue{}, fmt.Errorf("%w: opcode %d is not evaluatable", ErrExpressionEvaluation, n.Opcode)
	}
	cache[index] = result
	*steps = append(*steps, EvaluationStep{Index: index, Opcode: n.Opcode, Type: n.Type, State: result.state})
	return result, nil
}

func runtimeValueMatches(typ ExpressionType, value Value) bool {
	switch typ {
	case ExpressionTypeBool:
		return value.Kind() == KindBool
	case ExpressionTypeInt:
		return value.Kind() == KindInt
	case ExpressionTypeDecimal:
		return value.Kind() == KindDecimal
	case ExpressionTypeString:
		return value.Kind() == KindString
	case ExpressionTypeDate:
		if value.Kind() != KindString {
			return false
		}
		_, err := values.ParseLocalDate(value.String())
		return err == nil
	default:
		return false
	}
}

func negateRuntime(v runtimeValue) (runtimeValue, error) {
	switch v.value.Kind() {
	case KindInt:
		i, _ := v.value.Int()
		return runtimeValue{state: EvalStatePresent, value: IntValue(-i)}, nil
	case KindDecimal:
		d, err := v.value.Decimal()
		if err != nil {
			return runtimeValue{}, err
		}
		nd, err := d.Neg()
		if err != nil {
			return runtimeValue{}, err
		}
		return runtimeValue{state: EvalStatePresent, value: DecimalValue(nd)}, nil
	default:
		return runtimeValue{}, fmt.Errorf("%w: NEGATE operand", ErrExpressionType)
	}
}

func evaluateBinary(op BinaryOp, left, right runtimeValue) (runtimeValue, error) {
	if op == BinaryOpAnd || op == BinaryOpOr {
		return evaluateLogic(op, left, right)
	}
	if left.state == EvalStateUnknown || right.state == EvalStateUnknown {
		return runtimeValue{state: EvalStateUnknown}, nil
	}
	if op == BinaryOpEqual || op == BinaryOpNotEqual || op == BinaryOpLess || op == BinaryOpLessOrEqual || op == BinaryOpGreater || op == BinaryOpGreaterOrEqual {
		cmp, err := compareRuntime(left.value, right.value)
		if err != nil {
			return runtimeValue{}, err
		}
		match := false
		switch op {
		case BinaryOpEqual:
			match = cmp == 0
		case BinaryOpNotEqual:
			match = cmp != 0
		case BinaryOpLess:
			match = cmp < 0
		case BinaryOpLessOrEqual:
			match = cmp <= 0
		case BinaryOpGreater:
			match = cmp > 0
		case BinaryOpGreaterOrEqual:
			match = cmp >= 0
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(match)}, nil
	}
	switch op {
	case BinaryOpAdd, BinaryOpSubtract, BinaryOpMultiply, BinaryOpDivide:
		return arithmeticRuntime(op, left.value, right.value)
	default:
		return runtimeValue{}, fmt.Errorf("%w: binary operator %s", ErrExpressionEvaluation, op)
	}
}

func evaluateLogic(op BinaryOp, left, right runtimeValue) (runtimeValue, error) {
	if left.state == EvalStatePresent && right.state == EvalStatePresent {
		lb, lok := left.value.Bool()
		rb, rok := right.value.Bool()
		if !lok || !rok {
			return runtimeValue{}, ErrExpressionType
		}
		if op == BinaryOpAnd {
			return runtimeValue{state: EvalStatePresent, value: BoolValue(lb && rb)}, nil
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(lb || rb)}, nil
	}
	if op == BinaryOpAnd && right.state == EvalStatePresent {
		rb, ok := right.value.Bool()
		if ok && !rb {
			return runtimeValue{state: EvalStatePresent, value: BoolValue(false)}, nil
		}
	}
	if op == BinaryOpOr && right.state == EvalStatePresent {
		rb, ok := right.value.Bool()
		if ok && rb {
			return runtimeValue{state: EvalStatePresent, value: BoolValue(true)}, nil
		}
	}
	return runtimeValue{state: EvalStateUnknown}, nil
}

func compareRuntime(left, right Value) (int, error) {
	if left.Kind() != right.Kind() {
		return 0, fmt.Errorf("%w: %s and %s", ErrExpressionType, left.Kind(), right.Kind())
	}
	if left.Kind() == KindString {
		if left.String() < right.String() {
			return -1, nil
		}
		if left.String() > right.String() {
			return 1, nil
		}
		return 0, nil
	}
	return left.Cmp(right)
}

func arithmeticRuntime(op BinaryOp, left, right Value) (runtimeValue, error) {
	if left.Kind() == KindString && right.Kind() == KindString && op == BinaryOpAdd {
		return runtimeValue{state: EvalStatePresent, value: StringValue(left.String() + right.String())}, nil
	}
	if left.Kind() == KindInt && right.Kind() == KindInt {
		li, _ := left.Int()
		ri, _ := right.Int()
		switch op {
		case BinaryOpAdd:
			return runtimeValue{state: EvalStatePresent, value: IntValue(li + ri)}, nil
		case BinaryOpSubtract:
			return runtimeValue{state: EvalStatePresent, value: IntValue(li - ri)}, nil
		case BinaryOpMultiply:
			return runtimeValue{state: EvalStatePresent, value: IntValue(li * ri)}, nil
		case BinaryOpDivide:
			if ri == 0 {
				return runtimeValue{}, fmt.Errorf("%w: integer division by zero", ErrExpressionEvaluation)
			}
			return runtimeValue{state: EvalStatePresent, value: IntValue(li / ri)}, nil
		}
	}
	if left.Kind() != KindDecimal || right.Kind() != KindDecimal {
		return runtimeValue{}, fmt.Errorf("%w: numeric operands must both be decimal or int", ErrExpressionType)
	}
	ld, _ := left.Decimal()
	rd, _ := right.Decimal()
	scale := ld.Scale()
	if rd.Scale() > scale {
		scale = rd.Scale()
	}
	var result values.Decimal
	var err error
	switch op {
	case BinaryOpAdd:
		if ld.Scale() != rd.Scale() {
			return runtimeValue{}, fmt.Errorf("%w: decimal addition requires equal scale", ErrExpressionType)
		}
		result, err = ld.Add(rd)
	case BinaryOpSubtract:
		if ld.Scale() != rd.Scale() {
			return runtimeValue{}, fmt.Errorf("%w: decimal subtraction requires equal scale", ErrExpressionType)
		}
		result, err = ld.Sub(rd)
	case BinaryOpMultiply:
		result, err = ld.Mul(rd, scale, values.RoundingHalfEven)
	case BinaryOpDivide:
		result, err = ld.Div(rd, scale, values.RoundingHalfEven)
	}
	if err != nil {
		return runtimeValue{}, fmt.Errorf("%w: %v", ErrExpressionEvaluation, err)
	}
	return runtimeValue{state: EvalStatePresent, value: DecimalValue(result)}, nil
}

// Accessors keep Value's representation private while allowing the owned
// evaluator to inspect it.
func (v Value) Bool() (bool, bool) {
	if v.Kind() != KindBool {
		return false, false
	}
	return v.b, true
}
func (v Value) Int() (int64, bool) {
	if v.Kind() != KindInt {
		return 0, false
	}
	return v.i, true
}
func (v Value) Decimal() (values.Decimal, error) {
	if v.Kind() != KindDecimal {
		return values.Decimal{}, fmt.Errorf("%w: expected decimal", ErrExpressionType)
	}
	return v.dec, nil
}

func evaluateCall(name string, args []runtimeValue) (runtimeValue, error) {
	for _, arg := range args {
		if arg.state == EvalStateUnknown {
			return runtimeValue{state: EvalStateUnknown}, nil
		}
	}
	lower := strings.ToLower(name)
	if lower == "lower" || lower == "upper" {
		if len(args) != 1 || args[0].value.Kind() != KindString {
			return runtimeValue{}, ErrExpressionType
		}
		text := args[0].value.String()
		if lower == "lower" {
			text = strings.ToLower(text)
		} else {
			text = strings.ToUpper(text)
		}
		return runtimeValue{state: EvalStatePresent, value: StringValue(text)}, nil
	}
	if lower == "contains" || lower == "starts_with" || lower == "ends_with" {
		if len(args) != 2 || args[0].value.Kind() != KindString || args[1].value.Kind() != KindString {
			return runtimeValue{}, ErrExpressionType
		}
		a, b := args[0].value.String(), args[1].value.String()
		match := false
		if lower == "contains" {
			match = strings.Contains(a, b)
		}
		if lower == "starts_with" {
			match = strings.HasPrefix(a, b)
		}
		if lower == "ends_with" {
			match = strings.HasSuffix(a, b)
		}
		return runtimeValue{state: EvalStatePresent, value: BoolValue(match)}, nil
	}
	if lower == "len" {
		if len(args) != 1 || args[0].value.Kind() != KindString {
			return runtimeValue{}, ErrExpressionType
		}
		return runtimeValue{state: EvalStatePresent, value: IntValue(int64(len(args[0].value.String())))}, nil
	}
	if lower == "abs" {
		if len(args) != 1 {
			return runtimeValue{}, ErrExpressionType
		}
		if args[0].value.Kind() == KindInt {
			i, _ := args[0].value.Int()
			if i < 0 {
				i = -i
			}
			return runtimeValue{state: EvalStatePresent, value: IntValue(i)}, nil
		}
		d, err := args[0].value.Decimal()
		if err != nil {
			return runtimeValue{}, err
		}
		out, err := d.Abs()
		if err != nil {
			return runtimeValue{}, err
		}
		return runtimeValue{state: EvalStatePresent, value: DecimalValue(out)}, nil
	}
	return runtimeValue{}, fmt.Errorf("%w: %s", ErrExpressionFunction, name)
}
