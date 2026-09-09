package provenance

// PredicateType identifies the provenance schema this Statement follows.
// This is a documentation string (the SLSA v1.0 provenance predicate URI),
// not a network fetch or a new dependency - it is never dereferenced.
const PredicateType = "https://slsa.dev/provenance/v1"

// SchemaVersion is this package's own Statement schema revision, bumped
// whenever a field is added or reinterpreted in a way old readers must
// know about.
const SchemaVersion = 1

// Statement is a SLSA-style build provenance statement for one release: the
// Go artifacts it covers (Subjects), who built them (Builder), what source
// state the build ran from (Source), the exact toolchain/flags used
// (BuildConfig), and the SBOM (TOOL-017) this release's dependency
// inventory was published as (SBOM). Field declaration order is fixed and
// is also the order encoding/json emits them in, which is what makes
// CanonicalDigest stable across re-serialization - see sign.go.
type Statement struct {
	SchemaVersion int           `json:"schema_version"`
	PredicateType string        `json:"predicate_type"`
	GeneratedAt   string        `json:"generated_at"`
	Subjects      []Subject     `json:"subjects"`
	Builder       Builder       `json:"builder"`
	Source        SourceRef     `json:"source"`
	BuildConfig   BuildConfig   `json:"build_config"`
	SBOM          SBOMReference `json:"sbom"`
	Signature     *Signature    `json:"signature,omitempty"`
}

// Subject is one Go artifact this statement's provenance chain covers.
type Subject struct {
	// Name identifies the artifact, e.g. "hcmnext" or "cmd/hcmnext".
	Name string `json:"name"`
	// SHA256 is the lowercase hex digest of the artifact's exact bytes.
	SHA256 string `json:"sha256"`
}

// Builder identifies the process that produced Subjects.
type Builder struct {
	// ID names the builder, e.g.
	// "github.com/monstercameron/human-capital-management-suite/tools/policy/provenance/cmd/provgen".
	ID string `json:"id"`
	// Version is that builder's own version/revision, when known; empty is
	// valid (this generator is not itself released/tagged - see doc.go).
	Version string `json:"version,omitempty"`
}

// SourceRef records the source state a build ran from. Commit and Ref are
// read from environment variables rather than a git invocation (see
// doc.go); both are the literal "unknown" when unset, which is a valid,
// explicitly-recorded value, never a silently empty field.
type SourceRef struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref"`
	Commit     string `json:"commit"`
}

// BuildConfig is the exact toolchain and flag set a build ran with, folded
// into one digest so two provenance statements can be compared for build
// reproducibility without diffing every field by hand.
type BuildConfig struct {
	GoVersion    string   `json:"go_version"`
	GOOS         string   `json:"goos"`
	GOARCH       string   `json:"goarch"`
	Flags        []string `json:"flags"`
	ConfigDigest string   `json:"config_digest"`
}

// SBOMReference points a Statement at the CycloneDX SBOM (TOOL-017,
// tools/policy/sbom) describing this release's dependency inventory.
type SBOMReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Signature is an Ed25519 signature over a Statement's CanonicalDigest -
// see sign.go and doc.go's "Signing convention" section.
type Signature struct {
	Algorithm  string `json:"algorithm"`
	PublicKey  string `json:"public_key"`
	Value      string `json:"value"`
	KeyFixture string `json:"key_fixture,omitempty"`
}

// AlgorithmEd25519 is the only Signature.Algorithm value this package
// produces or accepts.
const AlgorithmEd25519 = "ed25519"

// Violation names one structural defect found by Validate.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return v.Field + ": " + v.Issue }

// Validate returns every structural violation on s: missing subjects,
// unnamed/undigested subjects, an empty builder identity, an incomplete
// source ref, a missing build-config digest, or a missing SBOM reference.
// It does not check the signature (see VerifyStatementSignature) or cross
// -check the SBOM digest against a file on disk (see Verify).
func (s Statement) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if s.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if s.PredicateType == "" {
		add("predicate_type", "missing")
	}
	if s.GeneratedAt == "" {
		add("generated_at", "missing")
	}
	if len(s.Subjects) == 0 {
		add("subjects", "missing - a provenance statement must name at least one artifact")
	}
	for _, subj := range s.Subjects {
		if subj.Name == "" {
			add("subjects", "a subject has no name")
		}
		if subj.SHA256 == "" {
			add("subjects", "subject "+subj.Name+" has no sha256 digest")
		}
	}
	if s.Builder.ID == "" {
		add("builder.id", "missing - builder identity is unknown")
	}
	if s.Source.Repository == "" {
		add("source.repository", "missing")
	}
	if s.Source.Ref == "" {
		add("source.ref", "missing")
	}
	if s.Source.Commit == "" {
		add("source.commit", "missing")
	}
	if s.BuildConfig.GoVersion == "" {
		add("build_config.go_version", "missing")
	}
	if s.BuildConfig.ConfigDigest == "" {
		add("build_config.config_digest", "missing")
	}
	if s.SBOM.SHA256 == "" {
		add("sbom.sha256", "missing - no SBOM digest reference")
	}

	return violations
}
