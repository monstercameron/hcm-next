package deferredschema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// PreviewFile is one generated file's name and exact contents.
type PreviewFile struct {
	Name    string
	Content string
}

// PreviewSet is the complete, deterministic output of Generate: one
// migration preview per domain plus the single disposition preview file.
type PreviewSet struct {
	Files []PreviewFile
}

// Generate renders the full DB-016 preview set from Domains(): ten migration
// previews (NNN_<slug>.sql) and one storage-disposition.deferred.yaml, in
// fixed, sorted-by-name order. It has no side effects -- it never touches
// disk -- and calling it twice always returns byte-identical output, which
// is exactly what generate_test.go's golden and race tests both depend on.
func Generate() (PreviewSet, error) {
	domains := Domains()

	files := make([]PreviewFile, 0, len(domains)+1)
	for _, d := range domains {
		files = append(files, PreviewFile{
			Name:    d.PreviewFileName(),
			Content: RenderMigrationPreview(d),
		})
	}

	dispositionYAML, err := RenderDispositionPreviewYAML(domains)
	if err != nil {
		return PreviewSet{}, err
	}
	files = append(files, PreviewFile{
		Name:    "storage-disposition.deferred.yaml",
		Content: dispositionYAML,
	})

	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return PreviewSet{Files: files}, nil
}

// Digest returns the pinned, deterministic sha256 digest of the whole
// preview set: every file name and its exact content, concatenated in
// sorted-by-name order with explicit separators so no two different file
// sets can collide on the same digest.
func (p PreviewSet) Digest() string {
	h := sha256.New()
	files := append([]PreviewFile(nil), p.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, f := range files {
		fmt.Fprintf(h, "FILE %s\nLEN %d\n", f.Name, len(f.Content))
		h.Write([]byte(f.Content))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Lookup returns a file's content by name.
func (p PreviewSet) Lookup(name string) (string, bool) {
	for _, f := range p.Files {
		if f.Name == name {
			return f.Content, true
		}
	}
	return "", false
}
