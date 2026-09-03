package gateevidence

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ResultRecord is one checked-in or freshly-run test result.
type ResultRecord struct {
	Test             string `json:"test"`
	Package          string `json:"package"`
	Result           string `json:"result"`
	Timestamp        string `json:"timestamp"`
	Command          string `json:"command"`
	Environment      string `json:"environment"`
	ToolchainVersion string `json:"toolchain_version"`
	CommitOrBranch   string `json:"commit_or_branch"`
}

// dateLayout is the timestamp format ResultRecord.Timestamp uses: a bare
// date, matching the "(YYYY-MM-DD)" evidence labels planning/todos.md
// already uses (tools/planning/evidence.ParseEvidenceField).
const dateLayout = "2006-01-02"

// ParseTimestamp parses r.Timestamp as a bare date (UTC midnight).
func (r ResultRecord) ParseTimestamp() (time.Time, error) {
	return time.Parse(dateLayout, r.Timestamp)
}

// LoadResults reads the checked-in evidence-results JSON file: a plain JSON
// array of ResultRecord. A missing file is treated as an empty result set
// (every manifest evidence entry will then compile to MISSING).
func LoadResults(path string) ([]ResultRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var records []ResultRecord
	if err := json.Unmarshal(content, &records); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return records, nil
}
