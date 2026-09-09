package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CompileExpression lowers an owned Expression into bounded backend-neutral
// IR. A variadic limit keeps the safe default convenient while allowing a
// caller to provide a smaller tenant or hot-path budget.
func CompileExpression(expr Expression, limits ...CostLimit) (CompiledExpression, error) {
	limit := DefaultCostLimit
	if len(limits) > 1 {
		return CompiledExpression{}, fmt.Errorf("%w: at most one cost limit", ErrExpressionInvalid)
	}
	if len(limits) == 1 {
		limit = limits[0]
	}
	if limit.MaxNodes <= 0 || limit.MaxDepth <= 0 || limit.MaxIterations <= 0 || limit.MaxCost <= 0 {
		return CompiledExpression{}, fmt.Errorf("%w: all cost limits must be positive", ErrExpressionUnbounded)
	}
	if expr.Version == "" {
		return CompiledExpression{}, fmt.Errorf("%w: expression version is required", ErrExpressionInvalid)
	}
	if !expr.UnknownSemantics.Valid() {
		return CompiledExpression{}, ErrExpressionUnknown
	}

	env := make(map[string]Input, len(expr.Inputs))
	for _, input := range expr.Inputs {
		if input.Name == "" || !input.Type.Valid() || (input.Type == ExpressionTypeList && !input.ElementType.Valid()) {
			return CompiledExpression{}, fmt.Errorf("%w: invalid input %q", ErrExpressionInvalid, input.Name)
		}
		if _, exists := env[input.Name]; exists {
			return CompiledExpression{}, fmt.Errorf("%w: duplicate input %q", ErrExpressionInvalid, input.Name)
		}
		env[input.Name] = input
	}
	deps := append([]Dependency(nil), expr.Dependencies...)
	sort.SliceStable(deps, func(i, j int) bool { return dependencyLess(deps[i], deps[j]) })
	for _, dep := range deps {
		if dep.Name == "" || dep.Version == "" || dep.Digest == "" {
			return CompiledExpression{}, fmt.Errorf("%w: %q", ErrExpressionDependency, dep.Name)
		}
	}
	depDigest, err := dependenciesDigest(deps)
	if err != nil {
		return CompiledExpression{}, err
	}

	state := compilerState{env: env, limit: limit, ir: IR{Nodes: make([]IRNode, 0, 16)}}
	root, typ, err := state.lower(expr.Root, 1)
	if err != nil {
		return CompiledExpression{}, err
	}
	if root < 0 || !typ.Valid() {
		return CompiledExpression{}, fmt.Errorf("%w: root has no type", ErrExpressionType)
	}
	state.ir.Root = root
	state.cost.Total = state.cost.Nodes + state.cost.Iterations
	if state.cost.Total > limit.MaxCost {
		return CompiledExpression{}, fmt.Errorf("%w: cost %d exceeds %d", ErrExpressionUnbounded, state.cost.Total, limit.MaxCost)
	}

	inputs := append([]Input(nil), expr.Inputs...)
	sort.SliceStable(inputs, func(i, j int) bool { return inputs[i].Name < inputs[j].Name })
	digest, err := compiledDigest(expr.Version, expr.UnknownSemantics, inputs, deps, depDigest, state.ir, state.cost)
	if err != nil {
		return CompiledExpression{}, err
	}
	return CompiledExpression{
		IR: state.ir, Inputs: inputs, Dependencies: deps, Version: expr.Version,
		UnknownSemantics: expr.UnknownSemantics, DependencyDigest: depDigest,
		Digest: digest, Cost: state.cost, Limits: limit,
	}, nil
}

// CompileOwnedExpression is a descriptive alias for callers at a boundary
// where the distinction from the existing decision-table Compile matters.
func CompileOwnedExpression(expr Expression, limits ...CostLimit) (CompiledExpression, error) {
	return CompileExpression(expr, limits...)
}

// CompileExpr is a short alias for callers that already use Compile for
// decision tables.
func CompileExpr(expr Expression, limits ...CostLimit) (CompiledExpression, error) {
	return CompileExpression(expr, limits...)
}

// Compile is also available as a method on the owned definition; the package
// function named Compile remains the established decision-table entry point.
func (e Expression) Compile(limits ...CostLimit) (CompiledExpression, error) {
	return CompileExpression(e, limits...)
}

type compilerState struct {
	env   map[string]Input
	limit CostLimit
	ir    IR
	cost  Cost
}

func (s *compilerState) lower(e Expr, depth int) (int, ExpressionType, error) {
	if depth > s.limit.MaxDepth {
		return -1, ExpressionTypeUnspecified, fmt.Errorf("%w: depth %d exceeds %d", ErrExpressionUnbounded, depth, s.limit.MaxDepth)
	}
	s.cost.Depth = maxInt(s.cost.Depth, depth)
	if s.cost.Nodes >= s.limit.MaxNodes {
		return -1, ExpressionTypeUnspecified, fmt.Errorf("%w: nodes exceed %d", ErrExpressionUnbounded, s.limit.MaxNodes)
	}
	s.cost.Nodes++
	if e.Kind == ExprKindUnspecified {
		return -1, ExpressionTypeUnspecified, fmt.Errorf("%w: node kind is unspecified", ErrExpressionInvalid)
	}

	n := IRNode{Type: e.Type, Name: e.Name, Function: e.Function, Value: e.Value, Unary: e.Unary, Binary: e.Binary, Iteration: e.Iteration, IterationBound: e.IterationBound}
	var typ ExpressionType
	var err error
	switch e.Kind {
	case ExprKindLiteral:
		n.Opcode = OpcodeLiteral
		typ = valueExpressionType(e.Value)
		if e.Type == ExpressionTypeDate {
			typ = ExpressionTypeDate
			if e.Value.Kind() != KindString {
				err = fmt.Errorf("%w: date literal must be a string", ErrExpressionType)
			}
			if err == nil {
				if _, parseErr := values.ParseLocalDate(e.Value.String()); parseErr != nil {
					err = fmt.Errorf("%w: invalid date literal: %v", ErrExpressionValue, parseErr)
				}
			}
		}
		if err == nil {
			err = e.Value.Validate()
		}
	case ExprKindVariable:
		n.Opcode = OpcodeVariable
		if strings.Contains(e.Name, ".") {
			err = forbiddenCallError(e.Name)
			break
		}
		input, ok := s.env[e.Name]
		if !ok {
			err = fmt.Errorf("%w: %q", ErrExpressionVariable, e.Name)
		} else {
			typ = input.Type
			if e.Type != ExpressionTypeUnspecified && e.Type != typ {
				err = fmt.Errorf("%w: variable %q is %s, node declares %s", ErrExpressionType, e.Name, typ, e.Type)
			}
		}
	case ExprKindUnknown:
		n.Opcode = OpcodeUnknown
		typ = ExpressionTypeUnknown
	case ExprKindUnary:
		n.Opcode = OpcodeUnary
		if len(e.Children) != 1 {
			err = fmt.Errorf("%w: unary needs one child", ErrExpressionInvalid)
		} else {
			var childType ExpressionType
			var child int
			child, childType, err = s.lower(e.Children[0], depth+1)
			n.Children = []int{child}
			if err == nil {
				switch e.Unary {
				case UnaryOpNot:
					if childType != ExpressionTypeBool && childType != ExpressionTypeUnknown {
						err = typeError("NOT", ExpressionTypeBool, childType)
					}
					typ = ExpressionTypeBool
				case UnaryOpNegate:
					if !isNumber(childType) && childType != ExpressionTypeUnknown {
						err = typeError("NEGATE", ExpressionTypeDecimal, childType)
					}
					typ = childType
				default:
					err = fmt.Errorf("%w: unary operator is unspecified", ErrExpressionInvalid)
				}
			}
		}
	case ExprKindBinary:
		n.Opcode = OpcodeBinary
		if len(e.Children) != 2 {
			err = fmt.Errorf("%w: binary needs two children", ErrExpressionInvalid)
		} else {
			left, lt, leftErr := s.lower(e.Children[0], depth+1)
			right, rt, rightErr := s.lower(e.Children[1], depth+1)
			n.Children = []int{left, right}
			if leftErr != nil {
				err = leftErr
			} else if rightErr != nil {
				err = rightErr
			} else {
				typ, err = binaryType(e.Binary, lt, rt)
			}
		}
	case ExprKindCall:
		n.Opcode = OpcodeCall
		for _, child := range e.Children {
			childIndex, _, childErr := s.lower(child, depth+1)
			n.Children = append(n.Children, childIndex)
			if childErr != nil && err == nil {
				err = childErr
			}
		}
		if err == nil {
			typ, err = callType(e.Function, n.Children, s.ir.Nodes, e.Children)
		}
	case ExprKindIterate:
		n.Opcode = OpcodeIterate
		if e.Iteration != IterationOpAll && e.Iteration != IterationOpAny {
			err = fmt.Errorf("%w: iteration operator is unspecified", ErrExpressionInvalid)
		}
		if e.IterationBound <= 0 || e.IterationBound > s.limit.MaxIterations {
			err = fmt.Errorf("%w: %d must be in [1,%d]", ErrExpressionIteration, e.IterationBound, s.limit.MaxIterations)
		}
		if len(e.Children) != 2 {
			err = fmt.Errorf("%w: iteration needs collection and predicate", ErrExpressionInvalid)
		} else {
			collection, ct, collectionErr := s.lower(e.Children[0], depth+1)
			predicate, pt, predicateErr := s.lower(e.Children[1], depth+1)
			n.Children = []int{collection, predicate}
			if collectionErr != nil {
				err = collectionErr
			} else if predicateErr != nil {
				err = predicateErr
			} else if ct != ExpressionTypeList {
				err = typeError("iteration", ExpressionTypeList, ct)
			} else if pt != ExpressionTypeBool && pt != ExpressionTypeUnknown {
				err = typeError("iteration predicate", ExpressionTypeBool, pt)
			}
			typ = ExpressionTypeBool
			s.cost.Iterations += e.IterationBound
		}
	default:
		err = fmt.Errorf("%w: unsupported node kind %d", ErrExpressionInvalid, e.Kind)
	}
	if err != nil {
		return -1, ExpressionTypeUnspecified, err
	}
	if e.Type != ExpressionTypeUnspecified && e.Type != typ && typ != ExpressionTypeUnknown {
		return -1, ExpressionTypeUnspecified, fmt.Errorf("%w: node declares %s, inferred %s", ErrExpressionType, e.Type, typ)
	}
	n.Type = typ
	index := len(s.ir.Nodes)
	s.ir.Nodes = append(s.ir.Nodes, n)
	return index, typ, nil
}

func typeError(operation string, want, got ExpressionType) error {
	return fmt.Errorf("%w: %s needs %s, got %s", ErrExpressionType, operation, want, got)
}
func isNumber(t ExpressionType) bool { return t == ExpressionTypeInt || t == ExpressionTypeDecimal }

func binaryType(op BinaryOp, left, right ExpressionType) (ExpressionType, error) {
	if left == ExpressionTypeUnknown || right == ExpressionTypeUnknown {
		switch op {
		case BinaryOpAnd, BinaryOpOr, BinaryOpEqual, BinaryOpNotEqual, BinaryOpLess, BinaryOpLessOrEqual, BinaryOpGreater, BinaryOpGreaterOrEqual:
			return ExpressionTypeBool, nil
		default:
			if left == ExpressionTypeUnknown {
				return right, nil
			}
			return left, nil
		}
	}
	switch op {
	case BinaryOpAnd, BinaryOpOr:
		if left != ExpressionTypeBool || right != ExpressionTypeBool {
			return 0, typeError(op.String(), ExpressionTypeBool, firstNonBool(left, right))
		}
		return ExpressionTypeBool, nil
	case BinaryOpEqual, BinaryOpNotEqual, BinaryOpLess, BinaryOpLessOrEqual, BinaryOpGreater, BinaryOpGreaterOrEqual:
		if left != right || (op != BinaryOpEqual && op != BinaryOpNotEqual && left == ExpressionTypeBool) {
			return 0, fmt.Errorf("%w: comparison operands are %s and %s", ErrExpressionType, left, right)
		}
		return ExpressionTypeBool, nil
	case BinaryOpAdd:
		if left == ExpressionTypeString && right == ExpressionTypeString {
			return ExpressionTypeString, nil
		}
		fallthrough
	case BinaryOpSubtract, BinaryOpMultiply, BinaryOpDivide:
		if !isNumber(left) || !isNumber(right) {
			return 0, fmt.Errorf("%w: %s requires numeric operands", ErrExpressionType, op)
		}
		if left == ExpressionTypeDecimal || right == ExpressionTypeDecimal {
			return ExpressionTypeDecimal, nil
		}
		return ExpressionTypeInt, nil
	default:
		return 0, fmt.Errorf("%w: binary operator is unspecified", ErrExpressionInvalid)
	}
}

func firstNonBool(a, b ExpressionType) ExpressionType {
	if a != ExpressionTypeBool {
		return a
	}
	return b
}

func callType(name string, indexes []int, nodes []IRNode, children []Expr) (ExpressionType, error) {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "read"), strings.Contains(lower, "write"), strings.Contains(lower, "file"), strings.Contains(lower, "http"), strings.Contains(lower, "net"), strings.Contains(lower, "db"), strings.Contains(lower, "fetch"), strings.Contains(lower, "reflect"):
		return 0, forbiddenCallError(name)
	case lower == "now", lower == "today", lower == "timestamp", lower == "clock", lower == "current_time":
		return 0, fmt.Errorf("%w: %s", ErrExpressionTime, name)
	case strings.Contains(lower, "random"), lower == "rand", lower == "uuid":
		return 0, fmt.Errorf("%w: %s", ErrExpressionRandom, name)
	}
	if len(indexes) != len(children) {
		return 0, fmt.Errorf("%w: call %s has malformed children", ErrExpressionInvalid, name)
	}
	childTypes := make([]ExpressionType, len(indexes))
	for i, index := range indexes {
		childTypes[i] = nodes[index].Type
	}
	need := func(n int) error {
		if len(childTypes) != n {
			return fmt.Errorf("%w: %s needs %d arguments", ErrExpressionFunction, name, n)
		}
		return nil
	}
	switch lower {
	case "lower", "upper":
		if err := need(1); err != nil {
			return 0, err
		}
		if childTypes[0] != ExpressionTypeString && childTypes[0] != ExpressionTypeUnknown {
			return 0, typeError(name, ExpressionTypeString, childTypes[0])
		}
		return ExpressionTypeString, nil
	case "contains", "starts_with", "ends_with":
		if err := need(2); err != nil {
			return 0, err
		}
		if childTypes[0] != ExpressionTypeString || childTypes[1] != ExpressionTypeString {
			return 0, typeError(name, ExpressionTypeString, firstNonString(childTypes[0], childTypes[1]))
		}
		return ExpressionTypeBool, nil
	case "abs":
		if err := need(1); err != nil {
			return 0, err
		}
		if !isNumber(childTypes[0]) && childTypes[0] != ExpressionTypeUnknown {
			return 0, typeError(name, ExpressionTypeDecimal, childTypes[0])
		}
		return childTypes[0], nil
	case "len":
		if err := need(1); err != nil {
			return 0, err
		}
		if childTypes[0] != ExpressionTypeString && childTypes[0] != ExpressionTypeList && childTypes[0] != ExpressionTypeUnknown {
			return 0, fmt.Errorf("%w: len needs STRING or LIST", ErrExpressionType)
		}
		return ExpressionTypeInt, nil
	default:
		if strings.Contains(lower, "exec") || strings.Contains(lower, "eval") || strings.Contains(lower, "script") || strings.Contains(lower, "code") {
			return 0, fmt.Errorf("%w: %s", ErrExpressionArbitrary, name)
		}
		return 0, fmt.Errorf("%w: %s", ErrExpressionFunction, name)
	}
}

func firstNonString(a, b ExpressionType) ExpressionType {
	if a != ExpressionTypeString {
		return a
	}
	return b
}

func forbiddenCallError(name string) error {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "http") || strings.Contains(lower, "net") || strings.Contains(lower, "file") || strings.Contains(lower, "read") || strings.Contains(lower, "write") || strings.Contains(lower, "db") || strings.Contains(lower, "fetch") {
		return fmt.Errorf("%w: %s", ErrExpressionIO, name)
	}
	return fmt.Errorf("%w: %s", ErrExpressionArbitrary, name)
}

func dependenciesDigest(deps []Dependency) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.rules.ExpressionDependencies", ExpressionVersion).Count("dependencies", len(deps))
	for _, dep := range deps {
		w.String("name", dep.Name).String("version", dep.Version).String("digest", dep.Digest)
	}
	return w.Digest()
}

func compiledDigest(version string, semantics UnknownSemantics, inputs []Input, deps []Dependency, depDigest string, ir IR, cost Cost) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.rules.CompiledExpression", ExpressionVersion).String("version", version).String("unknown_semantics", semantics.String()).String("dependency_digest", depDigest)
	w.Count("inputs", len(inputs))
	for _, input := range inputs {
		w.String("input.name", input.Name).String("input.type", input.Type.String()).String("input.element_type", input.ElementType.String())
	}
	w.Count("dependencies", len(deps))
	for _, dep := range deps {
		w.String("dependency.name", dep.Name).String("dependency.version", dep.Version).String("dependency.digest", dep.Digest)
	}
	w.Int("root", int64(ir.Root)).Count("nodes", len(ir.Nodes))
	for _, node := range ir.Nodes {
		w.Int("opcode", int64(node.Opcode)).String("type", node.Type.String()).String("name", node.Name).String("function", node.Function).String("unary", node.Unary.String()).String("binary", node.Binary.String()).String("iteration", node.Iteration.String()).Int("bound", int64(node.IterationBound))
		if node.Opcode == OpcodeLiteral {
			w.Value("value", node.Value)
		}
		w.Count("children", len(node.Children))
		for _, child := range node.Children {
			w.Int("child", int64(child))
		}
	}
	w.Int("cost.nodes", int64(cost.Nodes)).Int("cost.depth", int64(cost.Depth)).Int("cost.iterations", int64(cost.Iterations)).Int("cost.total", int64(cost.Total))
	digest, err := w.Digest()
	if err != nil {
		return "", fmt.Errorf("%w: digest: %v", ErrExpressionInvalid, err)
	}
	return digest, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
