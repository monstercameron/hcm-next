package main

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	navigationGroupStateMaxBytes   = 4096
	navigationGroupStateMaxEntries = 64
)

func decodeNavigationGroupState(raw string) map[string]bool {
	state := map[string]bool{}
	if len(raw) == 0 || len(raw) > navigationGroupStateMaxBytes {
		return state
	}
	var decoded map[string]bool
	if json.Unmarshal([]byte(raw), &decoded) != nil {
		return state
	}
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(state) == navigationGroupStateMaxEntries {
			break
		}
		if validNavigationGroupKey(key) {
			state[key] = decoded[key]
		}
	}
	return state
}

func encodeNavigationGroupState(state map[string]bool) string {
	clean := map[string]bool{}
	keys := make([]string, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(clean) == navigationGroupStateMaxEntries {
			break
		}
		if validNavigationGroupKey(key) {
			clean[key] = state[key]
		}
	}
	body, err := json.Marshal(clean)
	if err != nil || len(body) > navigationGroupStateMaxBytes {
		return "{}"
	}
	return string(body)
}

func validNavigationGroupKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if r != '-' {
					return false
				}
			}
		}
	}
	return true
}
