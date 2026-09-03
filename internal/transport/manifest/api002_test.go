package manifest

import "testing"

func api002Version() Version {
	return Version{
		EndpointID: "hcmnext.intents.v1.IntentService/GetIntent",
		Version:    1, Status: LifecycleActive, SemanticDigest: "sem-v1",
		RequestFields:  map[string]ContractField{"id": {Type: "string", Required: true}, "cursor": {Type: "string"}},
		ResponseFields: map[string]ContractField{"id": {Type: "string", Required: true}, "name": {Type: "string"}},
		ErrorCodes:     map[string]string{"NOT_FOUND": "resource absent"},
	}
}

func TestTodo_API_002(t *testing.T) {
	old := api002Version()
	current := old
	current.Version = 2
	current.ResponseFields = map[string]ContractField{"id": {Type: "string", Required: true}, "name": {Type: "string"}, "display": {Type: "string"}}
	if got := Check(old, current); !got.OK() || got.Decision != DecisionCompatible {
		t.Fatalf("additive change = %+v, want COMPATIBLE", got)
	}

	broken := current
	broken.Version = 3
	broken.SemanticDigest = "sem-v2"
	broken.ResponseFields = map[string]ContractField{"id": {Type: "int64", Required: true}, "display": {Type: "string"}}
	got := Check(current, broken)
	if got.Decision != DecisionMigrate || len(got.Breaks) != 3 {
		t.Fatalf("breaking change = %+v, want MIGRATE with exact breaks", got)
	}

	deprecated := current
	deprecated.Status = LifecycleDeprecated
	deprecated.Consumers = []Consumer{{ID: "payroll", Tenant: "tenant-a", Required: true, Active: true, AdoptedVersion: 1}}
	if got := GateLifecycle(current, deprecated); got.Decision != DecisionBlock || len(got.Consumers) != 1 {
		t.Fatalf("deprecated consumer gate = %+v, want BLOCK", got)
	}

	retired := current
	retired.Version = 3
	retired.Status = LifecycleRetired
	retired.MigrationEvidence = "migration:2026-09-03"
	retired.Consumers = []Consumer{{ID: "payroll", Tenant: "tenant-a", Required: true, Active: true, AdoptedVersion: retired.Version}}
	if got := GateLifecycle(current, retired); got.Decision != DecisionCompatible {
		t.Fatalf("retirement with evidence/adoption = %+v, want COMPATIBLE", got)
	}
}

func TestTodo_API_002_Integration(t *testing.T) {
	manifest, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, endpoint := range manifest.Endpoints {
		v := Version{EndpointID: endpoint.EndpointID, Version: 1, Status: LifecycleActive, SemanticDigest: string(endpoint.IntentBehavior)}
		if got := Check(v, Version{EndpointID: endpoint.EndpointID, Version: 2, Status: LifecycleActive, SemanticDigest: v.SemanticDigest}); !got.OK() {
			t.Fatalf("endpoint %s additive lifecycle check = %+v", endpoint.EndpointID, got)
		}
	}
}

func TestTodo_API_002_Security(t *testing.T) {
	old := api002Version()
	current := old
	current.Version = 2
	current.SemanticDigest = "changed"
	got := Check(old, current)
	for _, b := range got.Breaks {
		if b.Code == BreakSemantic && b.Detail == "" {
			t.Fatal("semantic break omitted evidence")
		}
	}
	if got.Decision != DecisionMigrate {
		t.Fatalf("semantic change was masked: %+v", got)
	}
}

func TestTodo_API_002_Conformance(t *testing.T) {
	old := api002Version()
	current := old
	current.Version = 2
	current.RequestFields = make(map[string]ContractField, len(old.RequestFields))
	for path, field := range old.RequestFields {
		current.RequestFields[path] = field
	}
	current.RequestFields["cursor"] = ContractField{Type: "bytes"}
	got := Check(old, current)
	if len(got.Breaks) != 1 || got.Breaks[0].Path != "cursor" || got.Breaks[0].Code != BreakFieldType {
		t.Fatalf("field conformance = %+v", got)
	}
}

func FuzzTodo_API_002(f *testing.F) {
	f.Add(uint32(1), uint32(2))
	f.Fuzz(func(t *testing.T, oldVersion, newVersion uint32) {
		old := api002Version()
		old.Version = oldVersion
		current := old
		current.Version = newVersion
		_ = Check(old, current)
	})
}
