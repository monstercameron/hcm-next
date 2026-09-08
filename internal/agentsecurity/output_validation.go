package agentsecurity

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// OutputReferenceOwner resolves references against the authoritative store.
// It is intentionally an observation port: validation never creates or
// mutates a referenced object.
type OutputReferenceOwner interface {
	Exists(context.Context, string) (bool, error)
}

// OutputFieldAuthorizer is the existing AuthZ owner for output fields.
type OutputFieldAuthorizer interface {
	AuthorizeFields(context.Context, string, string, []string) error
}

// OutputClaimOwner determines whether a model claim is supported by governed
// evidence. Claims without an owner are rejected rather than treated as facts.
type OutputClaimOwner interface {
	Supports(context.Context, string) (bool, error)
}

// AgentOutput is the bounded shape passed to deterministic draft consumers.
// Narrative is explanatory text only; it is never interpreted as a plan.
type AgentOutput struct {
	Schema     string
	Value      any
	References []string
	Fields     []string
	Claims     []string
	Narrative  string
}

// DraftOutput is a validated, effect-free draft. Its typed result is the only
// value suitable for deterministic consumption.
type DraftOutput struct {
	Result     TypedResult
	References []string
	Fields     []string
	Claims     []string
	Narrative  string
}

// DraftValue is the validator-owned structural description of a draft. The
// tool's deterministic schema validator returns this type; agent-authored
// parallel declarations are checked for an exact match and never establish
// authority by themselves.
type DraftValue interface {
	DraftFields() []string
	DraftReferences() []string
	DraftClaims() []string
	DetachDraft() (DraftValue, error)
	DraftCanonicalBytes() ([]byte, error)
}

// ValidateDraftOutput validates an output after admission. All external
// checks are delegated to their owning ports; a failure returns a typed
// refusal and no draft value.
func (g *ToolGateway) ValidateDraftOutput(ctx context.Context, admission Admission, toolName string, output AgentOutput, refs OutputReferenceOwner, fields OutputFieldAuthorizer, claims OutputClaimOwner) (DraftOutput, error) {
	if g == nil {
		return DraftOutput{}, refusal(RefusalInvalid, "gateway", "nil gateway")
	}
	if strings.TrimSpace(output.Schema) == "" || nilValue(output.Value) {
		return DraftOutput{}, refusal(RefusalOutput, "schema", "schema and value are required")
	}
	tool, ok := g.tools[toolName]
	if !ok || tool.Class != ToolDraft {
		return DraftOutput{}, refusal(RefusalOutput, "tool", "only a registered draft tool can produce a deterministic draft")
	}
	result, err := g.ValidateOutput(admission, toolName, output.Value)
	if err != nil {
		return DraftOutput{}, err
	}
	if result.Schema != output.Schema {
		return DraftOutput{}, refusal(RefusalOutput, "schema", "output schema does not match the validated type")
	}
	shape, ok := result.Value.(DraftValue)
	if !ok || nilValue(shape) {
		return DraftOutput{}, refusal(RefusalOutput, "value", "validated draft has no structural metadata")
	}
	actualFields, ok := normalized(shape.DraftFields())
	if !ok || !sameDeclared(actualFields, output.Fields) {
		return DraftOutput{}, refusal(RefusalOutput, "fields", "declared fields do not match the validated draft")
	}
	actualRefs, ok := normalized(shape.DraftReferences())
	if !ok || !sameDeclared(actualRefs, output.References) {
		return DraftOutput{}, refusal(RefusalOutput, "references", "declared references do not match the validated draft")
	}
	actualClaims, ok := normalized(shape.DraftClaims())
	if !ok || !sameDeclared(actualClaims, output.Claims) {
		return DraftOutput{}, refusal(RefusalOutput, "claims", "declared claims do not match the validated draft")
	}
	for _, id := range actualRefs {
		if strings.TrimSpace(id) == "" || refs == nil {
			return DraftOutput{}, refusal(RefusalOutput, "references", "reference owner is required")
		}
		exists, err := refs.Exists(ctx, id)
		if err != nil || !exists {
			return DraftOutput{}, refusal(RefusalOutput, "references", fmt.Sprintf("reference %q is not authoritative", id))
		}
	}
	if len(actualFields) > 0 {
		if fields == nil {
			return DraftOutput{}, refusal(RefusalOutput, "fields", "field authorization owner is required")
		}
		if err := fields.AuthorizeFields(ctx, admission.Tenant, admission.Purpose, cloneStrings(actualFields)); err != nil {
			return DraftOutput{}, refusal(RefusalOutput, "fields", "output contains unauthorized fields")
		}
	}
	for _, claim := range actualClaims {
		if strings.TrimSpace(claim) == "" || claims == nil {
			return DraftOutput{}, refusal(RefusalOutput, "claims", "claim support owner is required")
		}
		supported, err := claims.Supports(ctx, claim)
		if err != nil || !supported {
			return DraftOutput{}, refusal(RefusalOutput, "claims", "output contains an unsupported claim")
		}
	}
	originalMaterial, err := shape.DraftCanonicalBytes()
	if err != nil || len(originalMaterial) == 0 {
		return DraftOutput{}, refusal(RefusalOutput, "value", "validated draft has no canonical material")
	}
	detached, err := shape.DetachDraft()
	if err != nil || nilValue(detached) || reflect.TypeOf(detached) != reflect.TypeOf(shape) {
		return DraftOutput{}, refusal(RefusalOutput, "value", "validated draft cannot be detached from tool-owned memory")
	}
	detachedMaterial, err := detached.DraftCanonicalBytes()
	if err != nil || !bytes.Equal(detachedMaterial, originalMaterial) {
		return DraftOutput{}, refusal(RefusalOutput, "value", "detached draft changed canonical material")
	}
	detachedFields, fieldsOK := normalized(detached.DraftFields())
	detachedRefs, refsOK := normalized(detached.DraftReferences())
	detachedClaims, claimsOK := normalized(detached.DraftClaims())
	if !fieldsOK || !refsOK || !claimsOK || !sameDeclared(actualFields, detachedFields) || !sameDeclared(actualRefs, detachedRefs) || !sameDeclared(actualClaims, detachedClaims) {
		return DraftOutput{}, refusal(RefusalOutput, "value", "detached draft changed structural metadata")
	}
	result.Value = detached
	receipt, err := digestValidatedResult(result)
	if err != nil || receipt != result.semanticReceipt {
		return DraftOutput{}, refusal(RefusalOutput, "value", "detached draft changed validated receipt")
	}
	return DraftOutput{Result: result, References: actualRefs, Fields: actualFields, Claims: actualClaims, Narrative: output.Narrative}, nil
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func normalized(in []string) ([]string, bool) {
	out := cloneStrings(in)
	for _, value := range out {
		if strings.TrimSpace(value) == "" {
			return nil, false
		}
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, false
		}
	}
	return out, true
}

func sameDeclared(actual, declared []string) bool {
	want, ok := normalized(declared)
	if !ok || len(actual) != len(want) {
		return false
	}
	for i := range actual {
		if actual[i] != want[i] {
			return false
		}
	}
	return true
}
