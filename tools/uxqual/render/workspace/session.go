package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SessionElementID is the id of the JSON island a governed page's shell
// writes its acting principal's display facts into. It is spelled the same
// as internal/humanwork/workspace.JourneyConfigElementID ("journey-config"),
// the one session island this repository actually serves today (the
// Promotion journey shell), because that island already carries exactly
// the fields [ReadSession] reads, alongside transport configuration this
// package has no field for and never reads (see [Session]'s doc comment).
const SessionElementID = "journey-config"

// Session is the subset of a governed page's session island this package
// reads: who is acting, under what roles and for what purpose.
//
// It is a struct, not a map, so the field names are a compile-time
// contract, and it deliberately carries no field for a bearer, tunnel
// address, or any other credential or transport detail: this package builds
// an "authority context" strip (the frontend plan's page-anatomy region 3 --
// planning/specs/production-frontend-and-page-composition.md), a display
// fact, never a place a credential can pass through even by accident.
// encoding/json silently ignores unknown fields on Unmarshal, so a real
// island's "bearer" and "tunnel_url" are never captured by this type at
// all, not merely left unrendered.
//
// Its JSON tags mirror internal/humanwork/workspace.JourneyConfig's own
// Tenant/Subject/Roles/Purpose fields by name, as a separate declaration
// rather than an import: this package may not import internal/, the server
// may not import a tools/ package, and (independently of either rule) this
// package is deliberately kept unaware of any one page client's own
// transport-specific config shape, so a later non-journey Intent Workspace
// page can supply the same four display facts through a differently-shaped
// island without this package changing at all. This is the same
// two-separate-declarations convention tools/uxqual/journeyclient.Config
// already uses against the same server struct, for the overlapping half of
// its reason.
type Session struct {
	Tenant  string   `json:"tenant"`
	Subject string   `json:"subject"`
	Roles   []string `json:"roles"`
	Purpose string   `json:"purpose"`
}

// ErrSessionMalformed means the island was not a JSON object at all.
var ErrSessionMalformed = errors.New("workspace: the session island is not a JSON object")

// ReadSession parses a session island's raw bytes into a [Session].
//
// Unlike tools/uxqual/journeyclient.ParseConfig, it never refuses for a
// missing bearer or tunnel address: this reader does not look at those
// fields, so their absence is not its business. A session with no Subject,
// Tenant, Purpose, or Roles at all is admitted -- the rendered strip simply
// has nothing to say (see [SessionStrip]) -- rather than refused, because
// showing the truth about the current session (including "none of this is
// known") is this package's job, not gating on it; that gate belongs to
// authentication, upstream of this package entirely.
func ReadSession(data []byte) (Session, error) {
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, fmt.Errorf("%w: %v", ErrSessionMalformed, err)
	}
	s.Tenant = strings.TrimSpace(s.Tenant)
	s.Subject = strings.TrimSpace(s.Subject)
	s.Purpose = strings.TrimSpace(s.Purpose)
	return s, nil
}

// IsZero reports whether s carries no display fact at all, which
// [SessionStrip] uses to decide whether to render anything.
func (s Session) IsZero() bool {
	return s.Tenant == "" && s.Subject == "" && s.Purpose == "" && len(s.Roles) == 0
}
