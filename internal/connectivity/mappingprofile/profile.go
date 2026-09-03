// Package mappingprofile compiles versioned, declarative connector mappings.
//
// The package deliberately contains no hooks for code evaluation or I/O. A
// compiled profile is an immutable description of a bounded set of field
// operations and can be replayed with the same bytes and result.
package mappingprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidProfile = errors.New("mappingprofile: invalid profile")
	ErrUnsupported    = errors.New("mappingprofile: unsupported transform")
	ErrMissing        = errors.New("mappingprofile: source missing")
	ErrTransform      = errors.New("mappingprofile: transform failed")
)

var allowedDateLayouts = map[string]bool{"2006-01-02": true, time.RFC3339: true, "2006/01/02": true, "01/02/2006": true, "20060102": true}
var currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)
var moneyRE = regexp.MustCompile(`^-?[0-9]+(\.[0-9]{1,2})?$`)

// Profile is the source representation accepted by Compile.
type Profile struct {
	MappingID, Version                    string
	SourceSchemaRef, DestinationSchemaRef string
	Rules                                 []Rule
	Defaults                              []Default
	Lookups                               map[string]map[string]string
}

// MappingProfile is retained as a descriptive alias for callers using the
// terminology in the integration contract.
type MappingProfile = Profile

type Rule struct {
	Source, Target string
	Op, Transform  string
	Type, Argument string
	LookupRef      string
	Lookup         map[string]string
	Default        string
	Condition      string
	Null           NullPolicy
	DeleteIfEmpty  bool
}
type Default struct{ Target, Value string }

type NullPolicy string

const (
	NullError  NullPolicy = "ERROR"
	NullOmit   NullPolicy = "OMIT"
	NullDelete NullPolicy = "DELETE"
)

type Field struct {
	Target, Value string
	Deleted       bool
}
type Result struct {
	Fields      []Field
	Diagnostics []Diagnostic
	Digest      string
}
type Diagnostic struct{ Target, Code, Detail string }

type Compiled struct {
	profile Profile
	digest  string
}

func (c Compiled) Digest() string   { return c.digest }
func (c Compiled) Profile() Profile { return c.profile }
func Compile(p Profile) (Compiled, error) {
	if p.MappingID == "" || p.Version == "" || len(p.Rules) == 0 {
		return Compiled{}, ErrInvalidProfile
	}
	seen := map[string]bool{}
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.Target == "" || seen[r.Target] {
			return Compiled{}, fmt.Errorf("%w: duplicate or empty target", ErrInvalidProfile)
		}
		seen[r.Target] = true
		if r.Op == "" {
			r.Op = r.Transform
		}
		if r.Op == "" {
			r.Op = "IDENTITY"
		}
		if r.Null == "" {
			r.Null = NullError
		}
		if r.Null != NullError && r.Null != NullOmit && r.Null != NullDelete {
			return Compiled{}, fmt.Errorf("%w: null policy", ErrInvalidProfile)
		}
		switch strings.ToUpper(r.Op) {
		case "IDENTITY", "RENAME", "TRIM", "UPPER", "LOWER":
			if strings.ToUpper(r.Op) != "RENAME" && r.Argument != "" {
				return Compiled{}, fmt.Errorf("%w: argument", ErrInvalidProfile)
			}
		case "CONSTANT", "DEFAULT", "COMPOSE", "DATE", "MONEY", "TYPE", "ENUM", "LOOKUP", "REFERENCE":
			if strings.ToUpper(r.Op) == "DATE" && !allowedDateLayouts[r.Argument] {
				return Compiled{}, fmt.Errorf("%w: date layout", ErrInvalidProfile)
			}
			if strings.ToUpper(r.Op) == "MONEY" && !currencyRE.MatchString(r.Argument) {
				return Compiled{}, fmt.Errorf("%w: currency", ErrInvalidProfile)
			}
			if strings.ToUpper(r.Op) == "LOOKUP" || strings.ToUpper(r.Op) == "ENUM" || strings.ToUpper(r.Op) == "REFERENCE" {
				if len(r.Lookup) == 0 && r.LookupRef == "" {
					return Compiled{}, fmt.Errorf("%w: unresolved lookup", ErrInvalidProfile)
				}
			}
		case "CONDITION", "CONDITIONAL":
			if r.Condition == "" {
				return Compiled{}, fmt.Errorf("%w: condition", ErrInvalidProfile)
			}
		default:
			return Compiled{}, fmt.Errorf("%w: %s", ErrUnsupported, r.Op)
		}
	}
	for _, d := range p.Defaults {
		if d.Target == "" || !seen[d.Target] {
			return Compiled{}, fmt.Errorf("%w: default target", ErrInvalidProfile)
		}
	}
	canon := canonical(p)
	h := sha256.Sum256(canon)
	return Compiled{profile: p, digest: "sha256:" + hex.EncodeToString(h[:])}, nil
}

func (c Compiled) Execute(input map[string]string) (Result, error) {
	fields := make([]Field, 0, len(c.profile.Rules))
	diags := []Diagnostic{}
	for _, r := range c.profile.Rules {
		op := strings.ToUpper(r.Op)
		raw, ok := input[r.Source]
		if op == "CONSTANT" {
			raw, ok = r.Argument, true
		}
		if !ok && r.Default != "" {
			raw, ok = r.Default, true
		}
		if !ok {
			for _, d := range c.profile.Defaults {
				if d.Target == r.Target {
					raw, ok = d.Value, true
				}
			}
		}
		if ok && !condition(r.Condition, input) {
			ok = false
		}
		if !ok {
			diags = append(diags, Diagnostic{r.Target, "source.missing", r.Source})
			if r.Null == NullError {
				return Result{}, fmt.Errorf("%w: %s", ErrMissing, r.Source)
			}
			if r.Null == NullDelete {
				fields = append(fields, Field{Target: r.Target, Deleted: true})
			}
			continue
		}
		v, err := apply(r, raw, c.profile.Lookups)
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
		if v == "" && r.DeleteIfEmpty {
			fields = append(fields, Field{Target: r.Target, Deleted: true})
			continue
		}
		if v == "" && r.Null == NullOmit {
			continue
		}
		if v == "" && r.Null == NullDelete {
			fields = append(fields, Field{Target: r.Target, Deleted: true})
			continue
		}
		fields = append(fields, Field{Target: r.Target, Value: v})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Target < fields[j].Target })
	sort.Slice(diags, func(i, j int) bool { return diags[i].Target < diags[j].Target })
	return Result{Fields: fields, Diagnostics: diags, Digest: resultDigest(c.digest, fields)}, nil
}
func (c Compiled) Map(input map[string]string) (Result, error) { return c.Execute(input) }

func apply(r Rule, s string, lookups map[string]map[string]string) (string, error) {
	op := strings.ToUpper(r.Op)
	switch op {
	case "IDENTITY", "RENAME":
		return s, nil
	case "TRIM":
		return strings.TrimSpace(s), nil
	case "UPPER":
		return strings.ToUpper(strings.TrimSpace(s)), nil
	case "LOWER":
		return strings.ToLower(strings.TrimSpace(s)), nil
	case "CONSTANT", "DEFAULT":
		return r.Argument, nil
	case "COMPOSE":
		return strings.ReplaceAll(r.Argument, "${value}", s), nil
	case "LOOKUP", "ENUM", "REFERENCE":
		m := r.Lookup
		if m == nil {
			m = lookups[r.LookupRef]
		}
		v, ok := m[strings.TrimSpace(s)]
		if !ok {
			return "", errors.New("lookup unresolved")
		}
		return v, nil
	case "DATE":
		t, e := time.Parse(r.Argument, strings.TrimSpace(s))
		if e != nil {
			return "", e
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case "MONEY":
		if !moneyRE.MatchString(strings.ReplaceAll(strings.TrimSpace(s), ",", "")) {
			return "", errors.New("invalid money")
		}
		return strings.ReplaceAll(strings.TrimSpace(s), ",", ""), nil
	case "TYPE":
		if r.Type == "string" || r.Type == "" {
			return s, nil
		}
		return s, nil
	case "CONDITION", "CONDITIONAL":
		return s, nil
	}
	return "", ErrUnsupported
}
func condition(expr string, in map[string]string) bool {
	if expr == "" {
		return true
	}
	p := strings.SplitN(expr, "==", 2)
	if len(p) != 2 {
		return false
	}
	k := strings.TrimSpace(p[0])
	v := strings.Trim(strings.TrimSpace(p[1]), "\"")
	return in[k] == v
}
func canonical(p Profile) []byte {
	b, _ := json.Marshal(struct {
		ID, Version, Source, Destination string
		Rules                            []Rule
		Defaults                         []Default
		Lookups                          map[string]map[string]string
	}{p.MappingID, p.Version, p.SourceSchemaRef, p.DestinationSchemaRef, p.Rules, p.Defaults, p.Lookups})
	return b
}
func resultDigest(base string, fs []Field) string {
	b, _ := json.Marshal(struct {
		Base   string
		Fields []Field
	}{base, fs})
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
