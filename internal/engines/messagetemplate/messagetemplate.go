// Package messagetemplate publishes and renders deterministic, purpose-bound
// message templates without knowing anything about delivery providers.
package messagetemplate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Purpose string
type Channel string

const (
	PurposePromotion   Purpose = "PROMOTION"
	PurposeLeave       Purpose = "LEAVE"
	PurposeApproval    Purpose = "APPROVAL"
	PurposeTask        Purpose = "TASK"
	PurposeNotice      Purpose = "NOTICE"
	PurposeLegalNotice Purpose = "LEGAL_NOTICE"
	PurposeWorkflow    Purpose = "WORKFLOW_UPDATE"
	Promotion          Purpose = PurposePromotion
	Leave              Purpose = PurposeLeave

	ChannelEmail       Channel = "EMAIL"
	ChannelInbox       Channel = "INBOX"
	ChannelSecureInbox Channel = "SECURE_INBOX"
	Email              Channel = ChannelEmail
	Inbox              Channel = ChannelInbox
	SecureInbox        Channel = ChannelSecureInbox
)

var (
	ErrInvalidTemplate    = errors.New("messagetemplate: invalid template")
	ErrUnknownPlaceholder = errors.New("messagetemplate: unknown placeholder")
	ErrMissingParameter   = errors.New("messagetemplate: missing parameter")
	ErrUnsafeContent      = errors.New("messagetemplate: unsafe content")
	ErrChannelNotAllowed  = errors.New("messagetemplate: channel is not allowed for purpose")
	ErrAlreadyPublished   = errors.New("messagetemplate: template version already published")
	ErrNotFound           = errors.New("messagetemplate: template not found")
)

// PlaceholderVocabulary is deliberately closed. Adding a placeholder is a
// contract change, not an arbitrary caller-controlled field name.
var PlaceholderVocabulary = map[string]bool{
	"recipient_name": true, "worker_name": true, "organization_name": true,
	"promotion_title": true, "effective_date": true, "leave_start_date": true,
	"leave_end_date": true, "action_url": true, "case_reference": true,
	"support_contact": true, "deadline": true, "message": true,
	"decision": true, "status": true, "workflow_name": true,
}

var purposeChannels = map[Purpose]map[Channel]bool{
	PurposePromotion:   {ChannelEmail: true, ChannelInbox: true, ChannelSecureInbox: true},
	PurposeLeave:       {ChannelEmail: true, ChannelInbox: true, ChannelSecureInbox: true},
	PurposeApproval:    {ChannelEmail: true, ChannelInbox: true, ChannelSecureInbox: true},
	PurposeTask:        {ChannelInbox: true, ChannelSecureInbox: true},
	PurposeNotice:      {ChannelEmail: true, ChannelInbox: true, ChannelSecureInbox: true},
	PurposeLegalNotice: {ChannelEmail: true, ChannelInbox: true, ChannelSecureInbox: true},
	PurposeWorkflow:    {ChannelInbox: true, ChannelSecureInbox: true},
}

type Template struct {
	Key                  string   `json:"key,omitempty"`
	Version              int      `json:"version,omitempty"`
	TemplateKey          string   `json:"template_key,omitempty"`
	TemplateVersion      int      `json:"template_version,omitempty"`
	Purpose              Purpose  `json:"purpose"`
	Channel              Channel  `json:"channel"`
	Locale               string   `json:"locale"`
	Classification       string   `json:"classification"`
	LegalBasis           string   `json:"legal_basis"`
	Subject              string   `json:"subject"`
	Body                 string   `json:"body"`
	Placeholders         []string `json:"placeholders"`
	DeclaredPlaceholders []string `json:"declared_placeholders,omitempty"`
}

func (t Template) normalized() Template {
	if t.Key == "" {
		t.Key = t.TemplateKey
	}
	if t.Version == 0 {
		t.Version = t.TemplateVersion
	}
	if len(t.Placeholders) == 0 {
		t.Placeholders = append([]string(nil), t.DeclaredPlaceholders...)
	}
	sort.Strings(t.Placeholders)
	return t
}

func (t Template) Validate() error {
	t = t.normalized()
	if t.Key == "" || t.Version < 1 || t.Purpose == "" || t.Channel == "" || t.Locale == "" || t.Classification == "" || t.LegalBasis == "" || t.Body == "" {
		return fmt.Errorf("%w: key/version/purpose/channel/locale/classification/legal basis/body are required", ErrInvalidTemplate)
	}
	if !purposeChannels[t.Purpose][t.Channel] {
		return fmt.Errorf("%w: %s/%s", ErrChannelNotAllowed, t.Purpose, t.Channel)
	}
	declared := make(map[string]bool, len(t.Placeholders))
	for _, name := range t.Placeholders {
		if !PlaceholderVocabulary[name] {
			return fmt.Errorf("%w: %s", ErrUnknownPlaceholder, name)
		}
		if declared[name] {
			return fmt.Errorf("%w: duplicate declaration %s", ErrInvalidTemplate, name)
		}
		declared[name] = true
	}
	for _, text := range []string{t.Subject, t.Body} {
		if strings.ContainsAny(text, "<>") {
			return fmt.Errorf("%w: template contains HTML markup", ErrUnsafeContent)
		}
		for _, name := range placeholders(text) {
			if !PlaceholderVocabulary[name] {
				return fmt.Errorf("%w: %s", ErrUnknownPlaceholder, name)
			}
			if !declared[name] {
				return fmt.Errorf("%w: %s is not declared", ErrUnknownPlaceholder, name)
			}
		}
	}
	return nil
}

type RenderRequest struct {
	Purpose        Purpose
	Channel        Channel
	Locale         string
	Classification string
	LegalBasis     string
	Parameters     map[string]string
	Values         map[string]string
	PersonalData   map[string]string
}

type Rendered struct {
	Key            string  `json:"key"`
	Version        int     `json:"version"`
	Purpose        Purpose `json:"purpose"`
	Channel        Channel `json:"channel"`
	Locale         string  `json:"locale"`
	Classification string  `json:"classification"`
	Subject        string  `json:"subject"`
	Body           string  `json:"body"`
	Digest         string  `json:"digest"`
}

func (t Template) Render(req RenderRequest) (Rendered, error) {
	t = t.normalized()
	if err := t.Validate(); err != nil {
		return Rendered{}, err
	}
	if req.Purpose == "" || req.Channel == "" || req.Locale == "" || req.Classification == "" || req.LegalBasis == "" {
		return Rendered{}, fmt.Errorf("%w: render purpose/channel/locale/classification/legal basis are required", ErrInvalidTemplate)
	}
	if req.Purpose != t.Purpose || req.Channel != t.Channel || req.Locale != t.Locale || req.Classification != t.Classification || req.LegalBasis != t.LegalBasis {
		return Rendered{}, fmt.Errorf("%w: render context differs from published template", ErrInvalidTemplate)
	}
	params, err := mergeParameters(req.Parameters, req.Values)
	if err != nil {
		return Rendered{}, err
	}
	declared := make(map[string]bool, len(t.Placeholders))
	for _, name := range t.Placeholders {
		declared[name] = true
	}
	for name := range params {
		if !declared[name] || !PlaceholderVocabulary[name] {
			return Rendered{}, fmt.Errorf("%w: %s", ErrUnknownPlaceholder, name)
		}
		if strings.ContainsAny(params[name], "<>") {
			return Rendered{}, fmt.Errorf("%w: parameter %s contains markup", ErrUnsafeContent, name)
		}
	}
	for name, value := range req.PersonalData {
		if !declared[name] || params[name] != value {
			return Rendered{}, fmt.Errorf("%w: personal data %s is outside its declared placeholder", ErrUnsafeContent, name)
		}
		if strings.Contains(t.Subject, value) || strings.Contains(t.Body, value) {
			return Rendered{}, fmt.Errorf("%w: personal data is embedded literally", ErrUnsafeContent)
		}
	}
	for _, name := range placeholders(t.Subject + "\x00" + t.Body) {
		if _, ok := params[name]; !ok {
			return Rendered{}, fmt.Errorf("%w: %s", ErrMissingParameter, name)
		}
	}
	subject := substitute(t.Subject, params)
	body := substitute(t.Body, params)
	out := Rendered{Key: t.Key, Version: t.Version, Purpose: t.Purpose, Channel: t.Channel, Locale: t.Locale, Classification: t.Classification, Subject: subject, Body: body}
	out.Digest = renderedDigest(out)
	return out, nil
}

func mergeParameters(a, b map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if old, ok := out[k]; ok && old != v {
			return nil, fmt.Errorf("%w: conflicting value %s", ErrInvalidTemplate, k)
		}
		out[k] = v
	}
	return out, nil
}

func placeholders(text string) []string {
	var out []string
	for i := 0; i < len(text); {
		start := strings.Index(text[i:], "{{")
		if start < 0 {
			break
		}
		start += i
		end := strings.Index(text[start+2:], "}}")
		if end < 0 {
			out = append(out, text[start+2:])
			break
		}
		end += start + 2
		name := strings.TrimSpace(text[start+2 : end])
		out = append(out, name)
		i = end + 2
	}
	return out
}

func substitute(text string, params map[string]string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		start := strings.Index(text[i:], "{{")
		if start < 0 {
			out.WriteString(text[i:])
			break
		}
		start += i
		out.WriteString(text[i:start])
		end := strings.Index(text[start+2:], "}}")
		if end < 0 {
			out.WriteString(text[start:])
			break
		}
		end += start + 2
		name := strings.TrimSpace(text[start+2 : end])
		out.WriteString(params[name])
		i = end + 2
	}
	return out.String()
}

func renderedDigest(r Rendered) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Registry is an in-memory publication port keyed by template key and version.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]Template
}

func NewRegistry() *Registry { return &Registry{templates: make(map[string]Template)} }

func (r *Registry) Publish(t Template) error {
	if r == nil {
		return ErrInvalidTemplate
	}
	t = t.normalized()
	if err := t.Validate(); err != nil {
		return err
	}
	id := fmt.Sprintf("%s@%d", t.Key, t.Version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.templates[id]; ok {
		return ErrAlreadyPublished
	}
	t.Placeholders = append([]string(nil), t.Placeholders...)
	r.templates[id] = t
	return nil
}

func (r *Registry) Render(key string, version int, req RenderRequest) (Rendered, error) {
	r.mu.RLock()
	t, ok := r.templates[fmt.Sprintf("%s@%d", key, version)]
	r.mu.RUnlock()
	if !ok {
		return Rendered{}, ErrNotFound
	}
	return t.Render(req)
}

func Render(t Template, req RenderRequest) (Rendered, error) { return t.Render(req) }
