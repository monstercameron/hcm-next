// Package vulnimpact resolves a vulnerability record through in-memory
// CycloneDX SBOMs and deployment maps (SUPPLY-002).
//
// The package is a bounded, kernel-pure query: it does not fetch vulnerability
// data, inspect a module cache, query a database, or infer a deployment. The
// caller supplies the vulnerability identity, SBOM inventory, and explicit
// tenant-to-artifact map. Successful reports retain the SBOM watermark,
// typed severity, affected artifacts, affected tenants, and canonical
// evidence digests.
package vulnimpact
