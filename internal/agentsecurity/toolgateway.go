// Package agentsecurity contains the bounded, pure boundary for agent tool
// calls. It admits a call and validates a result; deterministic services own
// every effect after admission.
package agentsecurity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ToolClass is the security-relevant class of a tool capability.
type ToolClass string

const (
	ToolRead    ToolClass = "READ"
	ToolAnalyze ToolClass = "ANALYZE"
	ToolDraft   ToolClass = "DRAFT"
	ToolWrite   ToolClass = "WRITE"
	ToolSend    ToolClass = "SEND"
	ToolExecute ToolClass = "EXECUTE"
)

func (c ToolClass) valid() bool {
	switch c {
	case ToolRead, ToolAnalyze, ToolDraft, ToolWrite, ToolSend, ToolExecute:
		return true
	default:
		return false
	}
}

func (c ToolClass) admitted() bool { return c == ToolRead || c == ToolAnalyze || c == ToolDraft }

// AgentIdentity is the caller identity and its original bounded authority.
// Identity is an opaque reference to a verified workload/agent identity; the
// gateway never accepts a raw credential in its place.
type AgentIdentity struct {
	Identity  string
	AgentID   string
	Tenant    string
	Purpose   string
	ToolSet   []string
	DataScope []string
	Budget    int
}

// DelegationLink is one server-resolved link in a root-to-leaf delegation
// chain. A child may only be equal to or narrower than its parent.
type DelegationLink struct {
	GrantID   string
	Delegator string
	Delegate  string
	Tenant    string
	Purpose   string
	ToolSet   []string
	DataScope []string
	Budget    int
}

// ToolDescriptor is an immutable, version-pinned capability descriptor. The
// validator is pure: it validates a result supplied by a deterministic
// service, but it is never an effect port and is never called for a refused
// request.
type ToolDescriptor struct {
	Name       string
	Capability string
	Version    uint32
	Class      ToolClass
	DataScope  []string
	Cost       int
	Schema     string
	Validate   func(any) (TypedResult, error)
}

// ToolCall is the complete security binding for one invocation attempt.
type ToolCall struct {
	Agent      AgentIdentity
	Delegation []DelegationLink
	Tenant     string
	Purpose    string
	Tool       string
	Capability string
	Version    uint32
	Nonce      string
	Args       map[string]any
	ArgsDigest string
	InputTaint []string
	Provenance []string
	CostBudget int
	DataScope  []string
}

// Admission is a pure, effect-free authorization receipt. A deterministic
// service may use it to perform the selected operation and then pass its
// output to ValidateOutput.
type Admission struct {
	AgentID          string
	Tenant           string
	Purpose          string
	Tool             string
	Capability       string
	Version          uint32
	Nonce            string
	ArgsDigest       string
	InputTaint       []string
	Provenance       []string
	Cost             int
	Budget           int
	DataScope        []string
	admissionSeal    *ToolGateway
	admissionReceipt string
}

// RefusalCode is a stable machine-readable refusal reason.
type RefusalCode string

const (
	RefusalInvalid            RefusalCode = "AGENT_INVALID_REQUEST"
	RefusalIdentity           RefusalCode = "AGENT_IDENTITY_REQUIRED"
	RefusalDelegation         RefusalCode = "AGENT_DELEGATION_INVALID"
	RefusalAuthorityExpansion RefusalCode = "AGENT_AUTHORITY_EXPANSION"
	RefusalCapability         RefusalCode = "AGENT_CAPABILITY_NOT_APPROVED"
	RefusalEffectClass        RefusalCode = "AGENT_EFFECT_CLASS_REFUSED"
	RefusalCredential         RefusalCode = "AGENT_RAW_CREDENTIAL_REFUSED"
	RefusalArgsDigest         RefusalCode = "AGENT_ARGUMENT_DIGEST_MISMATCH"
	RefusalBudget             RefusalCode = "AGENT_BUDGET_EXHAUSTED"
	RefusalOutput             RefusalCode = "AGENT_OUTPUT_UNVALIDATED"
)

// Refusal is returned for every governed rejection. Field identifies the
// authority or binding that failed without exposing arguments or payloads.
type Refusal struct {
	Code   RefusalCode
	Field  string
	Detail string
}

func (e *Refusal) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Field == "" {
		return string(e.Code) + ": " + e.Detail
	}
	return string(e.Code) + ": field " + e.Field + ": " + e.Detail
}

// Explain returns an audit-safe summary. It contains digests and identifiers,
// never raw arguments, credentials, payloads or result values.
func (e *Refusal) Explain() string { return e.Error() }

// ToolGateway admits bounded calls and validates typed results. It owns no
// execution port, credential resolver, database handle, clock, or network
// client.
type ToolGateway struct {
	tools        map[string]ToolDescriptor
	detector     Detector
	semanticSeal *ToolGateway
}

// NewToolGateway validates and freezes tool descriptors.
func NewToolGateway(tools []ToolDescriptor) (*ToolGateway, error) {
	result := &ToolGateway{tools: make(map[string]ToolDescriptor, len(tools)), detector: DefaultInstructionDetector}
	result.semanticSeal = result
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.Capability) == "" || tool.Version == 0 || !tool.Class.valid() || tool.Cost <= 0 || strings.TrimSpace(tool.Schema) == "" || tool.Validate == nil {
			return nil, &Refusal{Code: RefusalInvalid, Field: "tool_descriptor", Detail: "name, capability, version, class, positive cost, schema and validator are required"}
		}
		if _, exists := result.tools[tool.Name]; exists {
			return nil, &Refusal{Code: RefusalInvalid, Field: "tool_descriptor.name", Detail: "duplicate tool"}
		}
		tool.DataScope = cloneStrings(tool.DataScope)
		result.tools[tool.Name] = tool
	}
	return result, nil
}

// Admit performs all checks before a deterministic service can receive the
// call. It has no effect port and never invokes a tool implementation.
func (g *ToolGateway) Admit(call ToolCall) (Admission, error) {
	if g == nil {
		return Admission{}, refusal(RefusalInvalid, "gateway", "nil gateway")
	}
	if err := validateCallShape(call); err != nil {
		return Admission{}, err
	}
	if err := validateIdentityAndDelegation(call); err != nil {
		return Admission{}, err
	}
	tool, ok := g.tools[call.Tool]
	if !ok {
		return Admission{}, refusal(RefusalCapability, "tool", "tool is not registered")
	}
	if !tool.Class.admitted() {
		return Admission{}, refusal(RefusalEffectClass, "capability.class", "WRITE, SEND and EXECUTE capabilities are refused")
	}
	if tool.Capability != call.Capability || tool.Version != call.Version {
		return Admission{}, refusal(RefusalCapability, "capability.version", "capability or version is not the registered exact descriptor")
	}
	if !containsString(call.Agent.ToolSet, call.Tool) || !containsString(call.Delegation[len(call.Delegation)-1].ToolSet, call.Tool) {
		return Admission{}, refusal(RefusalAuthorityExpansion, "tool_set", "tool is outside delegated authority")
	}
	if !subset(call.DataScope, tool.DataScope) || !subset(call.DataScope, call.Agent.DataScope) || !subset(call.DataScope, call.Delegation[len(call.Delegation)-1].DataScope) {
		return Admission{}, refusal(RefusalAuthorityExpansion, "data_scope", "data scope is outside delegated authority")
	}
	if rawCredentialPath(call.Args) != "" {
		return Admission{}, refusal(RefusalCredential, "args."+rawCredentialPath(call.Args), "raw credentials are not admissible tool arguments")
	}
	digest, err := DigestArguments(call.Args)
	if err != nil {
		return Admission{}, refusal(RefusalInvalid, "args", "arguments are not canonical JSON")
	}
	if digest != call.ArgsDigest {
		return Admission{}, refusal(RefusalArgsDigest, "args_digest", "exact argument digest does not match")
	}
	if call.CostBudget < tool.Cost || call.CostBudget > call.Agent.Budget || call.CostBudget > call.Delegation[len(call.Delegation)-1].Budget {
		return Admission{}, refusal(RefusalBudget, "cost_budget", "budget is exhausted or exceeds delegated budget")
	}
	admission := Admission{AgentID: call.Agent.AgentID, Tenant: call.Tenant, Purpose: call.Purpose, Tool: call.Tool, Capability: call.Capability, Version: call.Version, Nonce: call.Nonce, ArgsDigest: digest, InputTaint: cloneStrings(call.InputTaint), Provenance: cloneStrings(call.Provenance), Cost: tool.Cost, Budget: call.CostBudget, DataScope: cloneStrings(call.DataScope), admissionSeal: g.semanticSeal}
	admission.admissionReceipt = digestAdmission(admission)
	return admission, nil
}

// Invoke is a convenience for a deterministic service: the service supplies
// output, while the gateway only admits and validates it.
func (g *ToolGateway) Invoke(call ToolCall, output any) (TypedResult, error) {
	admission, err := g.Admit(call)
	if err != nil {
		return TypedResult{}, err
	}
	return g.ValidateOutput(admission, call.Tool, output)
}

// ValidateOutput validates an output after deterministic execution. The
// output validator is the tool's schema boundary, not an execution callback.
func (g *ToolGateway) ValidateOutput(admission Admission, toolName string, output any) (TypedResult, error) {
	if g == nil {
		return TypedResult{}, refusal(RefusalInvalid, "gateway", "nil gateway")
	}
	if g.semanticSeal == nil || admission.admissionSeal != g.semanticSeal || admission.admissionReceipt == "" || admission.admissionReceipt != digestAdmission(admission) {
		return TypedResult{}, refusal(RefusalInvalid, "admission", "admission was not issued unchanged by this gateway")
	}
	tool, ok := g.tools[toolName]
	if !ok || toolName != admission.Tool || tool.Version != admission.Version {
		return TypedResult{}, refusal(RefusalCapability, "capability.version", "admission does not bind the registered tool")
	}
	result, err := tool.Validate(output)
	if err != nil || !result.Validated || result.Schema != tool.Schema || result.Value == nil || len(result.Taint) == 0 || len(result.Provenance) == 0 || !containsAll(result.Taint, admission.InputTaint) || !containsAll(result.Provenance, admission.Provenance) {
		return TypedResult{}, refusal(RefusalOutput, "output", "output is missing deterministic validation, schema, value, taint or provenance")
	}
	result.Taint = cloneStrings(result.Taint)
	result.Provenance = cloneStrings(result.Provenance)
	result.Taint = joinStrings(result.Taint, string(TaintTool))
	result.Provenance = joinStrings(result.Provenance, fmt.Sprintf("tool:%s@%d", tool.Name, tool.Version))
	result.semanticSeal = g.semanticSeal
	receipt, err := digestValidatedResult(result)
	if err != nil {
		return TypedResult{}, refusal(RefusalOutput, "output", "validated output cannot be bound to canonical material")
	}
	result.semanticReceipt = receipt
	return result, nil
}

func digestAdmission(a Admission) string {
	bound := struct {
		AgentID    string   `json:"agent_id"`
		Tenant     string   `json:"tenant"`
		Purpose    string   `json:"purpose"`
		Tool       string   `json:"tool"`
		Capability string   `json:"capability"`
		Version    uint32   `json:"version"`
		Nonce      string   `json:"nonce"`
		ArgsDigest string   `json:"args_digest"`
		InputTaint []string `json:"input_taint"`
		Provenance []string `json:"provenance"`
		Cost       int      `json:"cost"`
		Budget     int      `json:"budget"`
		DataScope  []string `json:"data_scope"`
	}{a.AgentID, a.Tenant, a.Purpose, a.Tool, a.Capability, a.Version, a.Nonce, a.ArgsDigest, a.InputTaint, a.Provenance, a.Cost, a.Budget, a.DataScope}
	encoded, _ := json.Marshal(bound)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-admission/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestValidatedResult(result TypedResult) (string, error) {
	if draft, ok := result.Value.(DraftValue); ok && !nilValue(draft) {
		material, err := draft.DraftCanonicalBytes()
		if err != nil {
			return "", fmt.Errorf("draft canonical material: %w", err)
		}
		if len(material) == 0 {
			return "", errors.New("draft canonical material is empty")
		}
		materialSum := sha256.Sum256(material)
		bound := struct {
			Schema         string   `json:"schema"`
			MaterialDigest string   `json:"material_digest"`
			Validated      bool     `json:"validated"`
			Taint          []string `json:"taint"`
			Provenance     []string `json:"provenance"`
		}{result.Schema, "sha256:" + hex.EncodeToString(materialSum[:]), result.Validated, result.Taint, result.Provenance}
		encoded, err := json.Marshal(bound)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(append([]byte("hcm-next-agent-validated-draft/v1\x00"), encoded...))
		return "sha256:" + hex.EncodeToString(sum[:]), nil
	}
	bound := struct {
		Schema     string   `json:"schema"`
		Value      any      `json:"value"`
		Validated  bool     `json:"validated"`
		Taint      []string `json:"taint"`
		Provenance []string `json:"provenance"`
	}{result.Schema, result.Value, result.Validated, result.Taint, result.Provenance}
	encoded, err := json.Marshal(bound)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent-validated-result/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Explain is an audit-safe projection of a call. It does not validate or
// admit the call and therefore cannot accidentally expose a raw argument.
func (g *ToolGateway) Explain(call ToolCall) string {
	return fmt.Sprintf("agent=%s tenant=%s purpose=%s tool=%s capability=%s version=%d nonce=%s args_digest=%s cost_budget=%d taint_count=%d provenance_count=%d", call.Agent.AgentID, call.Tenant, call.Purpose, call.Tool, call.Capability, call.Version, digestText(call.Nonce), call.ArgsDigest, call.CostBudget, len(call.InputTaint), len(call.Provenance))
}

// DigestArguments computes the exact canonical argument digest expected by
// ToolCall.ArgsDigest. encoding/json sorts map keys, making the result stable
// across insertion order while preserving values and array order.
func DigestArguments(args map[string]any) (string, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent-args/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateCallShape(call ToolCall) error {
	if strings.TrimSpace(call.Tenant) == "" || strings.TrimSpace(call.Purpose) == "" || strings.TrimSpace(call.Tool) == "" || strings.TrimSpace(call.Capability) == "" || call.Version == 0 || strings.TrimSpace(call.Nonce) == "" || call.CostBudget <= 0 {
		return refusal(RefusalInvalid, "binding", "tenant, purpose, tool, capability, version, nonce and positive cost budget are required")
	}
	if len(call.InputTaint) == 0 || len(call.Provenance) == 0 || !validLabels(call.InputTaint) || !validLabels(call.Provenance) {
		return refusal(RefusalInvalid, "taint_provenance", "non-empty taint and provenance labels are required")
	}
	return nil
}

func validateIdentityAndDelegation(call ToolCall) error {
	a := call.Agent
	if strings.TrimSpace(a.Identity) == "" || strings.TrimSpace(a.AgentID) == "" || strings.TrimSpace(a.Tenant) == "" || strings.TrimSpace(a.Purpose) == "" || a.Budget <= 0 || len(a.ToolSet) == 0 || len(a.DataScope) == 0 {
		return refusal(RefusalIdentity, "agent_identity", "verified identity and bounded authority are required")
	}
	if call.Tenant != a.Tenant {
		return refusal(RefusalAuthorityExpansion, "tenant", "call tenant differs from agent identity")
	}
	if call.Purpose != a.Purpose {
		return refusal(RefusalAuthorityExpansion, "purpose", "call purpose differs from agent identity")
	}
	if len(call.Delegation) == 0 {
		return refusal(RefusalDelegation, "delegation", "a non-empty delegation chain is required")
	}
	parentTools, parentData, parentBudget, parentTenant, parentPurpose := a.ToolSet, a.DataScope, a.Budget, a.Tenant, a.Purpose
	for i, link := range call.Delegation {
		if strings.TrimSpace(link.GrantID) == "" || strings.TrimSpace(link.Delegator) == "" || strings.TrimSpace(link.Delegate) == "" || link.Budget <= 0 || link.Tenant == "" || link.Purpose == "" || len(link.ToolSet) == 0 || len(link.DataScope) == 0 {
			return refusal(RefusalDelegation, "delegation", "delegation link is incomplete")
		}
		if link.Tenant != parentTenant {
			return refusal(RefusalAuthorityExpansion, fmt.Sprintf("delegation[%d].tenant", i), "tenant expands or crosses the parent authority")
		}
		if link.Purpose != parentPurpose {
			return refusal(RefusalAuthorityExpansion, fmt.Sprintf("delegation[%d].purpose", i), "purpose expands or changes the parent authority")
		}
		if !subset(link.ToolSet, parentTools) {
			return refusal(RefusalAuthorityExpansion, fmt.Sprintf("delegation[%d].tool_set", i), "tool set expands the parent authority")
		}
		if !subset(link.DataScope, parentData) {
			return refusal(RefusalAuthorityExpansion, fmt.Sprintf("delegation[%d].data_scope", i), "data scope expands the parent authority")
		}
		if link.Budget > parentBudget {
			return refusal(RefusalAuthorityExpansion, fmt.Sprintf("delegation[%d].budget", i), "budget expands the parent authority")
		}
		if i > 0 && call.Delegation[i-1].Delegate != link.Delegator {
			return refusal(RefusalDelegation, fmt.Sprintf("delegation[%d].delegator", i), "delegation chain is not attributable")
		}
		parentTools, parentData, parentBudget, parentTenant, parentPurpose = link.ToolSet, link.DataScope, link.Budget, link.Tenant, link.Purpose
	}
	if call.Delegation[len(call.Delegation)-1].Delegate != a.AgentID {
		return refusal(RefusalDelegation, "delegation.delegate", "leaf delegation does not bind the agent identity")
	}
	return nil
}

func refusal(code RefusalCode, field, detail string) error {
	return &Refusal{Code: code, Field: field, Detail: detail}
}

func validLabels(labels []string) bool {
	for _, label := range labels {
		if strings.TrimSpace(label) == "" || strings.TrimSpace(label) != label {
			return false
		}
	}
	return true
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func subset(values, allowed []string) bool {
	for _, value := range values {
		if !containsString(allowed, value) {
			return false
		}
	}
	return true
}

func containsAll(values, required []string) bool { return subset(required, values) }

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func rawCredentialPath(args map[string]any) string {
	var walk func(any, string) string
	walk = func(value any, path string) string {
		switch v := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				lower := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
				if strings.Contains(lower, "credential") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "privatekey") || strings.Contains(lower, "token") || lower == "authorization" || strings.Contains(lower, "apikey") {
					return joinPath(path, key)
				}
				if found := walk(v[key], joinPath(path, key)); found != "" {
					return found
				}
			}
		case []any:
			for i, item := range v {
				if found := walk(item, fmt.Sprintf("%s[%d]", path, i)); found != "" {
					return found
				}
			}
		default:
			if value != nil && reflect.ValueOf(value).Kind() == reflect.Pointer && !reflect.ValueOf(value).IsNil() {
				return walk(reflect.ValueOf(value).Elem().Interface(), path)
			}
		}
		return ""
	}
	return walk(args, "")
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
