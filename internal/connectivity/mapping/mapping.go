package mapping

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidIR     = errors.New("mapping: invalid IR")
	ErrMissingSource = errors.New("mapping: source field is missing")
	ErrUnsupportedOp = errors.New("mapping: unsupported operation")
	ErrTransform     = errors.New("mapping: transform failed")
)

type Op string

const (
	OpIdentity Op = "IDENTITY"
	OpTrim     Op = "TRIM"
	OpUpper    Op = "UPPER"
	OpLower    Op = "LOWER"
	OpConstant Op = "CONSTANT"
	OpLookup   Op = "LOOKUP"
	OpDate     Op = "DATE"
	OpMoney    Op = "MONEY"
	OpCompose  Op = "COMPOSE"
)

type NullPolicy string

const (
	NullError  NullPolicy = "ERROR"
	NullOmit   NullPolicy = "OMIT"
	NullDelete NullPolicy = "DELETE"
)

type Rule struct {
	Source   string
	Target   string
	Op       Op
	Argument string
	Lookup   map[string]string
	Null     NullPolicy
}

type IR struct {
	Version string
	Rules   []Rule
}
type Diagnostic struct{ Target, Code, Detail string }
type Field struct {
	Target, Value string
	Deleted       bool
}
type Result struct {
	Fields        []Field
	Diagnostics   []Diagnostic
	PayloadDigest string
}

func (ir IR) Validate() error {
	if ir.Version == "" || len(ir.Rules) == 0 {
		return ErrInvalidIR
	}
	seen := map[string]bool{}
	for _, r := range ir.Rules {
		if r.Target == "" || seen[r.Target] {
			return fmt.Errorf("%w: duplicate or empty target", ErrInvalidIR)
		}
		seen[r.Target] = true
		if r.Null == "" {
			r.Null = NullError
		}
		switch r.Op {
		case OpIdentity, OpTrim, OpUpper, OpLower:
			if r.Argument != "" || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: extraneous arguments", ErrInvalidIR)
			}
		case OpConstant:
			if r.Argument == "" || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: constant", ErrInvalidIR)
			}
		case OpLookup:
			if len(r.Lookup) == 0 || r.Argument != "" {
				return fmt.Errorf("%w: lookup", ErrInvalidIR)
			}
		case OpDate:
			if !allowedLayouts[r.Argument] || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: date layout", ErrInvalidIR)
			}
		case OpMoney:
			if !currencyPattern.MatchString(r.Argument) || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: money currency", ErrInvalidIR)
			}
		case OpCompose:
			if r.Argument == "" || len(r.Lookup) > 0 {
				return fmt.Errorf("%w: compose", ErrInvalidIR)
			}
		default:
			return fmt.Errorf("%w: %q", ErrUnsupportedOp, r.Op)
		}
		if r.Null != NullError && r.Null != NullOmit && r.Null != NullDelete {
			return fmt.Errorf("%w: null policy", ErrInvalidIR)
		}
	}
	return nil
}

var allowedLayouts = map[string]bool{"2006-01-02": true, time.RFC3339: true, "2006/01/02": true, "01/02/2006": true, "20060102": true}
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var moneyPattern = regexp.MustCompile(`^-?[0-9]+(\.[0-9]{1,2})?$`)

// Execute evaluates a validated IR using only the supplied immutable input. It performs no I/O, clock, locale, randomness, or code evaluation.
func Execute(ir IR, input map[string]string) (Result, error) {
	if err := ir.Validate(); err != nil {
		return Result{}, err
	}
	fields := make([]Field, 0, len(ir.Rules))
	diags := []Diagnostic{}
	for _, r := range ir.Rules {
		raw, ok := input[r.Source]
		if r.Op == OpConstant {
			ok = true
		}
		if !ok {
			diags = append(diags, Diagnostic{r.Target, "source.missing", r.Source})
			if r.Null == NullError {
				return Result{}, fmt.Errorf("%w: %s", ErrMissingSource, r.Source)
			}
			if r.Null == NullDelete {
				fields = append(fields, Field{Target: r.Target, Deleted: true})
			}
			continue
		}
		v, err := eval(r, raw)
		if err != nil {
			diags = append(diags, Diagnostic{r.Target, "transform.failed", err.Error()})
			if r.Null == NullError {
				return Result{}, fmt.Errorf("%w: %s: %v", ErrTransform, r.Target, err)
			}
			if r.Null == NullDelete {
				fields = append(fields, Field{Target: r.Target, Deleted: true})
			}
			continue
		}
		if v == "" {
			if r.Null == NullError {
				diags = append(diags, Diagnostic{r.Target, "value.empty", "empty output"})
				return Result{}, fmt.Errorf("%w: %s: empty output", ErrTransform, r.Target)
			}
			if r.Null == NullDelete {
				fields = append(fields, Field{Target: r.Target, Deleted: true})
			}
			if r.Null == NullOmit {
				continue
			}
		}
		fields = append(fields, Field{Target: r.Target, Value: v})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Target < fields[j].Target })
	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Target != diags[j].Target {
			return diags[i].Target < diags[j].Target
		}
		return diags[i].Code < diags[j].Code
	})
	return Result{Fields: fields, Diagnostics: diags, PayloadDigest: digest(ir, fields)}, nil
}
func eval(r Rule, s string) (string, error) {
	switch r.Op {
	case OpIdentity:
		return s, nil
	case OpTrim:
		return strings.TrimSpace(s), nil
	case OpUpper:
		return strings.ToUpper(strings.TrimSpace(s)), nil
	case OpLower:
		return strings.ToLower(strings.TrimSpace(s)), nil
	case OpConstant:
		return r.Argument, nil
	case OpLookup:
		v, ok := r.Lookup[strings.TrimSpace(s)]
		if !ok {
			return "", errors.New("lookup unresolved")
		}
		return v, nil
	case OpDate:
		t, e := time.Parse(r.Argument, strings.TrimSpace(s))
		if e != nil {
			return "", e
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case OpMoney:
		return money(strings.TrimSpace(s), r.Argument)
	case OpCompose:
		parts := strings.Split(r.Argument, "|")
		out := make([]string, len(parts))
		for i, p := range parts {
			out[i] = strings.ReplaceAll(p, "${value}", s)
		}
		return strings.Join(out, ""), nil
	}
	return "", ErrUnsupportedOp
}
func money(s, c string) (string, error) {
	s = strings.ReplaceAll(s, ",", "")
	if strings.HasPrefix(s, c) {
		s = strings.TrimSpace(strings.TrimPrefix(s, c))
	}
	if strings.ContainsAny(s, "$€£") || s == "" {
		return "", errors.New("invalid money")
	}
	if !moneyPattern.MatchString(s) {
		return "", errors.New("invalid money")
	}
	return s, nil
}
func digest(ir IR, fs []Field) string {
	h := sha256.New()
	put := func(s string) { fmt.Fprintf(h, "%d:", len(s)); h.Write([]byte(s)) }
	put("hcmnext.connectivity.mapping")
	put(ir.Version)
	for _, f := range fs {
		put(f.Target)
		put(f.Value)
		if f.Deleted {
			put("DELETE")
		} else {
			put("VALUE")
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
