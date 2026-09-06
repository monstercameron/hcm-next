// Package workerids owns organization-scoped worker-number policy and the
// allocation port. A worker number is a human-facing business identifier; it
// is deliberately separate from the immutable UUID used as entity identity.
package workerids

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrUnavailable     = errors.New("workerids: store unavailable")
	ErrVersionConflict = errors.New("workerids: stale version")
	ErrInvalid         = errors.New("workerids: invalid policy")
	ErrExhausted       = errors.New("workerids: sequence exhausted")
)

const (
	YearNone  = "NONE"
	YearYY    = "YY"
	YearYYYY  = "YYYY"
	CheckNone = "NONE"
	CheckLuhn = "LUHN_MOD10"
)

// Policy is one organization's replaceable worker-number specification.
// NextSequence is allocation state and is never moved backwards by Save.
type Policy struct {
	Version             int64
	OrganizationScopeID string
	Prefix              string
	Suffix              string
	Separator           string
	SequenceDigits      int
	StartAt             int64
	NextSequence        int64
	IncrementBy         int64
	ZeroPad             bool
	YearFormat          string
	IncludeUnitCode     bool
	CheckDigit          string
	ExcludedRanges      string
	IssuedCount         int64
}

type FormatContext struct {
	At       time.Time
	UnitCode string
}

type Store interface {
	Load(context.Context, values.TenantId, string) (Policy, error)
	Save(context.Context, values.TenantId, string, string, Policy) (Policy, error)
	Reserve(context.Context, values.TenantId, string, string, FormatContext) (string, error)
}

func DefaultPolicy() Policy {
	return Policy{Prefix: "HC", Separator: "-", SequenceDigits: 6, StartAt: 1000, NextSequence: 1000, IncrementBy: 1, ZeroPad: true, YearFormat: YearNone, CheckDigit: CheckNone}
}

func Normalize(p Policy) Policy {
	d := DefaultPolicy()
	p.OrganizationScopeID = strings.TrimSpace(p.OrganizationScopeID)
	p.Prefix = cleanAffix(p.Prefix)
	p.Suffix = cleanAffix(p.Suffix)
	if p.Separator != "" && p.Separator != "-" && p.Separator != "/" && p.Separator != "." {
		p.Separator = d.Separator
	}
	if p.SequenceDigits < 1 || p.SequenceDigits > 12 {
		p.SequenceDigits = d.SequenceDigits
	}
	if p.StartAt < 0 {
		p.StartAt = d.StartAt
	}
	if p.NextSequence < p.StartAt {
		p.NextSequence = p.StartAt
	}
	if p.IncrementBy < 1 || p.IncrementBy > 1_000_000 {
		p.IncrementBy = d.IncrementBy
	}
	switch p.YearFormat {
	case YearNone, YearYY, YearYYYY:
	default:
		p.YearFormat = d.YearFormat
	}
	switch p.CheckDigit {
	case CheckNone, CheckLuhn:
	default:
		p.CheckDigit = d.CheckDigit
	}
	p.ExcludedRanges = normalizeRanges(p.ExcludedRanges)
	return p
}

func Validate(p Policy) error {
	p = Normalize(p)
	if p.Prefix == "" && p.Suffix == "" && p.YearFormat == YearNone && !p.IncludeUnitCode {
		// A bare sequence is valid, but call this out explicitly by allowing it.
	}
	max := maxSequence(p.SequenceDigits)
	if p.StartAt > max || p.NextSequence > max {
		return fmt.Errorf("%w: sequence exceeds %d digits", ErrInvalid, p.SequenceDigits)
	}
	if _, err := parseRanges(p.ExcludedRanges, max); err != nil {
		return err
	}
	return nil
}

// Format renders a candidate without reserving it. It is safe for previews;
// only Store.Reserve may issue the returned text to a worker.
func Format(p Policy, sequence int64, ctx FormatContext) (string, error) {
	p = Normalize(p)
	if err := Validate(p); err != nil {
		return "", err
	}
	if sequence < 0 || sequence > maxSequence(p.SequenceDigits) {
		return "", ErrExhausted
	}
	number := strconv.FormatInt(sequence, 10)
	if p.ZeroPad {
		number = fmt.Sprintf("%0*d", p.SequenceDigits, sequence)
	}
	parts := make([]string, 0, 5)
	if p.Prefix != "" {
		parts = append(parts, p.Prefix)
	}
	if p.YearFormat != YearNone {
		year := ctx.At.UTC().Year()
		if year == 1 {
			year = time.Now().UTC().Year()
		}
		if p.YearFormat == YearYY {
			parts = append(parts, fmt.Sprintf("%02d", year%100))
		} else {
			parts = append(parts, fmt.Sprintf("%04d", year))
		}
	}
	if p.IncludeUnitCode {
		unit := cleanUnit(ctx.UnitCode)
		if unit == "" {
			unit = "ORG"
		}
		parts = append(parts, unit)
	}
	parts = append(parts, number)
	if p.CheckDigit == CheckLuhn {
		parts = append(parts, strconv.Itoa(luhnDigit(number)))
	}
	if p.Suffix != "" {
		parts = append(parts, p.Suffix)
	}
	return strings.Join(parts, p.Separator), nil
}

func IsExcluded(p Policy, sequence int64) bool {
	ranges, _ := parseRanges(Normalize(p).ExcludedRanges, maxSequence(Normalize(p).SequenceDigits))
	for _, r := range ranges {
		if sequence >= r[0] && sequence <= r[1] {
			return true
		}
	}
	return false
}

func maxSequence(digits int) int64 {
	var n int64 = 1
	for i := 0; i < digits; i++ {
		n *= 10
	}
	return n - 1
}

func cleanAffix(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
		if b.Len() == 12 {
			break
		}
	}
	return b.String()
}

func cleanUnit(value string) string {
	value = cleanAffix(value)
	if len(value) > 6 {
		value = value[:6]
	}
	return value
}

func normalizeRanges(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) })
	return strings.Join(parts, ",")
}

func parseRanges(value string, max int64) ([][2]int64, error) {
	if value == "" {
		return nil, nil
	}
	var out [][2]int64
	for _, token := range strings.Split(value, ",") {
		bounds := strings.Split(token, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("%w: invalid excluded range %q", ErrInvalid, token)
		}
		lo, err := strconv.ParseInt(bounds[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid excluded range %q", ErrInvalid, token)
		}
		hi := lo
		if len(bounds) == 2 {
			hi, err = strconv.ParseInt(bounds[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid excluded range %q", ErrInvalid, token)
			}
		}
		if lo < 0 || hi < lo || hi > max {
			return nil, fmt.Errorf("%w: invalid excluded range %q", ErrInvalid, token)
		}
		out = append(out, [2]int64{lo, hi})
	}
	return out, nil
}

func luhnDigit(number string) int {
	sum, double := 0, true
	for i := len(number) - 1; i >= 0; i-- {
		d := int(number[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return (10 - sum%10) % 10
}
