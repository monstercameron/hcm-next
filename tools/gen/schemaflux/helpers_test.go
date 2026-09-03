package schemaflux_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// findRepoRoot walks up from the working directory to the first ancestor
// containing go.mod, mirroring tools/gen's own findRepoRoot (unexported in
// that package, so this package keeps its own copy rather than reaching into
// a frozen sibling).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root: no go.mod found in any parent directory")
		}
		dir = parent
	}
}

// blockingTransport is an http.RoundTripper that refuses every request and
// counts how many it refused. Installing it as http.DefaultTransport for the
// duration of a test makes any real network access from that test physically
// impossible: there is no code path in net/http that reaches the network
// without going through the configured Transport.
type blockingTransport struct {
	dials atomic.Int64
}

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b.dials.Add(1)
	return nil, errors.New("schemaflux qualification fixture: network access blocked (" + req.URL.String() + ")")
}

// installBlockingTransport replaces http.DefaultTransport with a
// blockingTransport for the duration of the calling test and restores the
// original on cleanup.
func installBlockingTransport(t *testing.T) *blockingTransport {
	t.Helper()
	previous := http.DefaultTransport
	bt := &blockingTransport{}
	http.DefaultTransport = bt
	t.Cleanup(func() { http.DefaultTransport = previous })
	return bt
}
