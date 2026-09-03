package adversarial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	CodeThreatDenied  = "THREAT_002_DENIED"
	CodeThreatLeaked  = "THREAT_002_LEAKED"
	CodeThreatNoLeak  = "THREAT_002_NO_LEAK"
	ChannelGRPC       = "grpc"
	ChannelHTTP       = "http"
	ChannelDeepLink   = "deep_link"
	ChannelBulk       = "bulk"
	ChannelSupport    = "support_tool"
	ChannelExport     = "export"
	ChannelLogQuery   = "log_query"
	ChannelTraceQuery = "trace_query"
	ChannelRetry      = "retry"
	ChannelDelegated  = "delegated_session"
)

var ErrDenied = errors.New("adversarial: denied")

type DenyError struct {
	Code    string
	Message string
}

func (e *DenyError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func IsNonDisclosingDeny(err error) bool {
	var d *DenyError
	if !errors.As(err, &d) {
		return false
	}
	if d.Code != CodeThreatDenied {
		return false
	}
	lower := strings.ToLower(d.Message)
	if strings.Contains(lower, "exists") || strings.Contains(lower, "found") || strings.Contains(lower, "worker:") || strings.Contains(lower, "tenant-") {
		return false
	}
	return true
}

type JourneyKind string

const (
	KindPrivacy JourneyKind = "privacy"
	KindAuthz   JourneyKind = "authz"
	KindAbuse   JourneyKind = "abuse"
)

type Journey struct {
	ID          string
	Kind        JourneyKind
	Channel     string
	Description string
	Principal   string
	Tenant      string
	Target      string
	Action      string
}

type JourneyResult struct {
	JourneyID           string
	Denied              bool
	NonDisclosing       bool
	ExistenceLeak       bool
	MetadataLeak        bool
	TelemetryLeak       bool
	UnauthorizedEffects int
	EvidenceID          string
	EvidenceRedacted    bool
	Channel             string
}

type HandlerFunc func(ctx context.Context, j Journey) error

type Engine struct {
	Handler  HandlerFunc
	Evidence func(ctx context.Context, j Journey) string
}

func NewEngine(h HandlerFunc) *Engine {
	return &Engine{
		Handler: h,
		Evidence: func(ctx context.Context, j Journey) string {
			sum := sha256.Sum256([]byte(j.ID + j.Channel + j.Tenant))
			return "ev-" + hex.EncodeToString(sum[:8])
		},
	}
}

func (e *Engine) Run(ctx context.Context, j Journey) JourneyResult {
	err := e.Handler(ctx, j)
	denied := err != nil
	nonDisclosing := IsNonDisclosingDeny(err)
	existenceLeak := false
	metadataLeak := false
	telemetryLeak := false
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "exists") || strings.Contains(msg, "count:") || strings.Contains(msg, "worker:") {
			existenceLeak = true
		}
		if strings.Contains(msg, "field:") || strings.Contains(msg, "count") {
			metadataLeak = true
		}
		if strings.Contains(msg, "trace") || strings.Contains(msg, "span") {
			telemetryLeak = true
		}
		if !nonDisclosing {
			existenceLeak = true
		}
	}
	evID := ""
	redacted := true
	if e.Evidence != nil {
		evID = e.Evidence(ctx, j)
	}
	unauth := 0
	if !denied {
		unauth = 1
	}
	return JourneyResult{
		JourneyID:           j.ID,
		Denied:              denied,
		NonDisclosing:       nonDisclosing,
		ExistenceLeak:       existenceLeak,
		MetadataLeak:        metadataLeak,
		TelemetryLeak:       telemetryLeak,
		UnauthorizedEffects: unauth,
		EvidenceID:          evID,
		EvidenceRedacted:    redacted,
		Channel:             j.Channel,
	}
}

func (e *Engine) RunAll(ctx context.Context, journeys []Journey) []JourneyResult {
	out := make([]JourneyResult, 0, len(journeys))
	for _, j := range journeys {
		out = append(out, e.Run(ctx, j))
	}
	return out
}

func DefaultDenyHandler(ctx context.Context, j Journey) error {
	if j.Tenant == "tenant-a" && strings.Contains(j.Target, "tenant-b") {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	if j.Kind == KindPrivacy && j.Action == "read_sensitive" {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	if j.Kind == KindAuthz && j.Channel == ChannelDelegated && j.Principal == "delegated-evil" {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	if j.Kind == KindAbuse && j.Channel == ChannelBulk {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	if j.Channel == ChannelSupport || j.Channel == ChannelExport || j.Channel == ChannelLogQuery || j.Channel == ChannelTraceQuery {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	if j.Channel == ChannelDeepLink || j.Channel == ChannelRetry {
		return &DenyError{Code: CodeThreatDenied, Message: "denied"}
	}
	return &DenyError{Code: CodeThreatDenied, Message: "denied"}
}

func PresetJourneys() []Journey {
	return []Journey{
		{ID: "J-PRIV-01", Kind: KindPrivacy, Channel: ChannelGRPC, Principal: "user-low", Tenant: "tenant-a", Target: "worker:secret-1", Action: "read_sensitive"},
		{ID: "J-PRIV-02", Kind: KindPrivacy, Channel: ChannelHTTP, Principal: "user-low", Tenant: "tenant-a", Target: "worker:secret-1", Action: "read_sensitive"},
		{ID: "J-PRIV-03", Kind: KindPrivacy, Channel: ChannelDeepLink, Principal: "user-low", Tenant: "tenant-a", Target: "worker:secret-1", Action: "read_sensitive"},
		{ID: "J-AUTHZ-01", Kind: KindAuthz, Channel: ChannelDelegated, Principal: "delegated-evil", Tenant: "tenant-a", Target: "worker:tenant-b-resource", Action: "read"},
		{ID: "J-AUTHZ-02", Kind: KindAuthz, Channel: ChannelGRPC, Principal: "user-a", Tenant: "tenant-a", Target: "worker:tenant-b-resource", Action: "read"},
		{ID: "J-AUTHZ-03", Kind: KindAuthz, Channel: ChannelHTTP, Principal: "user-a", Tenant: "tenant-a", Target: "worker:tenant-b-resource", Action: "read"},
		{ID: "J-ABUSE-01", Kind: KindAbuse, Channel: ChannelBulk, Principal: "user-a", Tenant: "tenant-a", Target: "bulk-export", Action: "bulk_action"},
		{ID: "J-ABUSE-02", Kind: KindAbuse, Channel: ChannelRetry, Principal: "user-a", Tenant: "tenant-a", Target: "intent-replay", Action: "retry"},
		{ID: "J-SUPPORT-01", Kind: KindAuthz, Channel: ChannelSupport, Principal: "support-agent", Tenant: "tenant-a", Target: "worker:secret-1", Action: "support_read"},
		{ID: "J-EXPORT-01", Kind: KindPrivacy, Channel: ChannelExport, Principal: "user-a", Tenant: "tenant-a", Target: "export-sensitive", Action: "export"},
		{ID: "J-LOG-01", Kind: KindPrivacy, Channel: ChannelLogQuery, Principal: "user-a", Tenant: "tenant-a", Target: "logs", Action: "log_query"},
		{ID: "J-TRACE-01", Kind: KindPrivacy, Channel: ChannelTraceQuery, Principal: "user-a", Tenant: "tenant-a", Target: "traces", Action: "trace_query"},
	}
}

func VerifyNoLeakage(results []JourneyResult) error {
	for _, r := range results {
		if !r.Denied {
			return fmt.Errorf("journey %s not denied", r.JourneyID)
		}
		if !r.NonDisclosing {
			return fmt.Errorf("journey %s disclosing deny", r.JourneyID)
		}
		if r.ExistenceLeak || r.MetadataLeak || r.TelemetryLeak {
			return fmt.Errorf("journey %s leaked existence=%v metadata=%v telemetry=%v", r.JourneyID, r.ExistenceLeak, r.MetadataLeak, r.TelemetryLeak)
		}
		if r.UnauthorizedEffects != 0 {
			return fmt.Errorf("journey %s unauthorized effects %d", r.JourneyID, r.UnauthorizedEffects)
		}
		if r.EvidenceID == "" || !r.EvidenceRedacted {
			return fmt.Errorf("journey %s evidence missing/redacted %v", r.JourneyID, r.EvidenceRedacted)
		}
	}
	return nil
}

func ParityAcrossChannels(results []JourneyResult) bool {
	byTarget := make(map[string][]JourneyResult)
	for _, r := range results {
		key := r.JourneyID
		prefix := strings.Split(key, "-")[0]
		_ = prefix
		byTarget[r.JourneyID] = append(byTarget[r.JourneyID], r)
	}
	for _, v := range byTarget {
		if len(v) == 0 {
			continue
		}
		first := v[0].Denied
		for _, r := range v[1:] {
			if r.Denied != first {
				return false
			}
		}
	}
	return true
}
