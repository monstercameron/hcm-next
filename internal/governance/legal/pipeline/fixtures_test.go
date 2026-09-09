package pipeline

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// fixedSigner returns a deterministic ed25519 signer for reproducible golden
// tests: the seed is 32 fixed bytes, never crypto/rand. This mirrors
// internal/governance/legal's own test helper of the same name and the
// package's dev-fixture-key convention.
func fixedSigner(t *testing.T, seedByte byte) *legal.Signer {
	t.Helper()
	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	signer, err := legal.NewSigner(priv)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return signer
}

// waMinimumWageDefinitionJSON is LEGAL-015's Golden fixture: a real state-law
// example rather than a synthetic one, as the todo requires. It is Washington
// state's minimum-wage schedule under RCW 49.46.020 - $17.13/hour for 2026,
// annually CPI-W-adjusted - drawn from
// planning/research/state-employment-law/washington.md. It is defined here,
// as a Go byte literal, rather than as a checked-in file under
// definitions/legal/packs: this package's file roots exclude definitions/,
// and a pipeline test fixture is not a registered pack the extractor or the
// fifty-state matrix needs to know about.
func waMinimumWageDefinitionJSON() []byte {
	return []byte(`{
  "schema_version": 1,
  "pack_id": "us-wa-minimum-wage-schedule",
  "version": { "major": 1, "minor": 0 },
  "vocabulary_version": 2,
  "jurisdiction": {
    "country": "US",
    "subdivision": "WA",
    "locality_path": [],
    "level": "SUBDIVISION"
  },
  "window": { "start": "2026-01-01" },
  "source_type": "STATUTE",
  "review_status": "UNREVIEWED",
  "obligations": [
    {
      "kind": "WAGE_FLOOR",
      "id": "us-wa-minimum-wage-floor",
      "citation": {
        "source_file": "planning/research/state-employment-law/washington.md",
        "section": "RCW 49.46.020",
        "note": "Minimum wage $17.13/hour (2026); annual CPI-W adjustment; youth 14-15 at 85%; no regional multiplier.",
        "review_status": "UNREVIEWED",
        "confidence_marker": "CONFIRMED"
      },
      "body": {
        "floor_amount": { "amount": "17.13", "currency": "USD" },
        "worker_class": "ALL",
        "basis": "HOURLY",
        "indexation": "CPI",
        "next_adjustment_date": "2027-01-01",
        "standard": "REQUIRED"
      }
    }
  ],
  "preemption_assertions": [],
  "provenance": {
    "generator": "legal/pipeline test fixture",
    "source_files": ["planning/research/state-employment-law/washington.md"],
    "notes": "LEGAL-002/LEGAL-015 fixture pack: Washington state minimum-wage schedule."
  }
}
`)
}

// waMinimumWageDisputedDefinitionJSON is the same fixture with the wage
// floor's confidence marker set to VERIFY, for proving that a pipeline
// refuses to raise an uncertain rule to COUNSEL_APPROVED.
func waMinimumWageDisputedDefinitionJSON() []byte {
	return bytes.Replace(waMinimumWageDefinitionJSON(), []byte(`"confidence_marker": "CONFIRMED"`), []byte(`"confidence_marker": "VERIFY"`), 1)
}

// waMinimumWageSuccessorDefinitionJSON is the 2027 amendment to the same
// Washington minimum-wage schedule: the annual CPI-W adjustment the v1
// fixture's own next_adjustment_date names, raising the floor from
// $17.13/hour to the fixture's next-adjustment figure and declaring
// "supersedes" against v1, exactly as the contract's section 3.3 requires
// for a floor-amount change ("a typed body field value changes" is a major
// bump and a new release, never an edit).
func waMinimumWageSuccessorDefinitionJSON() []byte {
	return []byte(`{
  "schema_version": 1,
  "pack_id": "us-wa-minimum-wage-schedule",
  "version": { "major": 2, "minor": 0 },
  "vocabulary_version": 2,
  "jurisdiction": {
    "country": "US",
    "subdivision": "WA",
    "locality_path": [],
    "level": "SUBDIVISION"
  },
  "window": { "start": "2027-01-01" },
  "source_type": "STATUTE",
  "review_status": "UNREVIEWED",
  "obligations": [
    {
      "kind": "WAGE_FLOOR",
      "id": "us-wa-minimum-wage-floor",
      "citation": {
        "source_file": "planning/research/state-employment-law/washington.md",
        "section": "RCW 49.46.020",
        "note": "Annual CPI-W adjustment effective 2027-01-01, per RCW 49.46.020(8).",
        "review_status": "UNREVIEWED",
        "confidence_marker": "CONFIRMED"
      },
      "body": {
        "floor_amount": { "amount": "17.66", "currency": "USD" },
        "worker_class": "ALL",
        "basis": "HOURLY",
        "indexation": "CPI",
        "next_adjustment_date": "2028-01-01",
        "standard": "REQUIRED"
      }
    }
  ],
  "preemption_assertions": [],
  "supersedes": {
    "pack_id": "us-wa-minimum-wage-schedule",
    "version": { "major": 1, "minor": 0 },
    "jurisdiction": {
      "country": "US",
      "subdivision": "WA",
      "locality_path": [],
      "level": "SUBDIVISION"
    }
  },
  "provenance": {
    "generator": "legal/pipeline test fixture",
    "source_files": ["planning/research/state-employment-law/washington.md"],
    "notes": "LEGAL-002 supersession fixture: the 2027 CPI-W adjustment to the Washington minimum-wage schedule."
  }
}
`)
}
