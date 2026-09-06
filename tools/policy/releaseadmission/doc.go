// Package releaseadmission owns the release admission policy for signed
// provenance statements (TOOL-023).
//
// Admission is deliberately a pure decision over an in-memory provenance
// statement and a pinned policy. The package does not invoke Cosign, contact
// Sigstore, read a transparency log, or make a deployment side effect. The
// existing provenance package supplies the Ed25519 and SBOM-linkage mechanics;
// this package supplies the release-owned trust policy and decision record.
//
// Cosign/Sigstore remains DEFERRED: no keyless flow is used and no Sigstore
// module is added to go.mod. Adoption is triggered only when a production
// release requires keyless identity or transparency-log verification and a
// separately qualified, offline-retained verification bundle is available.
package releaseadmission
