// Package secrets defines the metadata-only boundary for secret custody.
//
// A SecretReference is safe to persist in configuration, evidence, logs and
// snapshots. It intentionally has no field that can contain secret material;
// resolving a reference belongs to a later, authorized custody adapter.
package secrets

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Kind identifies the custody object without describing or carrying its value.
type Kind string

const (
	OpaqueSecret         Kind = "OPAQUE_SECRET"
	DatabaseCredential   Kind = "DATABASE_CREDENTIAL"
	APICredential        Kind = "API_CREDENTIAL"
	OAuthClientSecret    Kind = "OAUTH_CLIENT_SECRET"
	OAuthGrant           Kind = "OAUTH_GRANT"
	SymmetricKey         Kind = "SYMMETRIC_KEY"
	AsymmetricKeyHandle  Kind = "ASYMMETRIC_KEY_HANDLE"
	SigningKeyHandle     Kind = "SIGNING_KEY_HANDLE"
	CertificateKeyHandle Kind = "CERTIFICATE_KEY_HANDLE"
	ExternalKMSReference Kind = "EXTERNAL_KMS_REFERENCE"
)

// State is the lifecycle state of a secret reference.
type State string

const (
	Requested State = "REQUESTED"
	Active    State = "ACTIVE"
	Rotating  State = "ROTATING"
	ActiveNew State = "ACTIVE_NEW"
	Disabled  State = "DISABLED"
	Destroyed State = "DESTROYED"
)

// SecretReference is the only secret representation allowed in durable data.
// ProviderPath is an opaque provider locator, never a value or a credential.
type SecretReference struct {
	ID           string `json:"id"`
	Kind         Kind   `json:"kind"`
	Version      string `json:"version"`
	Provider     string `json:"provider"`
	ProviderPath string `json:"provider_path"`
	Tenant       string `json:"tenant"`
	Region       string `json:"region"`
	State        State  `json:"state"`
}

var ErrInvalidReference = errors.New("secrets: invalid secret reference")

// Validate checks the reference's identity and lifecycle metadata. It never
// contacts a provider and cannot retrieve a raw value.
func (r SecretReference) Validate() error {
	if r.ID == "" || r.Version == "" || r.Provider == "" || r.ProviderPath == "" || r.Tenant == "" || r.Region == "" {
		return fmt.Errorf("%w: id, version, provider, provider_path, tenant and region are required", ErrInvalidReference)
	}
	for _, item := range []struct{ name, value string }{
		{"id", r.ID}, {"version", r.Version}, {"provider", r.Provider},
		{"provider_path", r.ProviderPath}, {"tenant", r.Tenant}, {"region", r.Region},
	} {
		if strings.TrimSpace(item.value) != item.value || strings.IndexFunc(item.value, unicode.IsControl) >= 0 {
			return fmt.Errorf("%w: %s contains invalid whitespace or control characters", ErrInvalidReference, item.name)
		}
	}
	if !validKind(r.Kind) {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidReference, r.Kind)
	}
	if !validState(r.State) {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidReference, r.State)
	}
	if strings.Contains(strings.ToLower(r.ProviderPath), "password=") || sensitiveAssignment.MatchString(r.ProviderPath) {
		return fmt.Errorf("%w: provider path is not a credential", ErrInvalidReference)
	}
	return nil
}

func validKind(k Kind) bool {
	switch k {
	case OpaqueSecret, DatabaseCredential, APICredential, OAuthClientSecret, OAuthGrant, SymmetricKey, AsymmetricKeyHandle, SigningKeyHandle, CertificateKeyHandle, ExternalKMSReference:
		return true
	}
	return false
}
func validState(s State) bool {
	switch s {
	case Requested, Active, Rotating, ActiveNew, Disabled, Destroyed:
		return true
	}
	return false
}

// Finding identifies a redaction violation without echoing its value.
type Finding struct{ Path, Code string }

// Scan walks maps, structs, slices and pointers and finds likely raw secret
// material. Reference-shaped fields and SecretReference values are accepted.
// The returned findings contain paths and stable codes only, never values.
func Scan(snapshot any) []Finding {
	var out []Finding
	active := make(map[visit]bool)
	var walk func(reflect.Value, string, string)
	walk = func(v reflect.Value, path, field string) {
		if !v.IsValid() {
			return
		}
		for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			if v.Kind() == reflect.Pointer {
				key := visit{typ: v.Type(), ptr: v.Pointer()}
				if active[key] {
					return
				}
				active[key] = true
				defer delete(active, key)
			}
			v = v.Elem()
		}
		if v.Type() == reflect.TypeOf(SecretReference{}) {
			if r, ok := v.Interface().(SecretReference); ok && r.Validate() != nil {
				out = append(out, Finding{path, "invalid_reference"})
			}
			return
		}
		switch v.Kind() {
		case reflect.Map:
			for _, key := range v.MapKeys() {
				if key.Kind() != reflect.String {
					continue
				}
				k := key.String()
				p := path + "." + k
				walk(v.MapIndex(key), p, k)
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				sf := v.Type().Field(i)
				if sf.PkgPath != "" {
					continue
				}
				p := path + "." + sf.Name
				walk(v.Field(i), p, sf.Name)
			}
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				if looksSensitiveField(field) && !looksReferenceField(field) && v.Len() != 0 {
					out = append(out, Finding{path, "plaintext_secret_field"})
				} else if looksSecretValue(string(v.Bytes())) {
					out = append(out, Finding{path, "secret-shaped_value"})
				}
				return
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i), field)
			}
		case reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				if looksSensitiveField(field) && !looksReferenceField(field) && v.Len() != 0 {
					out = append(out, Finding{path, "plaintext_secret_field"})
				} else {
					buf := make([]byte, v.Len())
					for i := range buf {
						buf[i] = byte(v.Index(i).Uint())
					}
					if looksSecretValue(string(buf)) {
						out = append(out, Finding{path, "secret-shaped_value"})
					}
				}
				return
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i), field)
			}
		case reflect.String:
			if looksSensitiveField(field) && !looksReferenceField(field) && v.String() != "" {
				out = append(out, Finding{path, "plaintext_secret_field"})
				return
			}
			if looksSecretValue(v.String()) {
				out = append(out, Finding{path, "secret-shaped_value"})
			}
		}
	}
	walk(reflect.ValueOf(snapshot), "$", "")
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Code < out[j].Code
		}
		return out[i].Path < out[j].Path
	})
	return out
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

// Check is the enforcement point for durable snapshots and configuration.
func Check(snapshot any) error {
	findings := Scan(snapshot)
	if len(findings) == 0 {
		return nil
	}
	return fmt.Errorf("secrets: snapshot contains prohibited secret material at %s (%s)", findings[0].Path, findings[0].Code)
}

func looksReferenceField(s string) bool {
	s = normalizeName(s)
	return strings.Contains(s, "ref") || strings.Contains(s, "locator") || strings.Contains(s, "providerpath")
}
func looksSensitiveField(s string) bool {
	s = normalizeName(s)
	for _, x := range []string{"password", "secret", "token", "credential", "privatekey", "apikey", "clientsecret", "rawvalue"} {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func normalizeName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

var secretValue = regexp.MustCompile(`(?i)-----begin [^-]+ key-----|(?:^|\s)(?:sk|ghp|glpat|xox[baprs])-[-_A-Za-z0-9]{12,}|(?:^|\s)AKIA[0-9A-Z]{12,}`)
var sensitiveAssignment = regexp.MustCompile(`(?i)(?:^|[?&;/])(password|secret|token|credential|api[_-]?key|private[_-]?key)=`)

func looksSecretValue(s string) bool { return secretValue.MatchString(s) }
