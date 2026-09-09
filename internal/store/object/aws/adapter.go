// Package aws contains the provider-side S3 transport for the object-store
// contract.  It deliberately uses the S3 HTTP surface rather than exposing
// an SDK request, response, bucket, key, version, or retention type to the
// artifact owner.
package aws

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/store/object"
)

const (
	AdapterVersion         = "s3-http-adapter/v1"
	CredentialNone         = "NONE"
	CredentialStaticHeader = "STATIC_AUTHORIZATION_HEADER"
	RemovalDelete          = "DELETE_OBJECT_ON_ABORT"
	RemovalPolicy          = RemovalDelete
)

var (
	ErrInvalidConfig       = errors.New("aws object adapter: invalid configuration")
	ErrProviderUnavailable = errors.New("aws object adapter: provider unavailable")
	ErrProviderResponse    = errors.New("aws object adapter: provider response invalid")
	ErrChecksumMismatch    = errors.New("aws object adapter: provider checksum mismatch")
)

// HTTPDoer is the small seam used to qualify the adapter without a cloud or
// SDK dependency.  *http.Client satisfies it.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// RetryPolicy records the reviewed transport policy.  The adapter never
// automatically replays a write: an uncertain PUT or DELETE must be resolved
// by the object contract's stat/recovery path first.
type RetryPolicy struct {
	ReadMaxAttempts  int
	WriteMaxAttempts int
	RetryableStatus  []int
}

// Config is provider configuration, not artifact identity.  Endpoint and
// Bucket are used only to form a provider locator.  Authorization is kept as
// an opaque header value and is never included in errors or explanations.
type Config struct {
	Endpoint      string
	Bucket        string
	HTTPClient    HTTPDoer
	Authorization string
	Retry         RetryPolicy
}

// Retention carries the provider's exact object-lock result inputs without
// making retention a requirement of the provider-neutral object contract.
type Retention struct {
	RetainUntil time.Time
	LegalHold   bool
}

// PutOptions are provider mechanics.  The artifact owner remains responsible
// for deciding whether a retention or hold policy applies.
type PutOptions struct {
	Retention Retention
}

// ProviderInfo preserves provider metadata alongside the owned object.Info.
// VersionID, ETag, and checksum are evidence fields, never artifact identity.
type ProviderInfo struct {
	Object      object.Info
	VersionID   string
	ETag        string
	ChecksumSHA string
	RetainUntil time.Time
	LegalHold   bool
}

// Adapter implements object.Store over the S3-compatible HTTP protocol.
type Adapter struct {
	endpoint      *url.URL
	bucket        string
	client        HTTPDoer
	authorization string
	retry         RetryPolicy
}

var _ object.Store = (*Adapter)(nil)

// New validates and freezes provider configuration.
func New(cfg Config) (*Adapter, error) {
	endpoint, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || endpoint.User != nil {
		return nil, fmt.Errorf("%w: endpoint must be an absolute URL without user info", ErrInvalidConfig)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("%w: endpoint scheme must be http or https", ErrInvalidConfig)
	}
	if strings.TrimSpace(cfg.Bucket) == "" || strings.ContainsAny(cfg.Bucket, "/\\") {
		return nil, fmt.Errorf("%w: bucket must be a single path segment", ErrInvalidConfig)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	retry := cfg.Retry
	if retry.ReadMaxAttempts == 0 {
		retry.ReadMaxAttempts = 2
	}
	if retry.WriteMaxAttempts == 0 {
		retry.WriteMaxAttempts = 1
	}
	if retry.ReadMaxAttempts < 1 || retry.WriteMaxAttempts != 1 {
		return nil, fmt.Errorf("%w: reads need positive attempts and writes must be exactly one attempt", ErrInvalidConfig)
	}
	return &Adapter{endpoint: endpoint, bucket: cfg.Bucket, client: client, authorization: cfg.Authorization, retry: retry}, nil
}

// Put sends one conditional create.  A transport error is deliberately
// returned as-is: replaying it could hide an accepted provider write.
func (a *Adapter) Put(ctx context.Context, req object.PutRequest, opts PutOptions) (ProviderInfo, error) {
	if err := validatePut(req); err != nil {
		return ProviderInfo{}, err
	}
	want := contentDigest(req.Content)
	if req.Digest != "" && req.Digest != want {
		return ProviderInfo{}, fmt.Errorf("%w: want %s, got %s", object.ErrDigestMismatch, req.Digest, want)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPut, a.objectURL(req.ArtifactID), bytes.NewReader(req.Content))
	if err != nil {
		return ProviderInfo{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	r.Header.Set("If-None-Match", "*")
	r.Header.Set("Content-Type", req.MediaType)
	r.Header.Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(checksumBytes(req.Content)))
	r.Header.Set("x-amz-meta-hcmnext-digest", want)
	if req.ExpectedGeneration != nil {
		r.Header.Set("x-amz-meta-generation", strconv.FormatUint(*req.ExpectedGeneration, 10))
	}
	applyRetention(r, opts.Retention)
	a.authorize(r)
	response, err := a.client.Do(r)
	if err != nil {
		return ProviderInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ProviderInfo{}, a.mapError(response, req.ExpectedGeneration != nil)
	}
	info, err := providerInfo(response, req.ArtifactID, req.MediaType, int64(len(req.Content)))
	if err != nil {
		return ProviderInfo{}, err
	}
	info.Object.Digest = want
	if got := response.Header.Get("x-amz-checksum-sha256"); got != "" && got != base64.StdEncoding.EncodeToString(checksumBytes(req.Content)) {
		return ProviderInfo{}, fmt.Errorf("%w: put response checksum", ErrChecksumMismatch)
	}
	return info, nil
}

// PutIfAbsent implements object.Store's provider-neutral view.
func (a *Adapter) PutIfAbsent(ctx context.Context, req object.PutRequest) (object.Info, error) {
	info, err := a.Put(ctx, req, PutOptions{})
	return info.Object, err
}

// StatProvider returns exact provider metadata from HEAD.
func (a *Adapter) StatProvider(ctx context.Context, req object.Request) (ProviderInfo, error) {
	if err := validateRequest(req.ArtifactID); err != nil {
		return ProviderInfo{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodHead, a.objectURL(req.ArtifactID), nil)
	if err != nil {
		return ProviderInfo{}, err
	}
	a.authorize(r)
	response, err := a.client.Do(r)
	if err != nil {
		return ProviderInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ProviderInfo{}, a.mapError(response, false)
	}
	info, err := providerInfo(response, req.ArtifactID, response.Header.Get("Content-Type"), response.ContentLength)
	if err != nil {
		return ProviderInfo{}, err
	}
	if req.ExpectedGeneration != nil && info.Object.Generation != *req.ExpectedGeneration {
		return ProviderInfo{}, fmt.Errorf("%w: expected generation %d, got %d", object.ErrPrecondition, *req.ExpectedGeneration, info.Object.Generation)
	}
	return info, nil
}

func (a *Adapter) Stat(ctx context.Context, req object.Request) (object.Info, error) {
	info, err := a.StatProvider(ctx, req)
	return info.Object, err
}

// GetProvider reads a complete object and verifies an advertised checksum
// before returning it.  This bounds provider ambiguity at the adapter edge.
func (a *Adapter) GetProvider(ctx context.Context, req object.Request) (io.ReadCloser, ProviderInfo, error) {
	return a.get(ctx, req.ArtifactID, req.ExpectedGeneration, "")
}

func (a *Adapter) Get(ctx context.Context, req object.Request) (io.ReadCloser, object.Info, error) {
	body, info, err := a.GetProvider(ctx, req)
	return body, info.Object, err
}

func (a *Adapter) GetRangeProvider(ctx context.Context, req object.RangeRequest) (io.ReadCloser, ProviderInfo, error) {
	if req.Start < 0 || (req.End != nil && (*req.End <= req.Start)) {
		return nil, ProviderInfo{}, object.ErrRangeUnsupported
	}
	var value string
	if req.End == nil {
		value = fmt.Sprintf("bytes=%d-", req.Start)
	} else {
		value = fmt.Sprintf("bytes=%d-%d", req.Start, *req.End-1)
	}
	return a.get(ctx, req.ArtifactID, nil, value)
}

func (a *Adapter) GetRange(ctx context.Context, req object.RangeRequest) (io.ReadCloser, object.Info, error) {
	body, info, err := a.GetRangeProvider(ctx, req)
	return body, info.Object, err
}

func (a *Adapter) get(ctx context.Context, id string, expected *uint64, rangeValue string) (io.ReadCloser, ProviderInfo, error) {
	if err := validateRequest(id); err != nil {
		return nil, ProviderInfo{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, a.objectURL(id), nil)
	if err != nil {
		return nil, ProviderInfo{}, err
	}
	if rangeValue != "" {
		r.Header.Set("Range", rangeValue)
	}
	a.authorize(r)
	response, err := a.client.Do(r)
	if err != nil {
		return nil, ProviderInfo{}, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		return nil, ProviderInfo{}, a.mapError(response, false)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, ProviderInfo{}, err
	}
	info, err := providerInfo(response, id, response.Header.Get("Content-Type"), int64(len(data)))
	if err != nil {
		return nil, ProviderInfo{}, err
	}
	if rangeValue == "" && info.Object.Digest == "" {
		info.Object.Digest = contentDigest(data)
	}
	if expected != nil && info.Object.Generation != *expected {
		return nil, ProviderInfo{}, fmt.Errorf("%w: expected generation %d, got %d", object.ErrPrecondition, *expected, info.Object.Generation)
	}
	if rangeValue == "" {
		if advertised := response.Header.Get("x-amz-checksum-sha256"); advertised != "" && advertised != base64.StdEncoding.EncodeToString(checksumBytes(data)) {
			return nil, ProviderInfo{}, fmt.Errorf("%w: get response checksum", ErrChecksumMismatch)
		}
	}
	return io.NopCloser(bytes.NewReader(data)), info, nil
}

func (a *Adapter) Abort(ctx context.Context, req object.Request) error {
	if err := validateRequest(req.ArtifactID); err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.objectURL(req.ArtifactID), nil)
	if err != nil {
		return err
	}
	a.authorize(r)
	response, err := a.client.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return a.mapError(response, false)
	}
	return nil
}

func (a *Adapter) objectURL(id string) string {
	base := *a.endpoint
	base.Path = strings.TrimRight(base.Path, "/") + "/" + url.PathEscape(a.bucket) + "/" + url.PathEscape(id)
	return base.String()
}

func (a *Adapter) authorize(r *http.Request) {
	if a.authorization != "" {
		r.Header.Set("Authorization", a.authorization)
	}
}

func (a *Adapter) mapError(response *http.Response, expectedGeneration bool) error {
	switch response.StatusCode {
	case http.StatusNotFound:
		return object.ErrNotFound
	case http.StatusPreconditionFailed:
		if expectedGeneration {
			return object.ErrPrecondition
		}
		return object.ErrAlreadyExists
	case http.StatusConflict:
		return object.ErrAlreadyExists
	case http.StatusBadRequest:
		return object.ErrInvalidRequest
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: status %d", ErrProviderUnavailable, response.StatusCode)
	default:
		return fmt.Errorf("%w: status %d", ErrProviderResponse, response.StatusCode)
	}
}

func providerInfo(response *http.Response, id, mediaType string, size int64) (ProviderInfo, error) {
	generation := uint64(1)
	if raw := response.Header.Get("x-amz-meta-generation"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || parsed == 0 {
			return ProviderInfo{}, fmt.Errorf("%w: invalid generation", ErrProviderResponse)
		}
		generation = parsed
	}
	retainUntil := time.Time{}
	if raw := response.Header.Get("x-amz-object-lock-retain-until-date"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return ProviderInfo{}, fmt.Errorf("%w: invalid retention timestamp", ErrProviderResponse)
		}
		retainUntil = parsed.UTC()
	}
	mediaType = strings.TrimSpace(mediaType)
	checksum := response.Header.Get("x-amz-checksum-sha256")
	digest := response.Header.Get("x-amz-meta-hcmnext-digest")
	if !strings.HasPrefix(digest, "sha256:") {
		digest = ""
	}
	if decoded, err := base64.StdEncoding.DecodeString(checksum); err == nil && len(decoded) == sha256.Size {
		digest = "sha256:" + hex.EncodeToString(decoded)
	}
	return ProviderInfo{Object: object.Info{ArtifactID: id, Digest: digest, Size: size, MediaType: mediaType, Generation: generation}, VersionID: response.Header.Get("x-amz-version-id"), ETag: response.Header.Get("ETag"), ChecksumSHA: checksum, RetainUntil: retainUntil, LegalHold: strings.EqualFold(response.Header.Get("x-amz-object-lock-legal-hold"), "on")}, nil
}

func applyRetention(r *http.Request, retention Retention) {
	if !retention.RetainUntil.IsZero() {
		r.Header.Set("x-amz-object-lock-retain-until-date", retention.RetainUntil.UTC().Format(time.RFC3339))
	}
	if retention.LegalHold {
		r.Header.Set("x-amz-object-lock-legal-hold", "ON")
	}
}

func validatePut(req object.PutRequest) error {
	if err := validateRequest(req.ArtifactID); err != nil {
		return err
	}
	if len(req.Content) == 0 {
		return object.ErrInvalidRequest
	}
	return nil
}

func validateRequest(id string) error {
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, "/\\") || id == "." || id == ".." || strings.Contains(id, "..") {
		return object.ErrInvalidPath
	}
	return nil
}

func checksumBytes(content []byte) []byte {
	sum := sha256.Sum256(content)
	return sum[:]
}

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Qualification is the reviewed replacement/removal record for this
// adapter.  The repository intentionally does not add the AWS SDK to the
// production module graph; the wire-compatible adapter keeps the provider
// boundary replaceable and avoids SDK types in owned contracts.
type Qualification struct {
	ID               string
	AdapterVersion   string
	SDKModule        string
	SDKVersion       string
	Decision         string
	CredentialPolicy string
	RetryPolicy      string
	RemovalPolicy    string
}

func Qualify() Qualification {
	return Qualification{ID: "LIB-018", AdapterVersion: AdapterVersion, SDKModule: "github.com/aws/aws-sdk-go-v2", SDKVersion: "NOT_LINKED", Decision: "STANDARD_LIBRARY_S3_COMPATIBLE_TRANSPORT", CredentialPolicy: "opaque authorization header; secret-bearing provider config stays outside artifact contracts", RetryPolicy: "reads may be retried by the caller; writes are single-attempt and require recovery on ambiguity", RemovalPolicy: RemovalPolicy}
}

func (q Qualification) Explain() string {
	return fmt.Sprintf("%s adapter=%s decision=%s sdk=%s@%s retry=%s removal=%s", q.ID, q.AdapterVersion, q.Decision, q.SDKModule, q.SDKVersion, q.RetryPolicy, q.RemovalPolicy)
}

func Explain(q Qualification) string { return q.Explain() }
