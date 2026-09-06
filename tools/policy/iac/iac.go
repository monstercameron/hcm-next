// Package iac owns the provider-neutral CI admission contract for the
// infrastructure-as-code gate. It is deliberately a pure policy package:
// callers provide the proposed resources, plan bytes, exceptions, and the
// evaluation time; no database, cloud SDK, or mutable global is involved.
package iac

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

// Version reports the IAC-003 policy contract version.
func Version() int { return contractVersion }

// Explain describes the policy contract for operator and CI output.
func Explain() string {
	return "IAC-003 v1: fail-closed resource, plan, and reviewed-exception admission"
}

// Resource is the provider-neutral security and operations surface CI admits.
type Resource struct {
	ID                     string
	Kind                   string
	Public                 bool
	Egress                 []string
	Image                  string
	ImageDigest            string
	ImageSignatureVerified bool
	Encryption             bool
	Backup                 bool
	Tags                   map[string]string
}

// PlanChange describes one proposed infrastructure change.
type PlanChange struct {
	ResourceID string
	Action     string
}

// Exception is a narrow, expiring approval for one otherwise-blocked
// finding. An exception never applies by wildcard or to another resource.
type Exception struct {
	ID         string
	ResourceID string
	Code       string
	Owner      string
	Rationale  string
	ExpiresAt  time.Time
}

// Input is the complete deterministic input to the CI gate.
type Input struct {
	Resources    []Resource
	Changes      []PlanChange
	Exceptions   []Exception
	ExpectedPlan []byte
	ActualPlan   []byte
	EvaluatedAt  time.Time
}

// Finding is one fail-closed admission finding.
type Finding struct {
	Code       string
	ResourceID string
	Detail     string
}

// Report is the deterministic result of evaluating an Input.
type Report struct {
	Findings       []Finding
	Applied        []string
	ExpectedDigest string
	ActualDigest   string
}

// OK reports whether the plan is admissible.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Validate evaluates the IAC-003 contract at Input.EvaluatedAt. A zero time
// is accepted for fixture use and means that only structural expiry checks
// are applied; production callers should always provide an evaluation time.
func Validate(in Input) Report {
	report := Report{}
	if len(in.ExpectedPlan) != 0 {
		report.ExpectedDigest = Digest(in.ExpectedPlan)
	}
	if len(in.ActualPlan) != 0 {
		report.ActualDigest = Digest(in.ActualPlan)
		if !bytes.Equal(in.ExpectedPlan, in.ActualPlan) {
			report.Findings = append(report.Findings, Finding{Code: "PLAN_DIFF", Detail: "actual plan does not match the reviewed golden plan"})
		}
	}

	byKey := make(map[string]Exception, len(in.Exceptions))
	for _, exception := range in.Exceptions {
		if exception.ID == "" || exception.ResourceID == "" || exception.Code == "" || strings.TrimSpace(exception.Owner) == "" || strings.TrimSpace(exception.Rationale) == "" || exception.ExpiresAt.IsZero() {
			report.Findings = append(report.Findings, Finding{Code: "INVALID_EXCEPTION", ResourceID: exception.ResourceID, Detail: "exception requires id, resource, code, owner, rationale, and expiry"})
			continue
		}
		key := exception.ResourceID + "\x00" + exception.Code
		if _, exists := byKey[key]; exists {
			report.Findings = append(report.Findings, Finding{Code: "DUPLICATE_EXCEPTION", ResourceID: exception.ResourceID, Detail: "more than one exception targets the same finding"})
			continue
		}
		if !in.EvaluatedAt.IsZero() && !exception.ExpiresAt.After(in.EvaluatedAt) {
			report.Findings = append(report.Findings, Finding{Code: "EXPIRED_EXCEPTION", ResourceID: exception.ResourceID, Detail: "exception expiry is not after evaluation time"})
			continue
		}
		byKey[key] = exception
	}

	for _, resource := range in.Resources {
		for _, finding := range resourceFindings(resource) {
			finding.ResourceID = resource.ID
			key := resource.ID + "\x00" + finding.Code
			if exception, ok := byKey[key]; ok {
				report.Applied = append(report.Applied, exception.ID)
				continue
			}
			report.Findings = append(report.Findings, finding)
		}
	}
	for _, change := range in.Changes {
		if strings.EqualFold(strings.TrimSpace(change.Action), "destroy") || strings.EqualFold(strings.TrimSpace(change.Action), "replace") {
			finding := Finding{Code: "DESTRUCTIVE_CHANGE", ResourceID: change.ResourceID, Detail: "destructive change requires a reviewed, expiring exception"}
			if _, ok := byKey[change.ResourceID+"\x00"+finding.Code]; !ok {
				report.Findings = append(report.Findings, finding)
			} else {
				for _, exception := range in.Exceptions {
					if exception.ResourceID == change.ResourceID && exception.Code == finding.Code {
						report.Applied = append(report.Applied, exception.ID)
						break
					}
				}
			}
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].ResourceID != report.Findings[j].ResourceID {
			return report.Findings[i].ResourceID < report.Findings[j].ResourceID
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	sort.Strings(report.Applied)
	return report
}

// Check returns a stable CI error for the first finding.
func Check(in Input) error {
	report := Validate(in)
	if !report.OK() {
		finding := report.Findings[0]
		return fmt.Errorf("iac: %s resource=%s: %s", finding.Code, finding.ResourceID, finding.Detail)
	}
	return nil
}

func resourceFindings(resource Resource) []Finding {
	var out []Finding
	if strings.TrimSpace(resource.ID) == "" {
		out = append(out, Finding{Code: "MISSING_ID", Detail: "resource id is required"})
	}
	if strings.EqualFold(strings.TrimSpace(resource.Kind), "database") && resource.Public {
		out = append(out, Finding{Code: "PUBLIC_DATABASE", Detail: "database resources must not be public"})
	}
	for _, destination := range resource.Egress {
		if isWildcard(destination) {
			out = append(out, Finding{Code: "WILDCARD_EGRESS", Detail: "egress destinations must be explicit; wildcard access is forbidden"})
			break
		}
	}
	if strings.EqualFold(strings.TrimSpace(resource.Kind), "workload") {
		if !strings.HasPrefix(resource.ImageDigest, "sha256:") || len(strings.TrimPrefix(resource.ImageDigest, "sha256:")) != 64 {
			out = append(out, Finding{Code: "MUTABLE_IMAGE", Detail: "workload image must use a sha256 digest"})
		}
		if !resource.ImageSignatureVerified {
			out = append(out, Finding{Code: "UNVERIFIED_IMAGE", Detail: "workload image digest must be signature-verified"})
		}
	}
	if !resource.Encryption {
		out = append(out, Finding{Code: "MISSING_ENCRYPTION", Detail: "resource must declare encryption"})
	}
	if !resource.Backup {
		out = append(out, Finding{Code: "MISSING_BACKUP", Detail: "resource must declare backup"})
	}
	for _, tag := range []string{"owner", "environment", "data_classification"} {
		if strings.TrimSpace(resource.Tags[tag]) == "" {
			out = append(out, Finding{Code: "MISSING_TAGS", Detail: "resource is missing required tag " + tag})
			break
		}
	}
	return out
}

func isWildcard(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "*", "0.0.0.0/0", "::/0", "any", "internet":
		return true
	default:
		return false
	}
}

// Digest returns a content-addressed identity for plan bytes.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
