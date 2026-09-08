package pgtest

// Scratch development-boot holder: starts the shared embedded server and
// blocks until interrupted so migrate/serve/gateway can share one database.
// NOT part of the suite: guarded by HCMNEXT_DEVBOOT and deleted after use.

import (
	"fmt"
	"os"
	"os/signal"
	"testing"
)

func TestDevBootHold(t *testing.T) {
	if os.Getenv("HCMNEXT_DEVBOOT") == "" {
		t.Skip("dev boot holder only")
	}
	if err := ensureServer(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("DEVBOOT_PG_URL=%s\n", ServerURL())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
