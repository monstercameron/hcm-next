package otel_test

import (
	"os"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// collectorConfigPath is relative to this package directory
// (internal/platform/telemetry/otel, four levels above the repository
// root), mirroring internal/platform/telemetry/contract_test.go's own
// convention for definitions/telemetry/*.yaml.
const collectorConfigPath = "../../../../definitions/telemetry/collector/otel-collector.yaml"

// canonicalProhibitedAttributeKeys is this test's own copy of the
// PROHIBITED-class example keys planning/todos.md OBS-001's RED clause
// names ("salary/medical/bank/case/prompt/payload data or worker/request
// identifiers") merged with internal/platform/logging's default redaction
// denylist (internal/platform/logging/redact.go's defaultDeniedKeys, which
// this test cannot import — it is unexported). Both
// definitions/telemetry/collector/otel-collector.yaml's
// attributes/redact_prohibited processor and this list are hand-maintained
// against the same source; this test is what keeps them from silently
// drifting apart, the same role contract_test.go plays for the other
// definitions/telemetry/*.yaml files.
var canonicalProhibitedAttributeKeys = []string{
	"password", "passwd", "secret", "token", "access_token", "refresh_token",
	"api_key", "authorization", "ssn", "social_security_number",
	"bank_account", "bank_account_number", "bank_routing", "routing_number",
	"credit_card", "card_number", "cvv",
	"medical", "medical_record", "medical_condition", "diagnosis",
	"prompt", "raw_identity",
	"email", "email_address", "phone", "phone_number", "date_of_birth", "dob",
	"salary", "compensation", "base_pay",
	"case_notes", "case_file",
	"worker_id", "request_body", "payload",
}

type collectorAttributeAction struct {
	Key    string `yaml:"key"`
	Action string `yaml:"action"`
}

type collectorAttributesProcessor struct {
	Actions []collectorAttributeAction `yaml:"actions"`
}

type collectorPipeline struct {
	Receivers  []string `yaml:"receivers"`
	Processors []string `yaml:"processors"`
	Exporters  []string `yaml:"exporters"`
}

type collectorDoc struct {
	// yaml.Node values, not *yaml.Node: gopkg.in/yaml.v3 does not correctly
	// populate a map's *yaml.Node values through Decode (verified against
	// this exact file — each decoded node comes back with a zero Kind and
	// no Content), only its yaml.Node values.
	Receivers  map[string]yaml.Node `yaml:"receivers"`
	Processors map[string]yaml.Node `yaml:"processors"`
	Exporters  map[string]yaml.Node `yaml:"exporters"`
	Service    struct {
		Pipelines map[string]collectorPipeline `yaml:"pipelines"`
	} `yaml:"service"`
}

func loadCollectorDoc(t *testing.T) collectorDoc {
	t.Helper()
	data, err := os.ReadFile(collectorConfigPath)
	if err != nil {
		t.Fatalf("reading %s: %v", collectorConfigPath, err)
	}
	var doc collectorDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", collectorConfigPath, err)
	}
	return doc
}

// TestCollectorPipelineConfigParsesWithReceiversProcessorsExporters proves
// definitions/telemetry/collector/otel-collector.yaml is well-formed YAML
// declaring the OBS-002 Collector pipeline shape: an OTLP/HTTP receiver, the
// redaction/batch/memory_limiter processors, a debug exporter and a
// placeholder OTLP exporter, wired into a traces/metrics/logs pipeline that
// references only components the file itself declares.
func TestCollectorPipelineConfigParsesWithReceiversProcessorsExporters(t *testing.T) {
	doc := loadCollectorDoc(t)

	otlpNode, ok := doc.Receivers["otlp"]
	if !ok {
		t.Fatal(`receivers does not declare "otlp"`)
	}
	var otlpReceiver struct {
		Protocols struct {
			HTTP map[string]any `yaml:"http"`
		} `yaml:"protocols"`
	}
	if err := otlpNode.Decode(&otlpReceiver); err != nil {
		t.Fatalf("decoding otlp receiver: %v", err)
	}
	if otlpReceiver.Protocols.HTTP == nil {
		t.Fatal(`receivers.otlp.protocols does not declare "http"`)
	}

	for _, want := range []string{"memory_limiter", "attributes/redact_prohibited", "batch"} {
		if _, ok := doc.Processors[want]; !ok {
			t.Fatalf("processors does not declare %q", want)
		}
	}

	for _, want := range []string{"debug", "otlp/placeholder"} {
		if _, ok := doc.Exporters[want]; !ok {
			t.Fatalf("exporters does not declare %q", want)
		}
	}

	if len(doc.Service.Pipelines) == 0 {
		t.Fatal("service.pipelines declares no pipelines")
	}
	for _, name := range []string{"traces", "metrics", "logs"} {
		p, ok := doc.Service.Pipelines[name]
		if !ok {
			t.Fatalf("service.pipelines does not declare %q", name)
		}
		for _, r := range p.Receivers {
			if _, ok := doc.Receivers[r]; !ok {
				t.Errorf("pipeline %q references undeclared receiver %q", name, r)
			}
		}
		for _, proc := range p.Processors {
			if _, ok := doc.Processors[proc]; !ok {
				t.Errorf("pipeline %q references undeclared processor %q", name, proc)
			}
		}
		for _, e := range p.Exporters {
			if _, ok := doc.Exporters[e]; !ok {
				t.Errorf("pipeline %q references undeclared exporter %q", name, e)
			}
		}
	}
}

// TestCollectorPipelineConfigRedactionCoversEveryProhibitedClassKey proves
// the attributes/redact_prohibited processor deletes every key in this
// test's canonical PROHIBITED-class set (see
// canonicalProhibitedAttributeKeys) — the Collector-boundary half of OBS-002
// GREEN's "owned adapters emit the exact approved metrics/traces/logs",
// applied as defense in depth alongside internal/platform/telemetry/otel's
// in-process Evaluator (see the YAML file's own header comment).
func TestCollectorPipelineConfigRedactionCoversEveryProhibitedClassKey(t *testing.T) {
	doc := loadCollectorDoc(t)

	node, ok := doc.Processors["attributes/redact_prohibited"]
	if !ok {
		t.Fatal(`processors does not declare "attributes/redact_prohibited"`)
	}
	var ap collectorAttributesProcessor
	if err := (&node).Decode(&ap); err != nil {
		t.Fatalf("decoding attributes/redact_prohibited: %v", err)
	}

	deleted := make(map[string]bool, len(ap.Actions))
	for _, a := range ap.Actions {
		if a.Action != "delete" {
			t.Errorf("action for key %q is %q, want \"delete\" (this processor's role is prohibited-content removal, not transformation)", a.Key, a.Action)
			continue
		}
		deleted[a.Key] = true
	}

	var missing []string
	for _, key := range canonicalProhibitedAttributeKeys {
		if !deleted[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("attributes/redact_prohibited does not delete: %v", missing)
	}
}
