package list

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
	MinPageSize     = 1
	CursorTTL       = 5 * time.Minute
)

type ResourceName struct {
	Tenant string
	Kind   string
	ID     string
}

func ParseResourceName(s string) (ResourceName, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 4 {
		return ResourceName{}, fmt.Errorf("invalid resource name")
	}
	if parts[0] != "tenants" || parts[2] == "" {
		return ResourceName{}, fmt.Errorf("invalid resource name")
	}
	tenant := parts[1]
	kind := parts[2]
	id := parts[3]
	if tenant == "" || kind == "" || id == "" {
		return ResourceName{}, fmt.Errorf("invalid resource name")
	}
	if strings.Contains(id, " ") || strings.Contains(tenant, " ") {
		return ResourceName{}, fmt.Errorf("invalid resource name")
	}
	return ResourceName{Tenant: tenant, Kind: kind, ID: id}, nil
}

func (r ResourceName) String() string {
	return fmt.Sprintf("tenants/%s/%s/%s", r.Tenant, r.Kind, r.ID)
}

type CursorPayload struct {
	Principal string `json:"p"`
	Tenant    string `json:"t"`
	Filter    string `json:"f"`
	Watermark string `json:"w"`
	Version   int    `json:"v"`
	ExpiresAt int64  `json:"e"`
	Nonce     string `json:"n"`
}

func EncodeCursor(p CursorPayload, secret string) (string, error) {
	if p.Principal == "" || p.Tenant == "" {
		return "", fmt.Errorf("missing principal or tenant")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := mac.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + hex.EncodeToString(sig)
	return token, nil
}

func DecodeCursor(token, secret string, now time.Time, expectPrincipal, expectTenant, expectFilter string) (CursorPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return CursorPayload{}, fmt.Errorf("invalid cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return CursorPayload{}, fmt.Errorf("invalid cursor")
	}
	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		return CursorPayload{}, fmt.Errorf("invalid cursor")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return CursorPayload{}, fmt.Errorf("invalid cursor")
	}
	var p CursorPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return CursorPayload{}, fmt.Errorf("invalid cursor")
	}
	if p.Principal != expectPrincipal || p.Tenant != expectTenant || p.Filter != expectFilter {
		return CursorPayload{}, fmt.Errorf("invalid cursor")
	}
	if now.Unix() >= p.ExpiresAt {
		return CursorPayload{}, fmt.Errorf("cursor expired")
	}
	return p, nil
}

type Item struct {
	ID   string
	Data map[string]any
}

type ListRequest struct {
	Principal string
	Tenant    string
	Filter    string
	PageSize  int
	Cursor    string
	FieldMask []string
}

type ListResponse struct {
	Items         []Item
	NextCursor    string
	RedactedCount int
	AllowedCount  int
	DeniedCount   int
}

func NormalizePageSize(n int) (int, error) {
	if n == 0 {
		return DefaultPageSize, nil
	}
	if n < MinPageSize || n > MaxPageSize {
		return 0, fmt.Errorf("page size out of bounds")
	}
	return n, nil
}

func ListItems(all []Item, req ListRequest, secret string, now time.Time, allowedFields map[string]bool) (ListResponse, error) {
	size, err := NormalizePageSize(req.PageSize)
	if err != nil {
		return ListResponse{}, err
	}
	sorted := append([]Item(nil), all...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	start := 0
	if req.Cursor != "" {
		cp, err := DecodeCursor(req.Cursor, secret, now, req.Principal, req.Tenant, req.Filter)
		if err != nil {
			return ListResponse{}, fmt.Errorf("invalid cursor")
		}
		for i, it := range sorted {
			if it.ID > cp.Watermark {
				start = i
				break
			}
			if it.ID == cp.Watermark {
				start = i + 1
				break
			}
		}
		if start >= len(sorted) {
			return ListResponse{Items: nil}, nil
		}
	}
	end := start + size
	if end > len(sorted) {
		end = len(sorted)
	}
	page := sorted[start:end]
	filtered := make([]Item, 0, len(page))
	redacted := 0
	for _, it := range page {
		out := Item{ID: it.ID, Data: map[string]any{}}
		for _, f := range req.FieldMask {
			if !allowedFields[f] {
				redacted++
				continue
			}
			if v, ok := it.Data[f]; ok {
				out.Data[f] = v
			}
		}
		if len(req.FieldMask) == 0 {
			for k, v := range it.Data {
				if allowedFields[k] {
					out.Data[k] = v
				} else {
					redacted++
				}
			}
		}
		filtered = append(filtered, out)
	}
	var nextCursor string
	if end < len(sorted) {
		cp := CursorPayload{
			Principal: req.Principal,
			Tenant:    req.Tenant,
			Filter:    req.Filter,
			Watermark: sorted[end-1].ID,
			Version:   1,
			ExpiresAt: now.Add(CursorTTL).Unix(),
			Nonce:     fmt.Sprintf("%d", now.UnixNano()),
		}
		nextCursor, _ = EncodeCursor(cp, secret)
	}
	return ListResponse{Items: filtered, NextCursor: nextCursor, RedactedCount: redacted, AllowedCount: len(filtered), DeniedCount: redacted}, nil
}

func UniformError() error {
	return fmt.Errorf("not found")
}

func DigestList(items []Item) string {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	h := sha256.New()
	for _, it := range items {
		fmt.Fprintf(h, "%s;", it.ID)
		keys := make([]string, 0, len(it.Data))
		for k := range it.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(h, "%s=%v;", k, it.Data[k])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
