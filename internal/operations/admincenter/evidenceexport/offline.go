package evidenceexport

import (
	"errors"
	"fmt"
)

var ErrTampered = errors.New("evidence export: package tampered")

// Package is the portable, offline-verifiable artifact.  The verifier needs
// no database, network, or exporter process.
type Package struct {
	Manifest Manifest
	Records  []Record
}

func (o Operation) Artifact(records []Record) Package {
	return Package{Manifest: o.Manifest, Records: append([]Record(nil), records...)}
}

func (p Package) Verify() error {
	if !p.Manifest.VerifyDigest() {
		return ErrTampered
	}
	if p.Manifest.RecordCount != len(p.Records) {
		return fmt.Errorf("%w: record count", ErrTampered)
	}
	content := ""
	for _, r := range p.Records {
		content = hash(r.Digest + content)
	}
	if content != p.Manifest.ContentDigest {
		return ErrTampered
	}
	return nil
}

func VerifyOffline(p Package) bool { return p.Verify() == nil }
