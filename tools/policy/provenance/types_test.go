package provenance_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/provenance"
)

func validStatement() provenance.Statement {
	return provenance.Statement{
		SchemaVersion: provenance.SchemaVersion,
		PredicateType: provenance.PredicateType,
		GeneratedAt:   "2026-09-05T00:00:00Z",
		Subjects: []provenance.Subject{
			{Name: "hcmnext", SHA256: strings.Repeat("ab", 32)},
		},
		Builder: provenance.Builder{ID: provenance.BuilderID},
		Source: provenance.SourceRef{
			Repository: provenance.RootModulePath,
			Ref:        "unknown",
			Commit:     "unknown",
		},
		BuildConfig: provenance.BuildConfig{
			GoVersion:    "go1.26.3",
			GOOS:         "windows",
			GOARCH:       "arm64",
			Flags:        []string{"-trimpath=true"},
			ConfigDigest: strings.Repeat("cd", 32),
		},
		SBOM: provenance.SBOMReference{
			Path:   provenance.DefaultSBOMPath,
			SHA256: strings.Repeat("ef", 32),
		},
	}
}

func TestStatementValidateAcceptsAWellFormedStatement(t *testing.T) {
	if v := validStatement().Validate(); len(v) != 0 {
		t.Fatalf("valid statement failed Validate: %v", v)
	}
}

func TestStatementValidateRejectsEachMissingField(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*provenance.Statement)
		wantHit string
	}{
		{"no schema version", func(s *provenance.Statement) { s.SchemaVersion = 0 }, "schema_version"},
		{"no predicate type", func(s *provenance.Statement) { s.PredicateType = "" }, "predicate_type"},
		{"no generated at", func(s *provenance.Statement) { s.GeneratedAt = "" }, "generated_at"},
		{"no subjects", func(s *provenance.Statement) { s.Subjects = nil }, "subjects"},
		{"unnamed subject", func(s *provenance.Statement) { s.Subjects[0].Name = "" }, "subjects"},
		{"undigested subject", func(s *provenance.Statement) { s.Subjects[0].SHA256 = "" }, "subjects"},
		{"no builder id", func(s *provenance.Statement) { s.Builder.ID = "" }, "builder.id"},
		{"no source repository", func(s *provenance.Statement) { s.Source.Repository = "" }, "source.repository"},
		{"no source ref", func(s *provenance.Statement) { s.Source.Ref = "" }, "source.ref"},
		{"no source commit", func(s *provenance.Statement) { s.Source.Commit = "" }, "source.commit"},
		{"no go version", func(s *provenance.Statement) { s.BuildConfig.GoVersion = "" }, "build_config.go_version"},
		{"no config digest", func(s *provenance.Statement) { s.BuildConfig.ConfigDigest = "" }, "build_config.config_digest"},
		{"no sbom digest", func(s *provenance.Statement) { s.SBOM.SHA256 = "" }, "sbom.sha256"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validStatement()
			s.Subjects = append([]provenance.Subject(nil), s.Subjects...)
			tc.mutate(&s)
			violations := s.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected a violation for %q, got none", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.Field, tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a violation touching field %q, got %v", tc.wantHit, violations)
			}
		})
	}
}

func TestViolationStringFormatsFieldAndIssue(t *testing.T) {
	v := provenance.Violation{Field: "subjects", Issue: "missing"}
	if got, want := v.String(), "subjects: missing"; got != want {
		t.Errorf("Violation.String() = %q, want %q", got, want)
	}
}
