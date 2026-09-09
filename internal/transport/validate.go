package transport

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// Validator performs strict structural validation of a decoded request. It is
// a port so that the generated endpoint manifest (ENDPOINT-001) can replace
// the hand-written rule table below without touching either transport.
//
// A Validator decides structure only: presence, bounds, encoding. Whether a
// business operation is permitted, timely, in-scope or consistent is a
// capability decision and never appears here.
type Validator interface {
	// Validate returns nil when msg is structurally admissible for method, and
	// an owned INVALID_ARGUMENT error naming the offending field paths
	// otherwise.
	Validate(method string, msg proto.Message) *envelope.Error
}

// Rule identifiers for structural rejections.
const (
	reasonStructuralRejection = "structural.request_rejected"
	ruleUnknownField          = "strict_decoding.unknown_field"
	ruleRequiredField         = "structural_validation.required_field"
	ruleStringBound           = "structural_validation.string_length"
	rulePageSizeBound         = "structural_validation.page_size"
)

// Structural bounds. They are conservative transport limits, not business
// limits: a domain that needs a shorter identifier enforces that itself.
const (
	// maxStringFieldBytes bounds any single string field on a request.
	maxStringFieldBytes = 4096
	// maxPageSize is the largest page a list method will accept, per the
	// bounded-page-size rule in the endpoint contract.
	maxPageSize = 1000
)

// requiredFields lists the request fields a method cannot proceed without,
// keyed by fully qualified gRPC method name and expressed as dotted field
// paths. Proto3 has no required fields, so structural presence has to be
// stated somewhere; stating it here keeps it identical on both transports.
//
// This table is the placeholder for the generated endpoint manifest. When
// ENDPOINT-001 lands, [DefaultValidator] reads the manifest instead and this
// map goes away.
var requiredFields = map[string][]string{
	"/hcmnext.intents.v1.IntentService/CreateIntent":       {"idempotency_key", "definition.intent_type_id", "request.schema.schema_id"},
	"/hcmnext.intents.v1.IntentService/GetIntent":          {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ListIntents":        nil,
	"/hcmnext.intents.v1.IntentService/SimulateIntent":     {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ExecuteIntent":      {"idempotency_key", "intent_id", "approval.proposal_revision_id", "approval.approval_ref"},
	"/hcmnext.intents.v1.IntentService/SubmitIntent":       {"idempotency_key", "intent_id", "proposal_revision_id"},
	"/hcmnext.intents.v1.IntentService/CancelIntent":       {"idempotency_key", "intent_id", "reason_ref"},
	"/hcmnext.intents.v1.IntentService/SupersedeIntent":    {"idempotency_key", "superseded_intent_id", "definition.intent_type_id", "reason_ref"},
	"/hcmnext.intents.v1.IntentService/ExplainIntent":      {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ListIntentTimeline": {"intent_id"},

	"/hcmnext.registry.v1.RegistryService/ListIntentDefinitions": nil,
	"/hcmnext.registry.v1.RegistryService/GetIntentDefinition":   {"definition.intent_type_id"},
	"/hcmnext.registry.v1.RegistryService/ListCapabilities":      nil,
	"/hcmnext.registry.v1.RegistryService/GetCapability":         {"capability_id"},
}

// DefaultValidator is the strict structural validator both transports use.
type DefaultValidator struct{}

// Validate implements [Validator].
func (DefaultValidator) Validate(method string, msg proto.Message) *envelope.Error {
	if msg == nil {
		return nil
	}
	return Validate(method, msg)
}

// Validate is [DefaultValidator.Validate] as a function.
//
// It applies, in order: material unknown Protobuf fields, string bounds,
// page-size bounds, then required-field presence. Every violation found is
// reported, not just the first, so a caller fixes one round trip's worth of
// problems at a time.
func Validate(method string, msg proto.Message) *envelope.Error {
	if msg == nil {
		return nil
	}
	var violations []envelope.Violation
	walkMessage(msg.ProtoReflect(), "", &violations)

	for _, path := range requiredFields[method] {
		if !fieldPopulated(msg.ProtoReflect(), path) {
			violations = append(violations, envelope.Violation{
				FieldPath:   path,
				Description: "the field is required for this method",
				RuleRef:     ruleRequiredField,
			})
		}
	}

	if len(violations) == 0 {
		return nil
	}
	err := envelope.New(envelope.CodeInvalidArgument, reasonStructuralRejection,
		"the request is malformed or structurally invalid")
	for _, v := range violations {
		err.WithViolation(v.FieldPath, v.Description, v.RuleRef)
	}
	return err
}

// walkMessage collects every structural violation reachable from m.
func walkMessage(m protoreflect.Message, path string, out *[]envelope.Violation) {
	if len(m.GetUnknown()) > 0 {
		*out = append(*out, envelope.Violation{
			FieldPath:   emptyToRoot(path),
			Description: "the message carries unknown fields, which this contract does not accept",
			RuleRef:     ruleUnknownField,
		})
	}
	if string(m.Descriptor().FullName()) == "hcmnext.common.v1.PageRequest" {
		checkPageRequest(m, path, out)
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		child := joinPath(path, string(fd.Name()))
		switch {
		case fd.IsMap():
			if fd.MapValue().Kind() == protoreflect.MessageKind {
				v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
					walkMessage(mv.Message(), child+"["+k.String()+"]", out)
					return true
				})
			}
			return true
		case fd.IsList():
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				elem := child + "[" + strconv.Itoa(i) + "]"
				switch fd.Kind() {
				case protoreflect.MessageKind, protoreflect.GroupKind:
					walkMessage(list.Get(i).Message(), elem, out)
				case protoreflect.StringKind:
					checkString(list.Get(i).String(), elem, out)
				}
			}
			return true
		}
		switch fd.Kind() {
		case protoreflect.MessageKind, protoreflect.GroupKind:
			walkMessage(v.Message(), child, out)
		case protoreflect.StringKind:
			checkString(v.String(), child, out)
		}
		return true
	})
}

// checkString enforces the transport string bound.
func checkString(s, path string, out *[]envelope.Violation) {
	if len(s) > maxStringFieldBytes {
		*out = append(*out, envelope.Violation{
			FieldPath:   path,
			Description: fmt.Sprintf("the field exceeds the %d byte transport bound", maxStringFieldBytes),
			RuleRef:     ruleStringBound,
		})
	}
}

// checkPageRequest enforces the bounded page-size rule.
func checkPageRequest(m protoreflect.Message, path string, out *[]envelope.Violation) {
	fd := m.Descriptor().Fields().ByName("page_size")
	if fd == nil {
		return
	}
	size := m.Get(fd).Int()
	if size < 0 || size > maxPageSize {
		*out = append(*out, envelope.Violation{
			FieldPath:   joinPath(path, "page_size"),
			Description: fmt.Sprintf("the page size must be between 0 and %d", maxPageSize),
			RuleRef:     rulePageSizeBound,
		})
	}
}

// fieldPopulated reports whether the dotted path resolves to a populated
// scalar or message on m. An intermediate absent message means the leaf is
// absent too.
func fieldPopulated(m protoreflect.Message, path string) bool {
	name, rest, nested := strings.Cut(path, ".")
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return false
	}
	if !nested {
		if fd.Kind() == protoreflect.StringKind && !fd.IsList() {
			return m.Get(fd).String() != ""
		}
		if fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap() {
			return m.Has(fd)
		}
		if fd.IsList() {
			return m.Get(fd).List().Len() > 0
		}
		return m.Has(fd) || m.Get(fd).IsValid()
	}
	if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() || !m.Has(fd) {
		return false
	}
	return fieldPopulated(m.Get(fd).Message(), rest)
}

// joinPath appends a field name to a dotted path.
func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// emptyToRoot names the request root for a violation with no field path.
func emptyToRoot(path string) string {
	if path == "" {
		return "(request)"
	}
	return path
}
