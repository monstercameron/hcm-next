// Package discover walks a directory of reference workflow documents and
// parses each one, keeping per-file failures isolated so one malformed or
// unreadable document never prevents the rest of the batch from being
// reported.
package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/parse"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

// FileResult is one *.md file's outcome: either a parsed Document, or an
// error explaining why it could not be parsed.
type FileResult struct {
	RelPath string
	Doc     *model.Document
	Err     error
}

// Documents lists every *.md file directly inside dir (no recursion --
// reference workflow documents are not nested), parses each with v, and
// returns one FileResult per file in deterministic file-name order. RelPath
// is computed relative to repoRoot with forward slashes, so report output
// does not depend on where the repository happens to be checked out.
//
// A read or parse failure for one file is recorded on its FileResult.Err
// and does not stop the remaining files from being processed: the caller
// decides how to render a partial batch.
func Documents(repoRoot, dir string, v *vocab.Vocabulary) ([]FileResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Only the .md suffix is required, not a regular-file type check: a
	// path that carries the suffix but cannot actually be read as a file
	// (for example a directory or a broken symlink) still belongs in the
	// batch, so its per-file read error is reported like any other
	// malformed document instead of being silently dropped here.
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	results := make([]FileResult, 0, len(names))
	for _, name := range names {
		full := filepath.Join(dir, name)
		rel, relErr := filepath.Rel(repoRoot, full)
		if relErr != nil {
			rel = full
		}
		rel = filepath.ToSlash(rel)

		doc, parseErr := parse.ParseFile(full, rel, v)
		results = append(results, FileResult{RelPath: rel, Doc: doc, Err: parseErr})
	}
	return results, nil
}
