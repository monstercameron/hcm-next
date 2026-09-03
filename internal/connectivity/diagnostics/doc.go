// Package diagnostics owns read-only external credential and permission probes.
//
// Provider-specific failures are normalized into deterministic findings. The
// package never receives credential material: callers pass a connectivity
// connection (which contains only an opaque reference) and a Probe port.
package diagnostics
