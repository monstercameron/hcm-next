// Package provenance implements TOOL-018 (sign and verify release
// provenance) and SUPPLY-001 (produce signed SBOM and build provenance for
// every Go artifact).
//
// # Statement shape
//
// [Statement] is a SLSA-style build provenance statement: a subject list
// naming every Go artifact this release covers by name and sha256 digest
// ([Subject]), the builder identity that produced them ([Builder]), the
// source ref/commit the build ran from ([SourceRef]), the exact toolchain
// and build flags used, folded into one reproducible digest
// ([BuildConfig]), and a reference to the CycloneDX SBOM this release's
// dependency inventory was published as ([SBOMReference], see
// tools/policy/sbom, TOOL-017). [Generate] assembles a Statement for the
// hcmnext binary by building it fresh into a temporary directory (never
// committing the binary itself - only its digest is recorded) and hashing
// the checked-in SBOM file already on disk.
//
// # Signing convention
//
// [Statement.CanonicalDigest] hashes every field except Signature itself
// (sha256 over a fixed-order JSON projection), exactly the shape
// tools/planning/gateevidence.P1AManifest.CanonicalDigest uses for its own
// signed manifest (NEXT-002). [SignStatement] and [VerifyStatementSignature]
// cover that digest with Ed25519, using the same
// schema_version/algorithm/public_key/private_key key-fixture convention as
// gateevidence's testdata/dev-signing-key.yaml. The loader also accepts the
// package-local JSON fixture without adding a YAML dependency; it parses only
// the four scalar fields in that existing fixture. THIS FIXTURE KEY CARRIES
// NO OPERATIONAL AUTHORITY;
// see keyfixture.go's warning.
//
// # Verify as the release gate
//
// [Verify] is the deployment-admission check TOOL-018's GREEN clause
// describes: it refuses a Statement that is unsigned, signed by a key
// outside the caller's trusted set (VerifyOptions.TrustedPublicKeys, an
// "unknown builder/signer"), tampered after signing (any field change
// invalidates CanonicalDigest, so the Ed25519 signature no longer verifies),
// or whose SBOM reference digest does not match the SBOM file actually on
// disk (VerifyOptions.SBOMDigest) - only an approved
// source/build/toolchain/SBOM provenance chain reaches Verify's caller.
//
// # No git, no committed binaries
//
// Like tools/policy/sbom (see its doc.go), this package never shells out to
// git: this repository is not currently tagged and its working tree may be
// dirty, so there is no commit hash `git rev-parse` could report that is
// both reliable and reproducible from a clean checkout alone. SourceRef's
// Commit and Ref fields are instead read from caller-specified environment
// variables (CommitEnvVars/RefEnvVar in Options, defaulting to the common CI
// names GIT_COMMIT/GITHUB_SHA and GIT_REF/GITHUB_REF) and fall back to the
// literal "unknown" when none are set - exactly the same documented,
// deliberate deviation sbom.DefaultRootVersion makes for the BOM's root
// component version.
//
// cmd/provgen never writes a built binary anywhere durable: Generate builds
// into a fresh os.MkdirTemp directory, hashes the result, and removes the
// directory before returning. Only the resulting JSON Statement - names and
// digests, never bytes - is written to disk, by default at
// definitions/supply-chain/provenance.json.
package provenance
