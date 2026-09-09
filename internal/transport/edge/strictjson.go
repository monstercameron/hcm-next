package edge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// Rule identifiers for the strict JSON admission screen. They are distinct
// from the Protobuf-side rules because a caller needs to know which encoding
// rejected the request.
const (
	ruleInvalidUTF8      = "strict_decoding.invalid_utf8"
	ruleMalformedJSON    = "strict_decoding.malformed_json"
	ruleDuplicateKey     = "strict_decoding.duplicate_key"
	ruleAmbiguousNumber  = "strict_decoding.ambiguous_number"
	ruleUnknownJSONField = "strict_decoding.unknown_field"
	ruleBodyTooLarge     = "strict_decoding.body_too_large"
)

// maxSignificantDigits is the largest number of significant decimal digits a
// JSON numeric literal may carry. Beyond it the value cannot round-trip
// through the IEEE-754 double that JSON tooling uses, so its meaning depends
// on which parser reads it. Human Capital Management Suite does not accept a value whose meaning is
// parser-dependent; a caller that needs more precision sends a Decimal, which
// is exactly why hcmnext.common.v1.Decimal exists.
const maxSignificantDigits = 15

// unknownFieldPattern extracts the offending field name from a protojson
// unknown-field error so the rejection can name a field path instead of
// echoing a library message across the edge.
var unknownFieldPattern = regexp.MustCompile(`unknown field "([^"]*)"`)

// screenJSONBody applies the strict decoding rules to a JSON request body for
// the given request message type, returning every violation it finds.
//
// The order matters: encoding validity first, then structure, then schema.
// Reporting "unknown field" for a body that is not even valid UTF-8 would be
// misleading.
func screenJSONBody(body []byte, msg proto.Message) []envelope.Violation {
	if !utf8.Valid(body) {
		return []envelope.Violation{{
			FieldPath:   "(request)",
			Description: "the request body is not valid UTF-8",
			RuleRef:     ruleInvalidUTF8,
		}}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	violations := scanJSONStructure(body)
	if len(violations) > 0 {
		return violations
	}

	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, msg); err != nil {
		if m := unknownFieldPattern.FindStringSubmatch(err.Error()); len(m) == 2 {
			return []envelope.Violation{{
				FieldPath:   m[1],
				Description: "the field is not part of this method's request contract",
				RuleRef:     ruleUnknownJSONField,
			}}
		}
		return []envelope.Violation{{
			FieldPath:   "(request)",
			Description: "the request body does not decode against this method's request contract",
			RuleRef:     ruleMalformedJSON,
		}}
	}
	return nil
}

// scanJSONStructure walks the raw JSON once, reporting duplicate object keys
// and numeric literals whose value is parser-dependent.
func scanJSONStructure(body []byte) []envelope.Violation {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var violations []envelope.Violation
	if err := scanJSONValue(dec, "", &violations); err != nil {
		return append(violations, envelope.Violation{
			FieldPath:   "(request)",
			Description: "the request body is not well-formed JSON",
			RuleRef:     ruleMalformedJSON,
		})
	}
	if _, err := dec.Token(); err != io.EOF {
		violations = append(violations, envelope.Violation{
			FieldPath:   "(request)",
			Description: "the request body carries trailing content after the JSON value",
			RuleRef:     ruleMalformedJSON,
		})
	}
	return violations
}

// scanJSONValue consumes exactly one JSON value from dec.
func scanJSONValue(dec *json.Decoder, path string, violations *[]envelope.Violation) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			seen := make(map[string]struct{})
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := keyTok.(string)
				child := joinJSONPath(path, key)
				if _, dup := seen[key]; dup {
					*violations = append(*violations, envelope.Violation{
						FieldPath:   child,
						Description: "the object contains this member more than once",
						RuleRef:     ruleDuplicateKey,
					})
				}
				seen[key] = struct{}{}
				if err := scanJSONValue(dec, child, violations); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		case '[':
			for i := 0; dec.More(); i++ {
				if err := scanJSONValue(dec, fmt.Sprintf("%s[%d]", path, i), violations); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		}
	case json.Number:
		if significantDigits(t.String()) > maxSignificantDigits {
			*violations = append(*violations, envelope.Violation{
				FieldPath:   emptyToRoot(path),
				Description: fmt.Sprintf("the numeric literal carries more than %d significant digits and is not unambiguously representable", maxSignificantDigits),
				RuleRef:     ruleAmbiguousNumber,
			})
		}
	}
	return nil
}

// significantDigits counts the significant decimal digits in a JSON numeric
// literal, ignoring sign, the decimal point, the exponent and leading zeros.
func significantDigits(literal string) int {
	mantissa := literal
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		mantissa = mantissa[:i]
	}
	mantissa = strings.TrimLeft(mantissa, "+-")
	digits := 0
	leading := true
	for _, r := range mantissa {
		switch {
		case r == '.':
		case r == '0' && leading:
		case r >= '0' && r <= '9':
			leading = false
			digits++
		}
	}
	return digits
}

// joinJSONPath appends a member name to a JSON path.
func joinJSONPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// emptyToRoot names the request root for a violation with no path.
func emptyToRoot(path string) string {
	if path == "" {
		return "(request)"
	}
	return path
}

// isJSONContentType reports whether a request body is JSON for connect's
// purposes. connect names the codec "json"; the charset suffix is the same
// codec.
func isJSONContentType(header http.Header) bool {
	ct := strings.ToLower(strings.TrimSpace(header.Get("Content-Type")))
	if ct == "" {
		return false
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct == "application/json" || ct == "application/connect+json" || ct == "application/grpc+json"
}
