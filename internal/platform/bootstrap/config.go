package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FieldKind identifies how a Field's resolved raw string value should be
// interpreted.
type FieldKind int

const (
	// KindString leaves the resolved value as-is.
	KindString FieldKind = iota
	// KindDuration parses the resolved value with time.ParseDuration.
	KindDuration
	// KindInt parses the resolved value as a base-10 integer.
	KindInt
	// KindBool parses the resolved value with strconv.ParseBool.
	KindBool
)

// Field declares one configuration value sourced from a command-line flag
// and/or an environment variable, with a default and a secrecy marking.
type Field struct {
	// Name is the flag name (e.g. "database-url"), registered as -Name.
	Name string
	// Env is the environment variable name consulted when the flag is not
	// explicitly passed (e.g. "HCMNEXT_DATABASE_URL"). Empty means this
	// field has no environment override.
	Env string
	// Usage is the flag's help text.
	Usage string
	// Default is used when neither the flag nor the environment variable
	// is set.
	Default string
	// Kind determines how Values' typed accessors parse the resolved
	// value. It never changes which source wins.
	Kind FieldKind
	// Secret marks a field whose resolved value must never appear in
	// Values.Effective, Values.Fingerprint or any log attribute bootstrap
	// emits; RedactedValue is printed in its place.
	Secret bool
}

// RedactedValue is printed in place of a Secret field's resolved value.
const RedactedValue = "[REDACTED]"

// source names which precedence tier resolved a Field's value.
type source string

const (
	sourceFlag    source = "flag"
	sourceEnv     source = "env"
	sourceDefault source = "default"
)

// Values is the parsed, precedence-resolved result of ParseConfig.
type Values struct {
	fields map[string]Field
	order  []string // field names, in Field declaration order
	raw    map[string]string
	src    map[string]source
}

// Has reports whether name was declared as a Field.
func (v *Values) Has(name string) bool {
	_, ok := v.fields[name]
	return ok
}

// String returns the resolved raw value of the named field. It panics if
// name was not declared, since that is always a programming error (a typo
// between Field.Name and the accessor call site) rather than recoverable
// input.
func (v *Values) String(name string) string {
	if !v.Has(name) {
		panic(fmt.Sprintf("bootstrap: config field %q was never declared", name))
	}
	return v.raw[name]
}

// Duration parses the named field's resolved value as a time.Duration.
func (v *Values) Duration(name string) (time.Duration, error) {
	s := v.String(name)
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("bootstrap: config field %q value %q: %w", name, s, err)
	}
	return d, nil
}

// Int parses the named field's resolved value as an int.
func (v *Values) Int(name string) (int, error) {
	s := v.String(name)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bootstrap: config field %q value %q: %w", name, s, err)
	}
	return n, nil
}

// Bool parses the named field's resolved value as a bool.
func (v *Values) Bool(name string) (bool, error) {
	s := v.String(name)
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("bootstrap: config field %q value %q: %w", name, s, err)
	}
	return b, nil
}

// Source reports which precedence tier ("flag", "env" or "default")
// resolved the named field's value. It exists mainly so tests can assert
// precedence directly instead of only its observable effect.
func (v *Values) Source(name string) string {
	if !v.Has(name) {
		panic(fmt.Sprintf("bootstrap: config field %q was never declared", name))
	}
	return string(v.src[name])
}

// display returns the value that is safe to print or log for the named
// field: the resolved value, or RedactedValue if the field is Secret.
func (v *Values) display(name string) string {
	if v.fields[name].Secret {
		return RedactedValue
	}
	return v.raw[name]
}

// Effective renders every declared field, in declaration order, as one
// "name=value (source=...)" line per field, with Secret fields redacted.
// Commands print this once at startup so the running configuration is
// always inspectable without ever leaking a secret into a log or terminal.
func (v *Values) Effective() string {
	var b strings.Builder
	for _, name := range v.order {
		fmt.Fprintf(&b, "%s=%s (source=%s)\n", name, v.display(name), v.src[name])
	}
	return b.String()
}

// LogAttrs returns the effective configuration as alternating key/value
// pairs suitable for a Logger call (logger.Info("bootstrap.config",
// values.LogAttrs()...)), with Secret fields redacted the same way
// Effective redacts them.
func (v *Values) LogAttrs() []any {
	attrs := make([]any, 0, len(v.order)*2)
	for _, name := range v.order {
		attrs = append(attrs, name, v.display(name))
	}
	return attrs
}

// Fingerprint returns a stable sha256 hex digest of the effective,
// redaction-safe configuration. Two processes with the same fingerprint
// were started with the same non-secret configuration; the digest never
// exposes a secret value (a changed secret does not change the fingerprint,
// by design — this is a drift signal for observability, not a content
// hash).
func (v *Values) Fingerprint() string {
	h := sha256.New()
	names := append([]string(nil), v.order...)
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(h, "%s=%s\n", name, v.display(name))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ParseConfig resolves every declared field against args (as flags) and
// getenv (as environment fallbacks), applying flag > env > default
// precedence per field independently. It returns an error — without
// starting anything — if args contains an unregistered flag or a
// -h/-help request; that error must be treated as a config failure by the
// caller (Run maps it to ExitConfigError before any listener or workload
// starts).
func ParseConfig(args []string, getenv EnvLookup, fields []Field) (*Values, error) {
	if getenv == nil {
		getenv = defaultEnvLookup()
	}

	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	flagVals := make(map[string]*string, len(fields))
	for _, f := range fields {
		flagVals[f.Name] = fs.String(f.Name, "", f.Usage)
	}
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("bootstrap: parsing flags: %w", err)
	}

	explicit := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { explicit[fl.Name] = true })

	v := &Values{
		fields: make(map[string]Field, len(fields)),
		order:  make([]string, 0, len(fields)),
		raw:    make(map[string]string, len(fields)),
		src:    make(map[string]source, len(fields)),
	}
	for _, f := range fields {
		v.fields[f.Name] = f
		v.order = append(v.order, f.Name)

		switch {
		case explicit[f.Name]:
			v.raw[f.Name] = *flagVals[f.Name]
			v.src[f.Name] = sourceFlag
		default:
			if f.Env != "" {
				if envVal, ok := getenv(f.Env); ok {
					v.raw[f.Name] = envVal
					v.src[f.Name] = sourceEnv
					continue
				}
			}
			v.raw[f.Name] = f.Default
			v.src[f.Name] = sourceDefault
		}
	}
	return v, nil
}
