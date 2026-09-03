package integration

import (
	"slices"
	"sync"
	"testing"
)

// TestTodo_DB_014_Integration exercises the acceptance boundary exposed by
// this package: every modeled integration family is represented by a valid,
// tenant-scoped metadata contract, and the contract can be consumed without
// storing provider secrets or payload bytes.
func TestTodo_DB_014_Integration(t *testing.T) {
	t.Parallel()
	if err := Validate(Tables); err != nil {
		t.Fatalf("DB-014 contract rejected: %v", err)
	}
	want := map[string]string{
		"connector_connection": "REVISIONED_FACT",
		"mapping_profile":      "REVISIONED_REFERENCE",
		"connector_operation":  "REVISIONED_FACT",
		"message_intent":       "REVISIONED_FACT",
		"delivery_attempt":     "IMMUTABLE_EVIDENCE",
		"message_receipt":      "IMMUTABLE_EVIDENCE",
		"document":             "REVISIONED_FACT",
		"document_signature":   "REVISIONED_FACT",
		"artifact_object":      "IMMUTABLE_EVIDENCE",
		"artifact_reference":   "IMMUTABLE_EVIDENCE",
	}
	for name, lifecycle := range want {
		var got *Table
		for i := range Tables {
			if Tables[i].Name == name {
				got = &Tables[i]
				break
			}
		}
		if got == nil {
			t.Fatalf("acceptance contract omitted %s", name)
		}
		if got.Lifecycle != lifecycle || !got.TenantKey {
			t.Fatalf("%s lifecycle=%q tenant=%v, want %q and tenant scoped", name, got.Lifecycle, got.TenantKey, lifecycle)
		}
		if !slices.Contains(got.Columns, "created_at") {
			t.Fatalf("%s has no durable recording timestamp", name)
		}
	}
}

// TestTodo_DB_014_Mutation proves that the structural guard rejects the
// omissions and unsafe fields called out by DB-014's RED clause.
func TestTodo_DB_014_Mutation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func([]Table) []Table
	}{
		{"duplicate table", func(ts []Table) []Table { return append(ts, ts[0]) }},
		{"unscoped table", func(ts []Table) []Table { ts[0].TenantKey = false; return ts }},
		{"unknown lifecycle", func(ts []Table) []Table { ts[0].Lifecycle = "MUTABLE_SECRET"; return ts }},
		{"raw payload", func(ts []Table) []Table { ts[0].Columns = append(ts[0].Columns, "raw_payload"); return ts }},
		{"raw credential", func(ts []Table) []Table { ts[0].Columns = append(ts[0].Columns, "credential"); return ts }},
		{"missing tenant column", func(ts []Table) []Table {
			ts[0].Columns = slices.DeleteFunc(ts[0].Columns, func(c string) bool { return c == "tenant_id" })
			return ts
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := cloneTables()
			if err := Validate(tc.mutate(ts)); err == nil {
				t.Fatalf("mutation %q was accepted", tc.name)
			}
		})
	}
}

// TestTodo_DB_014_Property checks deterministic, order-independent validation
// and the invariants required of every table descriptor.
func TestTodo_DB_014_Property(t *testing.T) {
	t.Parallel()
	if err := Validate(Tables); err != nil {
		t.Fatal(err)
	}
	for i, table := range Tables {
		if table.Name == "" || table.Lifecycle == "" || !table.TenantKey {
			t.Fatalf("table %d has incomplete identity metadata: %+v", i, table)
		}
		if len(table.Columns) < 3 || !slices.Contains(table.Columns, "tenant_id") || !slices.Contains(table.Columns, "created_at") {
			t.Fatalf("table %s lacks required metadata columns", table.Name)
		}
		if err := Validate([]Table{table}); err != nil {
			t.Fatalf("single-table validation for %s: %v", table.Name, err)
		}
	}
	if err := Validate(cloneTables()); err != nil {
		t.Fatal("cloned contract is not deterministic:", err)
	}
}

// TestTodo_DB_014_Race validates the read-only contract concurrently. This
// catches accidental mutation of the package-level table publication.
func TestTodo_DB_014_Race(t *testing.T) {
	t.Parallel()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if err := Validate(Tables); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if Tables[0].Name == "" {
		t.Fatal("concurrent validation corrupted the published contract")
	}
}

func cloneTables() []Table {
	clone := make([]Table, len(Tables))
	for i, table := range Tables {
		clone[i] = table
		clone[i].Columns = slices.Clone(table.Columns)
	}
	return clone
}
