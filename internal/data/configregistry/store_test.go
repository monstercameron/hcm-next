package configregistry

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestNewReturnsAStoreImplementingThePort(t *testing.T) {
	t.Parallel()
	// nil satisfies DB at the type level for this purely structural check;
	// no method on it is invoked.
	var db DB
	s := New(db)
	if s == nil {
		t.Fatal("New returned nil")
	}
}

func TestWithTenantRefusesAnInvalidTenantIDBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	// s.db is nil: if withTenant reached s.db.Begin before validating the
	// tenant id, this test would panic on a nil interface call instead of
	// returning a clean error. Reaching the error path proves the uuid
	// parse happens first.
	s := &Store{db: nil}
	err := s.withTenant(context.Background(), "not-a-uuid", func(tx dbport.Tx) error {
		t.Fatal("fn must never be invoked when the tenant id fails to parse")
		return nil
	})
	if err == nil {
		t.Fatal("want an error for an invalid tenant id, got nil")
	}
	if !strings.Contains(err.Error(), "not-a-uuid") {
		t.Fatalf("error %q does not name the offending tenant id", err.Error())
	}
}
