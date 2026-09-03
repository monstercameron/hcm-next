// Package federation owns the tenant-scoped federation issuer registry.
//
// The registry is deliberately independent from protocol adapters: it records
// the signed, versioned profile that an adapter must use and provides an
// atomic validation seam for an incoming assertion's metadata.
package federation
