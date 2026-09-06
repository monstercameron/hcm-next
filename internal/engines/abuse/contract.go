package abuse

// Version reports the abuse engine package's own contract version (the
// ARCH-GO-009 engine contract symbol). It is distinct from the business
// version carried by a SignalDefinition, DetectorDefinition, or
// DetectorVersion: those are governed data the engine validates and
// publishes; this is the shape of the engine contract itself, and only
// changes when this package's exported contract changes incompatibly.
func Version() int { return 1 }
