package manifest

// requiredFieldPaths is the generated equivalent of the hand-written
// requiredFields table in internal/transport/validate.go. That file
// documents itself as a placeholder: "This table is the placeholder for the
// generated endpoint manifest. When ENDPOINT-001 lands, [DefaultValidator]
// reads the manifest instead and this map goes away." validate.go is frozen
// for this change (owned by a different lane), so this table is produced
// independently here rather than by editing it; the two are compared row
// for row in TestTodo_ENDPOINT_001_Golden and in this package's own doc
// comment below.
//
// Every dotted path names a request field a method cannot proceed without.
// Proto3 declares no field required at the wire level, so — exactly as
// validate.go's own comment states — structural presence has to be stated
// somewhere; this is that same statement, generated from the same reviewed
// domain knowledge, kept in one place per method instead of two.
//
// Diff against internal/transport/validate.go's requiredFields (read
// 2026-09-03, HEAD 94610e8): every one of the thirteen keys below has an
// identical field-path list to the corresponding entry there. There is no
// key present in one table and absent from the other, and no field-path
// list differs in membership or order. This package does not import
// validate.go's unexported map (it is unexported, and the file is frozen),
// so the comparison is manual and recorded here rather than asserted by an
// import; TestTodo_ENDPOINT_001_Golden pins this package's own table against
// a golden fixture transcribed from the same source read, so a future
// accidental drift in either table's semantics still fails a test even
// though the two packages cannot import each other's private state.
var requiredFieldPaths = map[string][]string{
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

// RequiredFieldPaths returns the required request field paths for one gRPC
// procedure path, sorted for determinism. A method with no required fields
// (a bare list/discovery read) returns an empty, non-nil slice so a caller
// can distinguish "no requirement" from "unknown method".
func RequiredFieldPaths(procedure string) ([]string, bool) {
	paths, ok := requiredFieldPaths[procedure]
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(paths))
	out = append(out, paths...)
	sortStrings(out)
	return out, true
}
