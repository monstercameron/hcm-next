package recordsmeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalScopeDigest returns the sha256 hex digest of predicate's
// canonical form: object keys sorted, no insignificant whitespace, array
// order preserved. Two JSON encodings of the same logical scope -- whatever
// order their keys were written in -- always produce the same digest.
//
// This is what makes RECORDS-HOLD-001's REFACTOR clause true: hold matching
// is deterministic and explainable because the scope snapshot a hold
// carries, and the reason recorded for every copy it grips, are both pure
// functions of the predicate's content, never of map iteration order or
// which copy happened to be scanned first.
func CanonicalScopeDigest(predicate json.RawMessage) (string, error) {
	if len(predicate) == 0 {
		predicate = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(predicate, &value); err != nil {
		return "", fmt.Errorf("recordsmeta: scope predicate is not valid json: %w", err)
	}
	canonical, err := canonicalizeJSON(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalizeJSON renders value with object keys sorted and no
// insignificant whitespace, recursively, so that structurally identical
// JSON always renders to identical bytes.
func canonicalizeJSON(value any) ([]byte, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(keyBytes)
			buf.WriteByte(':')
			valueBytes, err := canonicalizeJSON(v[k])
			if err != nil {
				return nil, err
			}
			buf.Write(valueBytes)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			itemBytes, err := canonicalizeJSON(item)
			if err != nil {
				return nil, err
			}
			buf.Write(itemBytes)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(v)
	}
}
