package bufprotovalidatekit

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Message is the generated-contract boundary accepted by Validator. It is an
// alias so callers do not depend on a validation library's message wrapper.
type Message = proto.Message

// Scope declares the only authority a validation rule may exercise.
type Scope string

const (
	ScopeStructural    Scope = "structural"
	ScopeLocal         Scope = "local"
	ScopeBusiness      Scope = "business"
	ScopeAuthorization Scope = "authorization"
	ScopeLegal         Scope = "legal"
	ScopeEligibility   Scope = "eligibility"
	ScopeMutation      Scope = "mutation"
)

// RuleKind is a closed set of deterministic request-shape checks. CEL is
// named only so an attempted CEL rule receives an explicit fail-closed error.
type RuleKind string

const (
	RuleRequired      RuleKind = "required"
	RuleUTF8          RuleKind = "utf8"
	RuleMaxBytes      RuleKind = "max_bytes"
	RulePattern       RuleKind = "pattern"
	RuleAnyRegistered RuleKind = "any_registered"
	RuleCEL           RuleKind = "cel"
)

var (
	// ErrAuthorityScope means a rule attempted to decide owned HCM semantics or
	// perform a mutation rather than inspect local request shape.
	ErrAuthorityScope = errors.New("validation rule requests forbidden authority")
	// ErrUnsupportedRule means the backend cannot safely compile the rule.
	ErrUnsupportedRule = errors.New("validation rule is unsupported")
	// ErrInvalidRule means a structural rule does not match its descriptor.
	ErrInvalidRule = errors.New("validation rule is invalid")
)

// Rule is a backend-neutral structural constraint. Expression is retained to
// reject attempted CEL publication explicitly; it is never evaluated.
type Rule struct {
	RuleRef    string   `json:"rule_ref"`
	FieldPath  string   `json:"field_path"`
	Kind       RuleKind `json:"kind"`
	Scope      Scope    `json:"scope"`
	Limit      int      `json:"limit,omitempty"`
	Expression string   `json:"expression,omitempty"`
}

// Spec is the complete, publish-time validation input.
type Spec struct {
	Message            string   `json:"message"`
	Rules              []Rule   `json:"rules"`
	AllowedAnyTypeURLs []string `json:"allowed_any_type_urls"`
}

// Violation is the owned safe field error returned identically to every
// transport. Description is static and never includes rejected input.
type Violation struct {
	Code        string `json:"code"`
	FieldPath   string `json:"field_path"`
	Description string `json:"description"`
	RuleRef     string `json:"rule_ref"`
}

type compiledRule struct {
	rule    Rule
	field   protoreflect.FieldDescriptor
	pattern *regexp.Regexp
}

// Validator is immutable after Compile and safe for concurrent validation.
type Validator struct {
	messageDescriptor protoreflect.MessageDescriptor
	rules             []compiledRule
	allowedAny        map[string]struct{}
}

type compileError struct {
	cause   error
	ruleRef string
	detail  string
}

func (e *compileError) Error() string {
	if e.ruleRef == "" {
		return fmt.Sprintf("%v: %s", e.cause, e.detail)
	}
	return fmt.Sprintf("%v: rule %q: %s", e.cause, e.ruleRef, e.detail)
}

func (e *compileError) Unwrap() error { return e.cause }

// Compile validates every rule before publication. It intentionally supports
// no cross-message lookup, external call, mutable callback, or CEL evaluator.
func Compile(descriptor protoreflect.MessageDescriptor, spec Spec) (*Validator, error) {
	if descriptor == nil {
		return nil, &compileError{cause: ErrInvalidRule, detail: "message descriptor is required"}
	}
	if spec.Message != string(descriptor.FullName()) {
		return nil, &compileError{cause: ErrInvalidRule, detail: "spec message does not match descriptor"}
	}
	validator := &Validator{
		messageDescriptor: descriptor,
		rules:             make([]compiledRule, 0, len(spec.Rules)),
		allowedAny:        make(map[string]struct{}, len(spec.AllowedAnyTypeURLs)),
	}
	for _, typeURL := range spec.AllowedAnyTypeURLs {
		if !validLocalTypeURL(descriptor.ParentFile(), typeURL) {
			return nil, &compileError{cause: ErrInvalidRule, detail: "Any allow-list entry is not a message in the local descriptor file"}
		}
		if _, duplicate := validator.allowedAny[typeURL]; duplicate {
			return nil, &compileError{cause: ErrInvalidRule, detail: "duplicate Any allow-list entry"}
		}
		validator.allowedAny[typeURL] = struct{}{}
	}
	seenRefs := make(map[string]struct{}, len(spec.Rules))
	for _, rule := range spec.Rules {
		compiled, err := compileRule(descriptor, rule)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenRefs[rule.RuleRef]; duplicate {
			return nil, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "duplicate rule_ref"}
		}
		seenRefs[rule.RuleRef] = struct{}{}
		validator.rules = append(validator.rules, compiled)
	}
	return validator, nil
}

func compileRule(descriptor protoreflect.MessageDescriptor, rule Rule) (compiledRule, error) {
	if rule.Scope != ScopeStructural && rule.Scope != ScopeLocal {
		return compiledRule{}, &compileError{cause: ErrAuthorityScope, ruleRef: rule.RuleRef, detail: "only structural and local scopes are permitted"}
	}
	if rule.RuleRef == "" || rule.FieldPath == "" || strings.Contains(rule.FieldPath, ".") {
		return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "rule_ref and one local field path are required"}
	}
	field := descriptor.Fields().ByName(protoreflect.Name(rule.FieldPath))
	if field == nil {
		return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "field does not exist"}
	}
	compiled := compiledRule{rule: rule, field: field}
	switch rule.Kind {
	case RuleRequired:
		if rule.Limit != 0 || rule.Expression != "" {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "required rule has unexpected parameters"}
		}
	case RuleUTF8:
		if field.Kind() != protoreflect.StringKind {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "UTF-8 rule requires a string field"}
		}
	case RuleMaxBytes:
		if field.Kind() != protoreflect.StringKind && field.Kind() != protoreflect.BytesKind {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "max_bytes requires a string or bytes field"}
		}
		if rule.Limit <= 0 {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "max_bytes limit must be positive"}
		}
	case RulePattern:
		if field.Kind() != protoreflect.StringKind || rule.Expression == "" {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "pattern requires a string field and expression"}
		}
		pattern, err := regexp.Compile(rule.Expression)
		if err != nil {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "pattern does not compile"}
		}
		compiled.pattern = pattern
	case RuleAnyRegistered:
		if field.Kind() != protoreflect.MessageKind || field.Message().FullName() != "google.protobuf.Any" {
			return compiledRule{}, &compileError{cause: ErrInvalidRule, ruleRef: rule.RuleRef, detail: "any_registered requires google.protobuf.Any"}
		}
	case RuleCEL:
		return compiledRule{}, &compileError{cause: ErrUnsupportedRule, ruleRef: rule.RuleRef, detail: "CEL publication is not qualified"}
	default:
		return compiledRule{}, &compileError{cause: ErrUnsupportedRule, ruleRef: rule.RuleRef, detail: "unknown rule kind"}
	}
	return compiled, nil
}

func validLocalTypeURL(file protoreflect.FileDescriptor, typeURL string) bool {
	const prefix = "type.googleapis.com/"
	if !strings.HasPrefix(typeURL, prefix) {
		return false
	}
	name := protoreflect.FullName(strings.TrimPrefix(typeURL, prefix))
	if !name.IsValid() {
		return false
	}
	for i := 0; i < file.Messages().Len(); i++ {
		if file.Messages().Get(i).FullName() == name {
			return true
		}
	}
	return false
}

// Validate returns stable, safe violations in declared rule order. It never
// changes the message and has no mutation or external-effect hook.
func (v *Validator) Validate(input Message) []Violation {
	if v == nil || input == nil || (reflect.ValueOf(input).Kind() == reflect.Pointer && reflect.ValueOf(input).IsNil()) {
		return []Violation{{Code: "INVALID_MESSAGE", FieldPath: "_message", Description: "message is unavailable", RuleRef: "validator.message"}}
	}
	message := input.ProtoReflect()
	if !message.IsValid() {
		return []Violation{{Code: "INVALID_MESSAGE", FieldPath: "_message", Description: "message is unavailable", RuleRef: "validator.message"}}
	}
	if message.Descriptor().FullName() != v.messageDescriptor.FullName() {
		return []Violation{{Code: "INVALID_MESSAGE", FieldPath: "_message", Description: "message type does not match validator", RuleRef: "validator.message"}}
	}
	violations := make([]Violation, 0)
	for _, compiled := range v.rules {
		if violation, failed := v.evaluate(message, compiled); failed {
			violations = append(violations, violation)
		}
	}
	return violations
}

func (v *Validator) evaluate(message protoreflect.Message, compiled compiledRule) (Violation, bool) {
	rule := compiled.rule
	value := message.Get(compiled.field)
	makeViolation := func(code, description string) (Violation, bool) {
		return Violation{Code: code, FieldPath: rule.FieldPath, Description: description, RuleRef: rule.RuleRef}, true
	}
	switch rule.Kind {
	case RuleRequired:
		if isAbsent(message, compiled.field, value) {
			return makeViolation("REQUIRED", "required field is absent")
		}
	case RuleUTF8:
		if !utf8.ValidString(value.String()) {
			return makeViolation("INVALID_UTF8", "field is not valid UTF-8")
		}
	case RuleMaxBytes:
		length := 0
		if compiled.field.Kind() == protoreflect.StringKind {
			length = len(value.String())
		} else {
			length = len(value.Bytes())
		}
		if length > rule.Limit {
			return makeViolation("MAX_BYTES", fmt.Sprintf("field exceeds %d bytes", rule.Limit))
		}
	case RulePattern:
		if !compiled.pattern.MatchString(value.String()) {
			return makeViolation("PATTERN", "field does not match the required shape")
		}
	case RuleAnyRegistered:
		if !message.Has(compiled.field) {
			return Violation{}, false
		}
		dynamic := value.Message()
		typeURLField := dynamic.Descriptor().Fields().ByName("type_url")
		valueField := dynamic.Descriptor().Fields().ByName("value")
		if typeURLField == nil || valueField == nil {
			return makeViolation("INVALID_ANY", "dynamic message has an invalid structure")
		}
		typeURL := dynamic.Get(typeURLField).String()
		if _, allowed := v.allowedAny[typeURL]; !allowed {
			return makeViolation("ANY_TYPE_NOT_ALLOWED", "dynamic message type is not registered")
		}
		if len(dynamic.Get(valueField).Bytes()) == 0 {
			return makeViolation("ANY_VALUE_EMPTY", "dynamic message payload is empty")
		}
	}
	return Violation{}, false
}

func isAbsent(message protoreflect.Message, field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
	if field.HasPresence() {
		return !message.Has(field)
	}
	switch field.Kind() {
	case protoreflect.StringKind:
		return value.String() == ""
	case protoreflect.BytesKind:
		return len(value.Bytes()) == 0
	case protoreflect.BoolKind:
		return !value.Bool()
	case protoreflect.EnumKind:
		return value.Enum() == field.Enum().Values().Get(0).Number()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return value.Int() == 0
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return value.Uint() == 0
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return value.Float() == 0
	default:
		return !message.Has(field)
	}
}
