package popscale

import "fmt"

// Explain reports a rejection in operator language (the ARCH-GO-009
// Explain-shaped symbol for this engine): which field was refused, the
// state it was in, and the resolver version that made the call. It carries
// the same facts as Error without the POP_010 token prefix log matchers
// depend on.
func (r *Rejection) Explain() string {
	return fmt.Sprintf("population scale resolution refused field %q in state %q by resolver %s", r.Field, r.State, r.Version)
}
