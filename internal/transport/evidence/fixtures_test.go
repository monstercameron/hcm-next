package evidence

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// csvCell parses csvBytes (id, <fieldNames...> header, one row per
// dimension) and returns the value of column for the row named id.
func csvCell(t *testing.T, csvBytes []byte, id, column string) string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(csvBytes)).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	col := -1
	for i, name := range rows[0] {
		if name == column {
			col = i
		}
	}
	if col < 0 {
		t.Fatalf("CSV has no %q column: %v", column, rows[0])
	}
	for _, row := range rows[1:] {
		if row[0] == id {
			return row[col]
		}
	}
	t.Fatalf("CSV has no row for %q", id)
	return ""
}

// jsonFieldValue parses the MACHINE_DATA document produced by
// renderDimensionArtifacts and returns the named field's value for the
// record named id.
func jsonFieldValue(t *testing.T, jsonBytes []byte, id, field string) string {
	t.Helper()
	var doc struct {
		Records []struct {
			ID     string `json:"id"`
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"records"`
	}
	if err := json.Unmarshal(jsonBytes, &doc); err != nil {
		t.Fatalf("parse machine JSON: %v", err)
	}
	for _, rec := range doc.Records {
		if rec.ID != id {
			continue
		}
		for _, f := range rec.Fields {
			if f.Name == field {
				return f.Value
			}
		}
	}
	t.Fatalf("machine JSON has no %s.%s", id, field)
	return ""
}

// trustFixtureVerifier returns a trust.Verifier that always authenticates as
// testSubject in testTenant, authorized for every purpose this package's
// tests use. It is the same "any bearer token verifies as this one fixed
// principal" shape internal/transport/otelmw's own harness uses, adapted to
// a real gRPC integration test rather than a direct transport.Admit call.
func trustFixtureVerifier(t *testing.T) trust.Verifier {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: testTenant, Subject: testSubject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-" + testSubject, IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(100000, 0),
		CredentialDigest: "credential-" + testSubject, Purposes: []string{testPurpose, "redacted_view"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })
}
