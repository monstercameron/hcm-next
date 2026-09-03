package gateevidence

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// P1BTemplate is the schema for definitions/planning/gates/p1b-template.yaml
// (NEXT-002's "separately signed P1B template"): the six P1B candidate
// write/approval contracts, named only - never activatable from this
// document alone.
type P1BTemplate struct {
	SchemaVersion         int        `yaml:"schema_version" json:"schema_version"`
	Release               string     `yaml:"release" json:"release"`
	TodoID                string     `yaml:"todo_id" json:"todo_id"`
	Contracts             []Contract `yaml:"contracts" json:"contracts"`
	RequiresGateADecision bool       `yaml:"requires_gate_a_decision" json:"requires_gate_a_decision"`
	GateADecision         string     `yaml:"gate_a_decision" json:"gate_a_decision"`
	AuthorityDigest       string     `yaml:"authority_digest" json:"authority_digest"`
	Signature             *Signature `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// Contract is one P1B candidate write/approval contract.
type Contract struct {
	Order            int    `yaml:"order" json:"order"`
	ID               string `yaml:"id" json:"id"`
	Mode             string `yaml:"mode,omitempty" json:"mode,omitempty"`
	ActivationStatus string `yaml:"activation_status" json:"activation_status"`
}

// ActivationBlocked is the only ActivationStatus a P1B template contract may
// carry before a signed Gate A PROCEED decision exists.
const ActivationBlocked = "BLOCKED_PENDING_GATE_A"

// LoadP1BTemplate reads and parses a P1B template YAML file.
func LoadP1BTemplate(path string) (*P1BTemplate, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var t P1BTemplate
	if err := yaml.Unmarshal(content, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &t, nil
}

// CanActivate reports whether t could be activated: only true once a real
// Gate A PROCEED decision is recorded (GateADecision == "PROCEED") and a
// non-empty authority digest exists. This package populates neither field
// in the checked-in template; it only proves the template structurally
// cannot self-activate, matching next-steps.md P1B: "cannot activate
// without Gate A plus a new authority digest."
func (t P1BTemplate) CanActivate() bool {
	return t.GateADecision == "PROCEED" && strings.TrimSpace(t.AuthorityDigest) != ""
}

// Validate returns every structural violation on t.
func (t P1BTemplate) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if t.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if t.Release != "P1B" {
		add("release", fmt.Sprintf("must be P1B, got %q", t.Release))
	}
	if len(t.Contracts) == 0 {
		add("contracts", "missing")
	}
	if !t.RequiresGateADecision {
		add("requires_gate_a_decision", "must be true - a P1B template that does not require Gate A is not a template")
	}
	for _, c := range t.Contracts {
		if c.ActivationStatus != ActivationBlocked {
			add("contracts", fmt.Sprintf("%s: activation_status %q must be %s before Gate A", c.ID, c.ActivationStatus, ActivationBlocked))
		}
	}
	if t.CanActivate() {
		add("gate_a_decision/authority_digest", "template is activatable; a checked-in P1B template must never be")
	}

	return violations
}
