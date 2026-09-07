package sbom

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GraphEdge is one line of `go mod graph`: FromPath/FromVersion requires
// ToPath/ToVersion. FromVersion is empty when From is the main module
// itself (`go mod graph` never prints a version for it).
type GraphEdge struct {
	FromPath    string
	FromVersion string
	ToPath      string
	ToVersion   string
}

// ModGraph runs `go mod graph` from root and parses its output.
//
// Unlike `go list -m -json all` (see doc.go), `go mod graph` succeeds on
// this repository: it only prints the require edges recorded in every
// go.mod already fetched into the local module cache, and never has to
// validate or load a transitively-reachable module's own package tree the
// way `-m ... all` resolution does.
//
// The edges printed here are pre-MVS-selection: the same module can appear
// at more than one version across different edges, because `go mod graph`
// shows every version any go.mod in the graph asked for, not only the one
// Go's minimal version selection finally picked. Generate filters these
// edges down to the ones whose endpoints match the versions go.mod actually
// selected, so the emitted dependency graph reflects modules as built, not
// every version MVS considered and discarded.
func ModGraph(root string) ([]GraphEdge, error) {
	// An SBOM root is a module root. Without this check `go mod graph`
	// would climb to any enclosing go.mod (a temp directory routed under
	// the checkout, for one) and describe the wrong module.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return nil, fmt.Errorf("sbom: %s is not a module root: %w", root, err)
	}
	cmd := exec.Command("go", "mod", "graph")
	cmd.Dir = root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sbom: go mod graph: %w\nstderr:\n%s", err, stderr.String())
	}

	var edges []GraphEdge
	scanner := bufio.NewScanner(&stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("sbom: go mod graph:%d: expected 2 fields, got %d: %q", lineNo, len(fields), line)
		}
		fromPath, fromVersion := splitModAt(fields[0])
		toPath, toVersion := splitModAt(fields[1])
		edges = append(edges, GraphEdge{
			FromPath:    fromPath,
			FromVersion: fromVersion,
			ToPath:      toPath,
			ToVersion:   toVersion,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("sbom: scanning go mod graph output: %w", err)
	}
	return edges, nil
}

// splitModAt splits a `go mod graph` node ("path@version" or, for the main
// module, plain "path") into its path and version.
func splitModAt(node string) (path, version string) {
	if i := strings.LastIndex(node, "@"); i >= 0 {
		return node[:i], node[i+1:]
	}
	return node, ""
}
