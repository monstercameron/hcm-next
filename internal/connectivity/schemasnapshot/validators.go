package schemasnapshot

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidatorResult is one validator's verdict against one snapshot's raw
// bytes. Every configured [Validator] produces exactly one of these, whether
// it passed or failed, so [Evidence] always shows the complete picture
// rather than only the failures.
type ValidatorResult struct {
	Name   string
	Passed bool
	Detail string
}

// Validator is one bounded, offline check a snapshot's raw bytes must pass
// before [Ingest] will admit it. Check never performs I/O and never mutates
// its inputs: it is a pure function of the declared format and the bytes.
type Validator struct {
	Name  string
	Check func(format DeclaredFormat, raw []byte) (passed bool, detail string)
}

// Decide runs every validator against raw, in order, without short-
// circuiting on the first failure: [Evidence] is meant to show the complete
// verdict, not just whichever check happened to fail first. The snapshot is
// admitted only when every validator passes.
func Decide(format DeclaredFormat, raw []byte, validators []Validator) (State, []ValidatorResult) {
	results := make([]ValidatorResult, 0, len(validators))
	verdict := StateAdmitted
	for _, v := range validators {
		passed, detail := v.Check(format, raw)
		results = append(results, ValidatorResult{Name: v.Name, Passed: passed, Detail: detail})
		if !passed {
			verdict = StateRejected
		}
	}
	return verdict, results
}

// DefaultMaxBytes bounds a snapshot the way
// internal/data/artifacts.DefaultMaxContentBytes bounds an ordinary
// artifact: it is a policy default, not the schema's own absolute backstop
// (migrations/00025's integration_schema_snapshot_byte_size_bounded CHECK is
// that backstop, at 64 MiB, and no caller-chosen bound here can exceed it).
const DefaultMaxBytes int64 = 4 << 20 // 4 MiB

// DefaultValidators returns the three validators INTG-004's GREEN clause
// names: well-formedness for the declared format, a size bound, and a
// forbidden-content scan. Order matters only for readability of
// [Evidence].Results; Decide never short-circuits.
func DefaultValidators(maxBytes int64) []Validator {
	return []Validator{
		WellFormednessValidator(),
		SizeBoundValidator(maxBytes),
		ForbiddenContentValidator(nil),
	}
}

// WellFormednessValidator checks that raw is well-formed under its declared
// format: valid JSON for JSON_SCHEMA/OPENAPI, well-formed XML for XSD/WSDL, a
// non-empty header with no blank or repeated column for CSV_HEADER, and
// well-formed, non-empty UTF-8 text with no NUL byte for GRAPHQL_SDL and
// PROTOBUF -- the generic check a MINIMAL CONTRACT gets when this package
// does not parse the grammar itself.
func WellFormednessValidator() Validator {
	return Validator{Name: "WELL_FORMED", Check: checkWellFormed}
}

func checkWellFormed(format DeclaredFormat, raw []byte) (bool, string) {
	switch format {
	case FormatJSONSchema, FormatOpenAPI:
		if !json.Valid(raw) {
			return false, "content is not well-formed JSON"
		}
		return true, "well-formed JSON"
	case FormatXSD, FormatWSDL:
		return checkWellFormedXML(raw)
	case FormatCSVHeader:
		return checkCSVHeader(raw)
	case FormatGraphQLSDL, FormatProtobuf:
		return checkPlainText(raw)
	default:
		return false, fmt.Sprintf("declared format %q is not recognized", string(format))
	}
}

func checkWellFormedXML(raw []byte) (bool, string) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	tokens := 0
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, fmt.Sprintf("not well-formed XML: %v", err)
		}
		tokens++
	}
	if tokens == 0 {
		return false, "content has no XML tokens"
	}
	return true, "well-formed XML"
}

func checkCSVHeader(raw []byte) (bool, string) {
	if !utf8.Valid(raw) {
		return false, "header is not valid UTF-8"
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return false, "header line is empty"
	}
	fields := strings.Split(line, ",")
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		name := strings.TrimSpace(f)
		if name == "" {
			return false, "header contains an empty column name"
		}
		if seen[name] {
			return false, fmt.Sprintf("header repeats column %q", name)
		}
		seen[name] = true
	}
	return true, fmt.Sprintf("%d header columns", len(fields))
}

func checkPlainText(raw []byte) (bool, string) {
	if len(raw) == 0 {
		return false, "content is empty"
	}
	if !utf8.Valid(raw) {
		return false, "content is not valid UTF-8"
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return false, "content contains a NUL byte"
	}
	return true, "well-formed text"
}

// SizeBoundValidator rejects empty content and content beyond maxBytes. It
// is a policy bound a caller configures per connector or tenant; it is
// always tighter than or equal to the schema's own absolute backstop.
func SizeBoundValidator(maxBytes int64) Validator {
	return Validator{
		Name: "SIZE_BOUND",
		Check: func(_ DeclaredFormat, raw []byte) (bool, string) {
			if maxBytes <= 0 {
				return false, "size bound is not configured"
			}
			n := int64(len(raw))
			if n == 0 {
				return false, "content is empty"
			}
			if n > maxBytes {
				return false, fmt.Sprintf("content is %d bytes, exceeds bound %d", n, maxBytes)
			}
			return true, fmt.Sprintf("%d bytes within bound %d", n, maxBytes)
		},
	}
}

// ForbiddenContentScanner reports whether raw carries content that must
// block admission, and why. It is the "forbidden-content scan hook" INTG-004
// names: a caller may plug in a tenant- or connector-specific scanner (a DLP
// classifier, a secret-pattern list maintained elsewhere); nil selects
// [DefaultForbiddenContentScan].
type ForbiddenContentScanner func(raw []byte) (blocked bool, reason string)

// ForbiddenContentValidator wraps a [ForbiddenContentScanner] as a
// [Validator].
func ForbiddenContentValidator(scan ForbiddenContentScanner) Validator {
	if scan == nil {
		scan = DefaultForbiddenContentScan
	}
	return Validator{
		Name: "FORBIDDEN_CONTENT",
		Check: func(_ DeclaredFormat, raw []byte) (bool, string) {
			if blocked, reason := scan(raw); blocked {
				return false, reason
			}
			return true, "no forbidden content markers found"
		},
	}
}

// forbiddenMarkers are literal substrings whose presence in a schema
// document is never legitimate: a schema describes shape, not secret
// material. The set is deliberately small and literal (a MINIMAL CONTRACT
// default); a caller with a richer policy supplies its own
// [ForbiddenContentScanner] instead of widening this list.
var forbiddenMarkers = []string{
	"-----BEGIN ", // PEM-encoded private key or certificate material
	"AKIA",        // AWS access key id prefix
	"ghp_",        // GitHub personal access token prefix
}

// jwtShape matches a JWT-shaped token: three base64url segments joined by
// dots, the first starting with "eyJ" (the base64 of a JSON object's opening
// "{"), the same heuristic internal/connectivity's credential reference
// parser uses to reject inline tokens.
var jwtShape = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)

// DefaultForbiddenContentScan is the built-in forbidden-content scan: a
// small literal-marker list plus a JWT-shape heuristic. It is exported so a
// caller building a stricter [ForbiddenContentScanner] can run it first and
// add more checks on top, rather than reimplementing it.
func DefaultForbiddenContentScan(raw []byte) (bool, string) {
	text := string(raw)
	for _, marker := range forbiddenMarkers {
		if strings.Contains(text, marker) {
			return true, fmt.Sprintf("content contains forbidden marker %q", marker)
		}
	}
	if jwtShape.MatchString(text) {
		return true, "content contains a JWT-shaped token"
	}
	return false, ""
}
