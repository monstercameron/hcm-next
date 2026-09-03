package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DisclosurePolicy is the versioned, deny-by-default policy for an analytics
// result. Counts below MinCell are never returned. Round and Noise are
// mutually exclusive deterministic transformations (both may be zero).
type DisclosurePolicy struct {
	ID                  string
	Version             string
	MinCell             int
	Complementary       bool
	Round               int
	Noise               int
	Budget              int
	RepeatedQueryBudget int
	SensitiveDimensions []string
	RequireReview       bool
}

// AnalyticsQuery identifies the exact analytical request and its boundary.
type AnalyticsQuery struct {
	TenantID   string
	Principal  string
	Purpose    string
	Digest     string
	Dimensions []string
}

// AnalyticsCell is an aggregate cell. Value is retained only for authorized
// cells; Sensitive marks a dimension which requires review.
type AnalyticsCell struct {
	Key       string
	Dimension string
	Value     int
	Sensitive bool
}

type DisclosureStatus string

const (
	DisclosureAllowed        DisclosureStatus = "ALLOWED"
	DisclosureSuppressed     DisclosureStatus = "SUPPRESSED"
	DisclosureReviewRequired DisclosureStatus = "REVIEW_REQUIRED"
	DisclosureDenied         DisclosureStatus = "DENIED"
)

// DisclosureEvidence is an append-only explanation bound to the query and
// policy digests. It intentionally contains no raw identifiers or rows.
type DisclosureEvidence struct {
	QueryDigest  string
	PolicyDigest string
	Status       DisclosureStatus
	Suppressed   []string
	Transform    string
	BudgetUsed   int
	Reason       string
}

type DisclosureResult struct {
	Cells    []AnalyticsCell
	Evidence DisclosureEvidence
}

var (
	ErrInvalidDisclosurePolicy = errors.New("privacy: invalid disclosure policy")
	ErrDisclosureDenied        = errors.New("privacy: disclosure denied")
	ErrBudgetExceeded          = errors.New("privacy: repeated-query budget exceeded")
)

func (p DisclosurePolicy) Validate() error {
	if p.ID == "" || p.Version == "" || p.MinCell < 1 || p.Budget < 1 || p.RepeatedQueryBudget < 1 {
		return ErrInvalidDisclosurePolicy
	}
	if p.Round < 0 || p.Noise < 0 || p.Noise > 127 || (p.Round > 0 && p.Noise > 0) {
		return ErrInvalidDisclosurePolicy
	}
	return nil
}

func digestString(parts ...string) string {
	h := sha256.New()
	for _, s := range parts {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte(":"))
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (p DisclosurePolicy) Digest() string {
	return digestString(p.ID, p.Version, strconv.Itoa(p.MinCell), strconv.FormatBool(p.Complementary), strconv.Itoa(p.Round), strconv.Itoa(p.Noise), strconv.Itoa(p.Budget), strconv.Itoa(p.RepeatedQueryBudget), strings.Join(sorted(p.SensitiveDimensions), ","), strconv.FormatBool(p.RequireReview))
}

func QueryDigest(q AnalyticsQuery) string {
	if q.Digest != "" {
		return q.Digest
	}
	return digestString(q.TenantID, q.Principal, q.Purpose, strings.Join(sorted(q.Dimensions), "\x00"))
}

func sorted(in []string) []string { out := append([]string(nil), in...); sort.Strings(out); return out }

// Budget is concurrency-safe and keyed by tenant, principal, and exact query
// digest. A failed request does not consume budget.
type DisclosureBudget struct {
	mu             sync.Mutex
	used, repeated map[string]int
}

func NewDisclosureBudget() *DisclosureBudget {
	return &DisclosureBudget{used: make(map[string]int), repeated: make(map[string]int)}
}
func (b *DisclosureBudget) Consume(q AnalyticsQuery, p DisclosurePolicy) (int, error) {
	if b == nil {
		return 0, ErrBudgetExceeded
	}
	totalKey := q.TenantID + "\x00" + q.Principal + "\x00" + q.Purpose
	repeatKey := totalKey + "\x00" + QueryDigest(q)
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.used[totalKey]
	r := b.repeated[repeatKey]
	if u >= p.Budget || r >= p.RepeatedQueryBudget {
		return u, ErrBudgetExceeded
	}
	u++
	b.used[totalKey] = u
	b.repeated[repeatKey] = r + 1
	return u, nil
}

// Apply performs suppression before any rounding/noise and returns evidence
// tied to the canonical query digest. It never returns identifying rows.
func Apply(p DisclosurePolicy, q AnalyticsQuery, cells []AnalyticsCell, budget *DisclosureBudget) (DisclosureResult, error) {
	if err := p.Validate(); err != nil {
		return DisclosureResult{}, err
	}
	if q.TenantID == "" || q.Principal == "" || q.Purpose == "" {
		return DisclosureResult{}, ErrDisclosureDenied
	}
	used, err := budget.Consume(q, p)
	if err != nil {
		return DisclosureResult{Evidence: DisclosureEvidence{QueryDigest: QueryDigest(q), PolicyDigest: p.Digest(), Status: DisclosureDenied, Reason: err.Error(), BudgetUsed: used}}, err
	}
	out := append([]AnalyticsCell(nil), cells...)
	suppressed := make([]string, 0)
	for i := range out {
		if out[i].Value < p.MinCell || out[i].Sensitive {
			suppressed = append(suppressed, out[i].Key)
			out[i].Value = 0
		}
	}
	if p.Complementary {
		complementarySuppress(out, &suppressed, p.MinCell)
	}
	for i := range out {
		if out[i].Value == 0 {
			continue
		}
		if p.Round > 1 {
			out[i].Value = (out[i].Value / p.Round) * p.Round
		}
		if p.Noise > 0 {
			out[i].Value += deterministicNoise(QueryDigest(q), out[i].Key, p.Noise)
		}
	}
	status := DisclosureAllowed
	if len(suppressed) > 0 {
		status = DisclosureSuppressed
	}
	if p.RequireReview && len(suppressed) > 0 {
		status = DisclosureReviewRequired
	}
	sort.Strings(suppressed)
	return DisclosureResult{Cells: out, Evidence: DisclosureEvidence{QueryDigest: QueryDigest(q), PolicyDigest: p.Digest(), Status: status, Suppressed: suppressed, Transform: transform(p), BudgetUsed: used}}, nil
}

func complementarySuppress(c []AnalyticsCell, suppressed *[]string, min int) {
	byDim := map[string][]int{}
	hasSuppressed := map[string]bool{}
	for i := range c {
		if c[i].Value >= min {
			byDim[c[i].Dimension] = append(byDim[c[i].Dimension], i)
		} else {
			hasSuppressed[c[i].Dimension] = true
		}
	}
	for _, ix := range byDim {
		// Once one category is suppressed, suppress the smallest remaining
		// category too; publishing the complement would reveal it exactly.
		if len(ix) == 1 || hasSuppressed[c[ix[0]].Dimension] {
			sort.Slice(ix, func(a, b int) bool { return c[ix[a]].Value < c[ix[b]].Value })
			i := ix[0]
			c[i].Value = 0
			*suppressed = append(*suppressed, c[i].Key)
		}
	}
}
func deterministicNoise(q, key string, n int) int {
	h := sha256.Sum256([]byte(q + "\x00" + key))
	return int(h[0]%(byte(2*n+1))) - n
}
func transform(p DisclosurePolicy) string {
	if p.Noise > 0 {
		return fmt.Sprintf("NOISE:%d", p.Noise)
	}
	if p.Round > 1 {
		return fmt.Sprintf("ROUND:%d", p.Round)
	}
	return "NONE"
}
