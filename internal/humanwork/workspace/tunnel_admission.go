package workspace

import (
	"net/http"
	"strings"

	"github.com/monstercameron/hcm-next/internal/transport"
)

// This file exports the one credential-reading rule the workspace has always
// applied to its own requests, so that a second surface serving the same
// browser session cannot end up applying a slightly different one.
//
// The second surface is the gRPC-over-WebSocket tunnel
// (internal/transport/cell.TunnelPath). Its upgrade request is a browser
// request from the very page this package served, carrying the very cookie
// this package's dev sign-in set, and it has to be admitted by the same
// [transport.PreAdmit] under the same substitution rule. Re-deriving that
// rule there would be a second implementation of the one place a cookie is
// ever allowed to stand in for the Authorization header, which is precisely
// the thing that must not be duplicated.

// BearerFromRequest returns the Authorization header value r should be
// admitted with, or "" when it carries no credential at all.
//
// It is the request's own Authorization header whenever the request has one.
// Only when it does not, and only when devBrowserLogin is true, does the
// session cookie the dev sign-in flow set stand in for it, rendered as the
// header value it substitutes for ("Bearer <token>"). With devBrowserLogin
// false the cookie is not read at all: a deployment that never turned the
// operator flag on has no cookie-based way in, and this function is not a
// second one.
//
// The returned value is a credential. A caller that puts it anywhere a
// person or a log can see it is responsible for that decision; the one place
// in this package that does (the journey page shell's config island) says
// why in its own comment.
func BearerFromRequest(r *http.Request, devBrowserLogin bool) string {
	if r == nil {
		return ""
	}
	if values := r.Header.Values("Authorization"); len(values) > 0 {
		if header := strings.TrimSpace(values[0]); header != "" {
			return header
		}
	}
	if !devBrowserLogin {
		return ""
	}
	cookie, err := r.Cookie(loginSessionCookie)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return "Bearer " + cookie.Value
}

// AdmissionMetadata returns the transport metadata admission reads for r.
//
// It is the request's own headers, unmodified, unless dev browser sign-in is
// enabled and the request carries no Authorization header of its own - only
// then does the session cookie stand in for it, through
// [BearerFromRequest]. Admission itself is unchanged either way: what this
// function decides is which credential is presented, never what is done with
// it.
func AdmissionMetadata(r *http.Request, devBrowserLogin bool) transport.Metadata {
	if r == nil {
		return transport.MapMetadata(nil)
	}
	md := transport.MapMetadata(r.Header)
	if !devBrowserLogin || len(md.Get(transport.AuthorizationMetadataKey)) > 0 {
		return md
	}
	bearer := BearerFromRequest(r, devBrowserLogin)
	if bearer == "" {
		return md
	}
	cloned := r.Header.Clone()
	cloned.Set("Authorization", bearer)
	return transport.MapMetadata(cloned)
}
