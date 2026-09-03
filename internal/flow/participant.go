package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
)

type RepresentationMode string

const (
	RepresentationSelf        RepresentationMode = "SELF"
	RepresentationOnBehalf    RepresentationMode = "ON_BEHALF_OF"
	RepresentationAssisted    RepresentationMode = "ASSISTED"
	RepresentationInterpreter RepresentationMode = "INTERPRETER"
	RepresentationSupport     RepresentationMode = "SUPPORT_VIEW"
)

type StageDef struct {
	ID                   string
	RequiredRelationship string
	RequiresDecision     bool
	RequiredAssurance    trust.Assurance
	RequiredPurpose      string
	AllowedActions       []string
	AllowedView          []string
	Expiry               time.Duration
}

type DelegationGrant struct {
	ID     string
	From   string
	To     string
	Scope  []string
	Expiry values.Instant
	Mode   RepresentationMode
}

type Governance interface {
	Relationship(principal, subject string, at values.Instant) (string, bool)
	Delegation(delegator, delegate, stage string, at values.Instant) (DelegationGrant, bool)
	HasDecisionRight(principal, stage string, at values.Instant) bool
	IsRecused(principal, flow string) bool
}

type ResolveRequest struct {
	FlowID         string
	Stage          StageDef
	Principal      *trust.Principal
	Subject        string
	Representation RepresentationMode
	OnBehalfOf     string
	InterpreterFor string
	Persona        string
	Route          string
	At             values.Instant
	Gov            Governance
}

type Evidence struct {
	FlowID         string
	StageID        string
	Participant    string
	Subject        string
	OnBehalfOf     string
	Representation RepresentationMode
	DelegationID   string
	Relationship   string
	DecisionRight  bool
	At             values.Instant
	Digest         string
}

type ResolveResult struct {
	Participant     string
	Subject         string
	Relationship    string
	Representation  RepresentationMode
	Delegation      *DelegationGrant
	Assurance       trust.Assurance
	Purpose         string
	AllowedView     []string
	AllowedActions  []string
	DecisionAllowed bool
	Expiry          values.Instant
	Denied          bool
	DenialReason    string
	Evidence        Evidence
}

func Resolve(req ResolveRequest) (ResolveResult, error) {
	if req.Principal == nil {
		return ResolveResult{Denied: true, DenialReason: "not_authorized"}, nil
	}
	if req.Gov == nil {
		return ResolveResult{Denied: true, DenialReason: "not_authorized"}, nil
	}
	if !req.At.IsSet() {
		return ResolveResult{}, fmt.Errorf("flow: at is unset")
	}
	if req.FlowID == "" || req.Stage.ID == "" {
		return ResolveResult{}, fmt.Errorf("flow: flow or stage empty")
	}
	participant := req.Principal.Subject()
	if participant == "" {
		return ResolveResult{Denied: true, DenialReason: "not_authorized"}, nil
	}
	subject := req.Subject
	if subject == "" {
		subject = participant
	}
	if req.Representation == RepresentationInterpreter && req.InterpreterFor != "" {
		subject = req.InterpreterFor
		if subject == participant {
			subject = req.Subject
			if subject == "" {
				subject = participant
			}
		}
	}
	if req.Gov.IsRecused(participant, req.FlowID) {
		ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, "", false, req.At)
		return ResolveResult{Participant: participant, Subject: subject, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
	}
	rel, ok := req.Gov.Relationship(participant, subject, req.At)
	if !ok {
		ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, "", false, req.At)
		return ResolveResult{Participant: participant, Subject: subject, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
	}
	if req.Stage.RequiredRelationship != "" && req.Stage.RequiredRelationship != "any" && rel != req.Stage.RequiredRelationship {
		ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
		return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
	}
	var del *DelegationGrant
	if req.Representation == RepresentationOnBehalf {
		if req.OnBehalfOf == "" || req.OnBehalfOf == participant {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
		g, found := req.Gov.Delegation(req.OnBehalfOf, participant, req.Stage.ID, req.At)
		if !found {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
		if !g.Expiry.IsSet() || !req.At.Before(g.Expiry) {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
		if !isSubset(g.Scope, req.Stage.AllowedActions, req.Stage.AllowedView) {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
		if isFullScope(g.Scope, req.Stage.AllowedActions, req.Stage.AllowedView) {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
		tmp := g
		del = &tmp
		subject = req.OnBehalfOf
		rel2, ok2 := req.Gov.Relationship(req.OnBehalfOf, req.Subject, req.At)
		if ok2 {
			rel = rel2
		}
	}
	if req.Stage.RequiredAssurance != trust.AssuranceUnspecified {
		if !req.Principal.Assurance().AtLeast(req.Stage.RequiredAssurance) {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			if del != nil {
				ev.DelegationID = del.ID
			}
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Delegation: del, Assurance: req.Principal.Assurance(), Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
	}
	if req.Stage.RequiredPurpose != "" {
		if !req.Principal.AuthorizesPurpose(req.Stage.RequiredPurpose) {
			ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, false, req.At)
			if del != nil {
				ev.DelegationID = del.ID
			}
			return ResolveResult{Participant: participant, Subject: subject, Relationship: rel, Representation: req.Representation, Delegation: del, Assurance: req.Principal.Assurance(), Denied: true, DenialReason: "not_authorized", Evidence: ev}, nil
		}
	}
	decisionAllowed := false
	if req.Stage.RequiresDecision && req.Representation != RepresentationSupport {
		if req.Gov.HasDecisionRight(participant, req.Stage.ID, req.At) {
			if del != nil {
				has := false
				for _, s := range del.Scope {
					if s == "decide" || s == "approve" {
						has = true
						break
					}
				}
				if has {
					decisionAllowed = true
				}
			} else {
				decisionAllowed = true
			}
		}
	}
	purpose := req.Stage.RequiredPurpose
	if purpose == "" {
		purpose = req.Principal.DefaultPurpose()
	}
	expiry := values.NewInstant(req.At.Time().Add(req.Stage.Expiry))
	if req.Stage.Expiry == 0 {
		expiry = values.NewInstant(req.At.Time().Add(30 * time.Minute))
	}
	allowedView := append([]string(nil), req.Stage.AllowedView...)
	allowedActions := append([]string(nil), req.Stage.AllowedActions...)
	sort.Strings(allowedView)
	sort.Strings(allowedActions)
	ev := buildEvidence(req.FlowID, req.Stage.ID, participant, subject, req.Representation, req.OnBehalfOf, rel, decisionAllowed, req.At)
	if del != nil {
		ev.DelegationID = del.ID
		ev.OnBehalfOf = req.OnBehalfOf
	}
	return ResolveResult{
		Participant:     participant,
		Subject:         subject,
		Relationship:    rel,
		Representation:  req.Representation,
		Delegation:      del,
		Assurance:       req.Principal.Assurance(),
		Purpose:         purpose,
		AllowedView:     allowedView,
		AllowedActions:  allowedActions,
		DecisionAllowed: decisionAllowed,
		Expiry:          expiry,
		Denied:          false,
		Evidence:        ev,
	}, nil
}

func isSubset(scope, actions, view []string) bool {
	allowed := make(map[string]bool)
	for _, a := range actions {
		allowed[a] = true
	}
	for _, v := range view {
		allowed[v] = true
	}
	allowed["decide"] = allowed["decide"] || len(actions) > 0
	allowed["approve"] = allowed["approve"] || len(actions) > 0
	if len(scope) == 0 {
		return false
	}
	for _, s := range scope {
		if !allowed[s] {
			return false
		}
	}
	return true
}

func isFullScope(scope, actions, view []string) bool {
	allowed := make(map[string]bool)
	for _, a := range actions {
		allowed[a] = true
	}
	for _, v := range view {
		allowed[v] = true
	}
	if len(scope) < len(allowed) {
		return false
	}
	for k := range allowed {
		found := false
		for _, s := range scope {
			if s == k {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func buildEvidence(flowID, stageID, participant, subject string, rep RepresentationMode, onBehalf, rel string, decision bool, at values.Instant) Evidence {
	h := sha256.New()
	fmt.Fprintf(h, "flow=%d:%s;stage=%d:%s;participant=%d:%s;subject=%d:%s;rep=%d:%s;onbehalf=%d:%s;rel=%d:%s;decision=%t;at=%d:%s;",
		len(flowID), flowID, len(stageID), stageID, len(participant), participant, len(subject), subject, len(string(rep)), string(rep), len(onBehalf), onBehalf, len(rel), rel, decision, len(at.String()), at.String())
	d := hex.EncodeToString(h.Sum(nil))
	return Evidence{FlowID: flowID, StageID: stageID, Participant: participant, Subject: subject, OnBehalfOf: onBehalf, Representation: rep, Relationship: rel, DecisionRight: decision, At: at, Digest: d}
}
