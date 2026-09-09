package popscale

import "github.com/monstercameron/human-capital-management-suite/internal/engines/population"

// Caller is the already-decided authorization of the observer requesting a
// page: whether the restriction decision (POP-004) discloses raw membership,
// and which count view, if any, the observer is authorized for. popscale
// never recomputes these decisions; it only applies them, and every value it
// was not given is treated as not disclosed.
type Caller struct {
	MembershipDisclosed bool
	CountAuthorized     bool
	CountView           population.CountDisclosure
}

// Request is one page request against a frozen snapshot.
type Request struct {
	Caller   Caller
	PageSize int
	Token    string
	// Sink, when non-nil, must still be empty when the call returns: serving
	// a page persists nothing authoritative, and neither does a rejection.
	Sink *Sink
}

// Response is one served page together with the count the caller received.
// When the caller may not see membership - or the snapshot protects it, or
// the population is empty - Page is the zero Page, indistinguishable across
// all three cases.
type Response struct {
	Page  Page
	Count CountResponse
}

// Resolve is the stateless single-page entry point: it pins a one-shot
// session and serves one page. A handler that serves many pages for the same
// snapshot, caller and page size should pin one Session instead and call its
// Page method, paying the one-time O(n) membership validation once rather
// than once per page.
func Resolve(snap population.Snapshot, req Request) (Response, error) {
	sess, err := NewSession(snap, req.Caller, req.PageSize)
	if err != nil {
		return Response{}, err
	}
	page, err := sess.Page(req.Token)
	if err != nil {
		return Response{}, err
	}
	return Response{Page: page, Count: sess.Count()}, nil
}
