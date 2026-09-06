// Package artifactstore defines a provider-neutral contract for private,
// immutable, tenant-scoped S3-compatible artifact storage.
package artifactstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

func Version() int { return contractVersion }

func Explain() string {
	return "IAC-007 v1: private versioned immutable encrypted tenant-scoped artifact storage with holds and lifecycle"
}

type LifecycleRule struct {
	Class       string
	ExpireAfter time.Duration
	Archive     bool
}

type Policy struct {
	Endpoint                string
	Region                  string
	Private                 bool
	TLSRequired             bool
	Versioning              bool
	ObjectLock              bool
	EncryptionRequired      bool
	TenantKeyReferences     bool
	RestoreReadVerification bool
	DefaultRetention        time.Duration
	Lifecycle               []LifecycleRule
}

type Object struct {
	TenantID     string
	Key          string
	VersionID    string
	Region       string
	Digest       string
	KeyReference string
	CreatedAt    time.Time
	RetainUntil  time.Time
	Hold         bool
}

type PutRequest struct {
	Object      Object
	ExistingKey bool
}

type Decision struct {
	Allowed bool
	Code    string
	Detail  string
}

type LifecycleAction string

const (
	LifecycleRetain  LifecycleAction = "RETAIN"
	LifecycleArchive LifecycleAction = "ARCHIVE"
	LifecycleExpire  LifecycleAction = "EXPIRE"
)

var ErrInvalidPolicy = errors.New("artifactstore: invalid storage policy")

type Violation struct {
	Field  string
	Code   string
	Detail string
}

func Validate(p Policy) []Violation {
	var out []Violation
	need := func(field, code, detail string, bad bool) {
		if bad {
			out = append(out, Violation{Field: field, Code: code, Detail: detail})
		}
	}
	need("endpoint", "ENDPOINT_REQUIRED", "private provider endpoint is required", strings.TrimSpace(p.Endpoint) == "")
	need("region", "REGION_REQUIRED", "one approved region is required", strings.TrimSpace(p.Region) == "")
	need("private", "PRIVATE_REQUIRED", "artifact storage must be private", !p.Private)
	need("tls", "TLS_REQUIRED", "artifact access must require TLS", !p.TLSRequired)
	need("versioning", "VERSIONING_REQUIRED", "object versioning must be enabled", !p.Versioning)
	need("object_lock", "IMMUTABILITY_REQUIRED", "object lock must be enabled", !p.ObjectLock)
	need("encryption", "ENCRYPTION_REQUIRED", "objects must be encrypted", !p.EncryptionRequired)
	need("tenant_key_references", "TENANT_KEYS_REQUIRED", "encryption must use tenant key references", !p.TenantKeyReferences)
	need("restore_read_verification", "RESTORE_VERIFICATION_REQUIRED", "restore/read verification must be enabled", !p.RestoreReadVerification)
	need("default_retention", "RETENTION_REQUIRED", "default retention must be positive", p.DefaultRetention <= 0)
	if len(p.Lifecycle) == 0 {
		out = append(out, Violation{Field: "lifecycle", Code: "LIFECYCLE_REQUIRED", Detail: "at least one lifecycle rule is required"})
	}
	seen := map[string]bool{}
	for _, rule := range p.Lifecycle {
		if strings.TrimSpace(rule.Class) == "" || rule.ExpireAfter <= 0 || seen[rule.Class] {
			out = append(out, Violation{Field: "lifecycle", Code: "LIFECYCLE_INVALID", Detail: "lifecycle classes must be unique and have a positive expiry"})
		}
		seen[rule.Class] = true
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func Check(p Policy) error {
	if violations := Validate(p); len(violations) != 0 {
		v := violations[0]
		return fmt.Errorf("%w: %s %s: %s", ErrInvalidPolicy, v.Code, v.Field, v.Detail)
	}
	return nil
}

// EvaluatePut admits metadata for a single immutable object version. The
// adapter is responsible for writing bytes only after this decision passes.
func EvaluatePut(p Policy, req PutRequest) Decision {
	if err := Check(p); err != nil {
		return Decision{Code: "INVALID_POLICY", Detail: err.Error()}
	}
	o := req.Object
	if strings.TrimSpace(o.TenantID) == "" || strings.TrimSpace(o.Key) == "" || strings.TrimSpace(o.VersionID) == "" || strings.TrimSpace(o.Digest) == "" || strings.TrimSpace(o.KeyReference) == "" {
		return Decision{Code: "OBJECT_METADATA_REQUIRED", Detail: "tenant, key, version, digest, and key reference are required"}
	}
	if strings.Contains(o.Key, "..") || strings.HasPrefix(o.Key, "/") {
		return Decision{Code: "INVALID_OBJECT_KEY", Detail: "object key must not escape its tenant prefix"}
	}
	if !strings.HasPrefix(o.Digest, "sha256:") || len(strings.TrimPrefix(o.Digest, "sha256:")) != 64 {
		return Decision{Code: "DIGEST_REQUIRED", Detail: "object digest must be a sha256 identity"}
	}
	if o.Region != p.Region {
		return Decision{Code: "REGION_MISMATCH", Detail: "object region is not the approved storage region"}
	}
	if !strings.HasPrefix(o.KeyReference, "kms://"+o.TenantID+"/") {
		return Decision{Code: "TENANT_KEY_MISMATCH", Detail: "object key reference must be scoped to the object tenant"}
	}
	if req.ExistingKey {
		return Decision{Code: "IMMUTABLE_OVERWRITE", Detail: "existing object keys require a new version"}
	}
	if o.RetainUntil.IsZero() || !o.RetainUntil.After(o.CreatedAt) {
		return Decision{Code: "RETENTION_INVALID", Detail: "object retention must be after creation"}
	}
	if o.RetainUntil.Before(o.CreatedAt.Add(p.DefaultRetention)) {
		return Decision{Code: "RETENTION_TOO_SHORT", Detail: "object retention is shorter than the policy default"}
	}
	return Decision{Allowed: true, Code: "OBJECT_WRITE_ALLOWED", Detail: "new encrypted immutable version admitted"}
}

func Lifecycle(p Policy, object Object, now time.Time) (LifecycleAction, error) {
	if err := Check(p); err != nil {
		return "", err
	}
	if object.Hold || now.Before(object.RetainUntil) {
		return LifecycleRetain, nil
	}
	for _, rule := range p.Lifecycle {
		if !object.CreatedAt.IsZero() && !now.Before(object.CreatedAt.Add(rule.ExpireAfter)) {
			if rule.Archive {
				return LifecycleArchive, nil
			}
			return LifecycleExpire, nil
		}
	}
	return LifecycleRetain, nil
}

// Digest returns the content identity used by fixture adapters without
// retaining content in this package.
func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
