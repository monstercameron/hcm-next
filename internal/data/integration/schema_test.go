package integration

import (
	"slices"
	"testing"
)

func TestTodo_DB_014(t *testing.T) {
	if err := Validate(Tables); err != nil {
		t.Fatal(err)
	}
	if len(Tables) != 17 {
		t.Fatalf("table contract has %d tables, want 17", len(Tables))
	}
	for _, table := range Tables {
		if !slices.Contains(table.Columns, "created_at") {
			t.Errorf("%s has no recording timestamp", table.Name)
		}
	}
}

func TestTodo_DB_014_Security(t *testing.T) {
	for _, table := range Tables {
		if table.Name == "connector_definition" {
			continue
		}
		if !slices.Contains(table.Columns, "tenant_id") {
			t.Errorf("%s is not tenant scoped", table.Name)
		}
	}
	for _, table := range Tables {
		for _, column := range table.Columns {
			if column == "secret" || column == "raw_payload" || column == "content" {
				t.Errorf("%s stores forbidden raw field %s", table.Name, column)
			}
		}
	}
}

func TestTodo_DB_014_Golden(t *testing.T) {
	for _, name := range []string{"connector_connection", "mapping_profile", "connector_operation", "message_intent", "delivery_attempt", "document", "artifact_object", "artifact_reference"} {
		found := false
		for _, table := range Tables {
			if table.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required DB-014 table %q missing", name)
		}
	}
}
