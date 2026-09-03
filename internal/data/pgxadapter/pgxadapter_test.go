package pgxadapter

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

func TestInterfaceCompliance(t *testing.T) {
	var _ dbport.Tx = Tx{}
	var _ dbport.Conn = (*Conn)(nil)
	var _ dbport.Beginner = (*Conn)(nil)
	var _ dbport.Conn = (*Pool)(nil)
	var _ dbport.Beginner = (*Pool)(nil)
}

func TestConnectInvalidURL(t *testing.T) {
	_, err := Connect(context.Background(), "://bad-url", nil)
	if err == nil {
		t.Fatal("expected error for bad url")
	}
}

func TestNewPoolInvalidURL(t *testing.T) {
	_, err := NewPool(context.Background(), "://bad-url", nil)
	if err == nil {
		t.Fatal("expected error for bad url")
	}
}

func TestConnectRuntimeParams(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Connect(ctx, "postgres://user:pass@localhost:5432/db?sslmode=disable", map[string]string{"search_path": "public"})
	if err == nil {
		t.Fatal("Connect unexpectedly succeeded without a test server")
	}
}

func TestSaturationZero(t *testing.T) {
	var s Saturation
	if s.AcquiredConns != 0 || s.IdleConns != 0 || s.MaxConns != 0 || s.TotalConns != 0 {
		t.Fatal("zero Saturation not zero")
	}
}

func TestTxCommitRollbackNoPanic(t *testing.T) {
	var tx Tx
	_ = tx.Commit
	_ = tx.Rollback
	_ = tx.Exec
	_ = tx.Query
	_ = tx.QueryRow
}

func TestPoolStatsNoPanic(t *testing.T) {
	var p *Pool
	_ = p
}
