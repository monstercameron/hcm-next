package popscale

import (
	"sort"
	"strconv"
)

// Page is one page of disclosed membership. Subjects is in the fixed sorted
// order the pagination was built for; NextToken is "" on the final page. The
// zero Page - no subjects, no token, not truncated - is the wire form for a
// denied walk, an empty population and a protected snapshot alike, so the
// three cannot be told apart from a page.
type Page struct {
	Subjects  []string
	NextToken string
	Truncated bool
}

// Pagination walks a fixed membership exactly once: every member appears on
// exactly one page, in sorted order, for any page size. The membership is
// validated at construction, because a set that is not strictly sorted and
// unique would lose or duplicate members under paging; such a set is rejected
// up front instead of served.
type Pagination struct {
	subjects []string
	pageSize int
}

// New validates the page size and the membership set and fixes its order.
func New(subjects []string, pageSize int) (*Pagination, error) {
	if pageSize < 1 {
		return nil, rejected("page_size", "not_positive")
	}
	cp := make([]string, len(subjects))
	copy(cp, subjects)
	if !sort.StringsAreSorted(cp) {
		return nil, rejected("membership", "unsorted")
	}
	for i := 1; i < len(cp); i++ {
		if cp[i] == cp[i-1] {
			return nil, rejected("membership", "duplicate")
		}
	}
	return &Pagination{subjects: cp, pageSize: pageSize}, nil
}

// Len is the total membership the pagination covers.
func (p *Pagination) Len() int { return len(p.subjects) }

// First serves the first page.
func (p *Pagination) First() (Page, error) { return p.Next("") }

// Next serves the page that follows token; an empty token starts the walk. A
// token is a decimal position into the fixed membership order, so a walk
// driven only by the tokens this pagination emits loses no member and
// duplicates none: each position is covered by exactly one emitted page.
func (p *Pagination) Next(token string) (Page, error) {
	pos := 0
	if token != "" {
		n, err := strconv.Atoi(token)
		if err != nil || n < 0 {
			return Page{}, rejected("token", "malformed")
		}
		if n > p.Len() {
			return Page{}, rejected("token", "past_end")
		}
		pos = n
	}
	end := pos + p.pageSize
	if end > p.Len() {
		end = p.Len()
	}
	subjects := make([]string, 0, end-pos)
	subjects = append(subjects, p.subjects[pos:end]...)
	page := Page{Subjects: subjects, Truncated: end < p.Len()}
	if page.Truncated {
		page.NextToken = strconv.Itoa(end)
	}
	return page, nil
}

// Walk drains the pagination and returns the union of all pages, for
// exactly-once verification.
func (p *Pagination) Walk() ([]string, error) {
	var out []string
	token := ""
	for {
		page, err := p.Next(token)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Subjects...)
		if !page.Truncated {
			return out, nil
		}
		token = page.NextToken
	}
}
