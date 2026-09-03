// Package reliability owns the versioned pilot SLI/SLO contract.
//
// This package is deliberately a pure planning and assurance boundary:
// observations are supplied by callers and validation/evaluation never emits
// telemetry, writes state, or changes transaction-path availability semantics.
package reliability
