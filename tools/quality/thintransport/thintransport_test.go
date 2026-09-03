package thintransport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/thintransport"
)

func writeSource(t *testing.T, root, rel, source string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestTransportRejectsBusinessAndPersistenceImports is ARCH-GO-023's primary
// boundary test. It uses source fixtures so it cannot accidentally validate a
// package graph altered by generated code or runtime wiring.
func TestTransportRejectsBusinessAndPersistenceImports(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "internal/transport/grpc/handler.go", `package grpc
import (
  "github.com/monstercameron/hcm-next/internal/domains/leave"
  "github.com/monstercameron/hcm-next/internal/data/postgres"
  "github.com/jackc/pgx/v5"
)
var _ = leave.Request{}
var _ postgres.Store
var _ pgx.Tx
`)
	writeSource(t, root, "internal/transport/middleware/identity.go", `package middleware
import "github.com/monstercameron/hcm-next/internal/intent"
var _ intent.Type
`)
	findings := thintransport.Check(root)
	if len(findings) != 3 {
		t.Fatalf("findings = %v, want three forbidden imports", findings)
	}
	if !strings.Contains(findings[0].Code, "business") && !strings.Contains(findings[1].Code, "business") {
		t.Fatalf("business import was not reported: %v", findings)
	}
}

func TestThinTransportAllowsApplicationAndGeneratedContracts(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "internal/transport/grpcbridge/handler.go", `package grpcbridge
import (
  "context"
  intents "github.com/monstercameron/hcm-next/internal/intent/app"
  "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
)
var _ context.Context
var _ intents.Service
var _ v1.IntentServiceServer
`)
	if got := thintransport.Check(root); len(got) != 0 {
		t.Fatalf("allowed transport rejected: %v", got)
	}
}

func TestThinTransportSkipsTestsAndGeneratedFiles(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "internal/transport/grpc/bad_test.go", `package grpc
import "github.com/jackc/pgx/v5"
var _ pgx.Tx
`)
	writeSource(t, root, "internal/transport/grpc/generated.gen.go", `package grpc
import "database/sql"
var _ *sql.DB
`)
	if got := thintransport.Check(root); len(got) != 0 {
		t.Fatalf("excluded files reported: %v", got)
	}
}

func TestTodo_ARCH_GO_023_Golden(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "internal/transport/grpcserver/server.go", `package grpcserver
import "github.com/monstercameron/hcm-next/internal/data/store"
`)
	got := thintransport.Check(root)
	if len(got) != 1 || got[0].Code != "persistence-import" || got[0].Line != 2 {
		t.Fatalf("unexpected golden finding: %v", got)
	}
}
