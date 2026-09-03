package popscale

import "github.com/monstercameron/hcm-next/internal/engines/population"

// Session pins one frozen snapshot for one caller and one page size. It
// validates the membership exactly once at construction; every Page call after
// that copies only the slice it serves, so walking a large population in
// pages performs one O(n) validation and O(n) bytes of paging in total rather
// than O(n) work per page.
type Session struct {
	pag   *Pagination
	count CountResponse
}

// NewSession validates the snapshot, applies the caller's count view and,
// when the caller may see membership, fixes the membership order once.
func NewSession(snap population.Snapshot, caller Caller, pageSize int) (*Session, error) {
	if snap.Digest == "" || snap.DefinitionID == "" {
		return nil, rejected("snapshot", "not_frozen")
	}
	count, err := DiscloseCount(caller.CountAuthorized, caller.CountView, snap.Count)
	if err != nil {
		return nil, err
	}
	sess := &Session{count: count}
	if caller.MembershipDisclosed && !snap.MembershipProtected {
		if snap.Count.IsValue() {
			// A disclosed snapshot whose recorded count is a concrete value
			// must count exactly its disclosed members; a mismatch means the
			// snapshot is internally inconsistent and paging it would serve
			// a count its own membership does not support.
			value, _ := snap.Count.Get()
			if value != len(snap.SubjectIDList()) {
				return nil, rejected("count", "inconsistent")
			}
		}
		pag, err := New(snap.SubjectIDList(), pageSize)
		if err != nil {
			return nil, err
		}
		sess.pag = pag
	}
	return sess, nil
}

// Count is the count this caller is entitled to; it is the one answer shared
// by denied, empty and unknown when nothing may be revealed.
func (s *Session) Count() CountResponse { return s.count }

// Page serves the page that follows token; an empty token starts the walk. A
// session whose caller may not see membership - or whose snapshot protects it
// - always answers with the zero page, indistinguishable from an empty
// population.
func (s *Session) Page(token string) (Page, error) {
	if s.pag == nil {
		return zeroPage(), nil
	}
	return s.pag.Next(token)
}

// zeroPage is the wire form shared by denied, protected and empty.
func zeroPage() Page { return Page{Subjects: []string{}} }
