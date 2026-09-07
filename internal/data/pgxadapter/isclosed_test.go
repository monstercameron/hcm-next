package pgxadapter_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestConnIsClosedTracksTheUnderlyingConnection(t *testing.T) {
	db := pgtest.New(t)
	conn := db.NewConn(t)
	if conn.IsClosed() {
		t.Fatal("a freshly opened connection reports closed")
	}
	if err := conn.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !conn.IsClosed() {
		t.Fatal("a closed connection reports open")
	}
}
