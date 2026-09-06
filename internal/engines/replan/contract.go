package replan

// Version reports the replan engine package's own contract version (the
// ARCH-GO-009 engine contract symbol). It is distinct from any proposal or
// snapshot version the engine compares; it only changes when this package's
// exported contract changes incompatibly.
func Version() int { return 1 }
