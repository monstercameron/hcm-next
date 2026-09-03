// Package adapter defines the provider-neutral connector adapter SPI.
//
// The SPI is intentionally expressed in terms of typed schemas and canonical
// operation envelopes. Connector implementations may use a vendor SDK behind
// the boundary, but vendor values and transport handles cannot cross it.
package adapter
