package sbom

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SumHash is the decoded content hash go.sum recorded for one module@version.
type SumHash struct {
	Alg     string // always HashAlgSHA256 for go.sum's "h1:" hashes
	Content string // lowercase hex digest
}

// ParseGoSum reads root/go.sum and returns, for every "module version
// h1:<base64>" line (the module's own content hash — the "module
// version/go.mod h1:<base64>" lines hash only the go.mod file and are
// skipped), the decoded hash keyed by "module@version".
//
// go.sum's h1 scheme stores base64-standard-encoded SHA-256 digests
// (golang.org/x/mod/sumdb/dirhash's H1 format); CycloneDX hash content is
// hex, so each digest is re-encoded here.
func ParseGoSum(root string) (map[string]SumHash, error) {
	path := filepath.Join(root, "go.sum")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SumHash{}, nil
		}
		return nil, fmt.Errorf("sbom: opening go.sum: %w", err)
	}
	defer f.Close()

	hashes := make(map[string]SumHash)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("sbom: go.sum:%d: expected 3 fields, got %d: %q", lineNo, len(fields), line)
		}
		module, version, sum := fields[0], fields[1], fields[2]
		if strings.HasSuffix(version, "/go.mod") {
			// This line hashes the dependency's go.mod file, not its
			// module content; the BOM records the content hash only.
			continue
		}
		const prefix = "h1:"
		if !strings.HasPrefix(sum, prefix) {
			// Unknown hash scheme (future-proofing): skip rather than fail
			// the whole BOM over a hash format this generator predates.
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sum, prefix))
		if err != nil {
			return nil, fmt.Errorf("sbom: go.sum:%d: decoding h1 hash for %s@%s: %w", lineNo, module, version, err)
		}
		hashes[module+"@"+version] = SumHash{Alg: HashAlgSHA256, Content: hex.EncodeToString(raw)}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("sbom: scanning go.sum: %w", err)
	}
	return hashes, nil
}
