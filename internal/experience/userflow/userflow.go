// Package userflow owns the machine-readable experience contract shared by
// flow documentation, conformance tooling, and generated views.
package userflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Stage string

const (
	Discover  Stage = "DISCOVER"
	Orient    Stage = "ORIENT"
	Collect   Stage = "COLLECT"
	Validate  Stage = "VALIDATE"
	Simulate  Stage = "SIMULATE"
	Compare   Stage = "COMPARE"
	Confirm   Stage = "CONFIRM"
	Submit    Stage = "SUBMIT"
	Review    Stage = "REVIEW"
	Wait      Stage = "WAIT"
	Track     Stage = "TRACK"
	Replan    Stage = "REPLAN"
	Execute   Stage = "EXECUTE"
	Reconcile Stage = "RECONCILE"
	Repair    Stage = "REPAIR"
	Complete  Stage = "COMPLETE"
	Correct   Stage = "CORRECT"
	Appeal    Stage = "APPEAL"
)

var stageVocabulary = []Stage{Discover, Orient, Collect, Validate, Simulate, Compare, Confirm, Submit, Review, Wait, Track, Replan, Execute, Reconcile, Repair, Complete, Correct, Appeal}

func (s Stage) Valid() bool {
	for _, v := range stageVocabulary {
		if s == v {
			return true
		}
	}
	return false
}
func Stages() []Stage { return append([]Stage(nil), stageVocabulary...) }

type Participant struct {
	ID, Role, Relationship, DecisionRights string
	Representation, Delegation             string
}
type SemanticReferences struct{ BusinessIntent, Workflow, VerticalSlice, Action, Result string }
type EntryPaths struct{ Entry, Discovery, Resume, DeepLink, Notification string }
type StatePresentation struct{ State, Understand, Behavior string }
type FlowStage struct {
	ID                                                                                        string
	Stage                                                                                     Stage
	ParticipantGoal, Surface, SystemState, AvailableActions                                   string
	Input, Validation, CapabilityTransition, VisibleResult, Evidence, Decision, ErrorRecovery string
}
type NotApplicable struct{ Reason, Owner string }

// UserFlowRecord is the complete, semantic description of one participant
// journey. Strings are intentionally explicit: an empty value means the
// contract is incomplete, while NotApplicable records an owned exception.
type UserFlowRecord struct {
	FlowID, Version, Status, Owner                                                  string
	Title, JobToBeDone, SuccessDefinition                                           string
	RootBusinessIntent, ChildBusinessIntents                                        string
	References                                                                      SemanticReferences
	PrimaryParticipant                                                              Participant
	OtherParticipants                                                               []Participant
	IdentityAssurance, SessionAssumptions                                           string
	Entry                                                                           EntryPaths
	Surfaces, Devices                                                               []string
	Preconditions, UnavailableAction                                                string
	VisibleFacts, Provenance, Freshness, MaskedFacts, HiddenFacts, SummaryOnlyFacts string
	RequestedInput, ServerResolvedTruth, Forms, Documents, EvidenceCompartments     string
	Stages                                                                          []FlowStage
	StateMatrix                                                                     []StatePresentation
	Cancellation, Correction, Supersession, Completion, FollowUp                    string
	Locale, Accessibility, Accommodation, Responsive, Offline, Privacy, Safety      string
	AnalyticsEvents, ProhibitedTelemetry                                            string
	Scenarios, Oracles, Evidence, TodoLinks, OpenFindings                           []string
	Phase, MaximalConfiguration                                                     string
	NotApplicable                                                                   map[string]NotApplicable
}

func required(s string) bool { return strings.TrimSpace(s) != "" }
func (r UserFlowRecord) Validate() error {
	checks := map[string]string{"flow_id": r.FlowID, "version": r.Version, "status": r.Status, "owner": r.Owner, "title": r.Title, "job_to_be_done": r.JobToBeDone, "success_definition": r.SuccessDefinition, "root_business_intent": r.RootBusinessIntent, "workflow": r.References.Workflow, "vertical_slice": r.References.VerticalSlice, "primary_participant": r.PrimaryParticipant.ID, "identity_assurance": r.IdentityAssurance, "session_assumptions": r.SessionAssumptions, "entry": r.Entry.Entry, "discovery": r.Entry.Discovery, "resume": r.Entry.Resume, "requested_input": r.RequestedInput, "server_resolved_truth": r.ServerResolvedTruth, "locale": r.Locale, "accessibility": r.Accessibility, "privacy": r.Privacy, "phase": r.Phase, "maximal_configuration": r.MaximalConfiguration}
	for field, value := range checks {
		if !required(value) && r.NotApplicable[field].Reason == "" {
			return fmt.Errorf("user flow %s: missing %s", r.FlowID, field)
		}
	}
	if len(r.OtherParticipants) == 0 {
		return errors.New("user flow: other participants are required")
	}
	if len(r.Surfaces) == 0 || len(r.Devices) == 0 {
		return errors.New("user flow: surfaces and devices are required")
	}
	if len(r.Stages) == 0 {
		return errors.New("user flow: stages are required")
	}
	seen := map[string]bool{}
	for i, s := range r.Stages {
		if s.ID == "" || seen[s.ID] {
			return fmt.Errorf("user flow: invalid stage %d", i)
		}
		seen[s.ID] = true
		if !s.Stage.Valid() {
			return fmt.Errorf("user flow: unknown stage %q", s.Stage)
		}
		for n, v := range map[string]string{"participant_goal": s.ParticipantGoal, "surface": s.Surface, "system_state": s.SystemState, "available_actions": s.AvailableActions, "input": s.Input, "validation": s.Validation, "capability_transition": s.CapabilityTransition, "visible_result": s.VisibleResult, "evidence": s.Evidence, "error_recovery": s.ErrorRecovery} {
			if !required(v) {
				return fmt.Errorf("user flow stage %s: missing %s", s.ID, n)
			}
		}
	}
	if len(r.StateMatrix) == 0 {
		return errors.New("user flow: state matrix is required")
	}
	for _, s := range r.StateMatrix {
		if !required(s.State) || !required(s.Understand) || !required(s.Behavior) {
			return errors.New("user flow: incomplete state matrix")
		}
	}
	for field, values := range map[string][]string{"scenarios": r.Scenarios, "oracles": r.Oracles, "todo_links": r.TodoLinks, "evidence": r.Evidence} {
		if len(values) == 0 && r.NotApplicable[field].Reason == "" {
			return fmt.Errorf("user flow: missing %s", field)
		}
	}
	return nil
}
func CanonicalBytes(r UserFlowRecord) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}
func (r UserFlowRecord) CanonicalDigest() (string, error) {
	b, e := CanonicalBytes(r)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
