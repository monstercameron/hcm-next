package legal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Entitlement authorities: only statutory protection is mandatory.
const (
	AuthorityStatutory  = "statutory"
	AuthorityCollective = "collective"
	AuthorityCompany    = "company"
)

// Entitlement states: the closed composition vocabulary.
const (
	EntitlementValid          = "VALID"
	EntitlementConflict       = "CONFLICT"
	EntitlementReviewRequired = "REVIEW_REQUIRED"
	EntitlementUnknown        = "UNKNOWN"
)

// Interaction strategies: how one program combines with the rest.
const (
	InteractOverlap        = "overlap"
	InteractConcurrent     = "concurrent"
	InteractOffset         = "offset"
	InteractStack          = "stack"
	InteractMostProtective = "most-protective"
)

// EntitlementProgram is one statutory, collective or company leave program.
type EntitlementProgram struct {
	ID            string
	Authority     string
	Release       string
	Eligible      bool
	EvidenceKnown bool
	Weeks         int
	PaidWeeks     int
	Interaction   string
	Evidence      []string
	Notices       []string
	Effects       []string
}

// Composition is the immutable entitlement composition with its trace.
type Composition struct {
	Programs       []EntitlementProgram
	State          string
	ComposedWeeks  int
	ComposedPaid   int
	AccruedBalance int
	OnLeave        bool
	JobChange      string
	Trace          []string
	Digest         string
}

func compositionDigest(programs []EntitlementProgram, state string, weeks, paid, balance int, onLeave bool, jobChange string) string {
	ordered := append([]EntitlementProgram(nil), programs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	parts := []string{"legal-entitlement", state, fmt.Sprint(weeks, paid, balance, onLeave, jobChange)}
	for _, program := range ordered {
		parts = append(parts, strings.Join([]string{program.ID, program.Authority, program.Release, fmt.Sprint(program.Eligible, program.EvidenceKnown, program.Weeks, program.PaidWeeks), program.Interaction}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validAuthority(authority string) bool {
	return authority == AuthorityStatutory || authority == AuthorityCollective || authority == AuthorityCompany
}

func validInteraction(interaction string) bool {
	switch interaction {
	case InteractOverlap, InteractConcurrent, InteractOffset, InteractStack, InteractMostProtective:
		return true
	default:
		return false
	}
}

// ComposeEntitlements combines programs without collapsing their
// semantics. Statutory eligibility never depends on manager approval;
// company policy that narrows mandatory protection reports CONFLICT
// instead of silently narrowing; unknown evidence reports REVIEW_REQUIRED
// or UNKNOWN, never approval or denial; an accrued balance survives a
// promotion or demotion while the worker is not on leave.
func ComposeEntitlements(programs []EntitlementProgram, accruedBalance int, onLeave bool, jobChange string, managerApproved bool) (Composition, error) {
	_ = managerApproved
	if len(programs) == 0 {
		return Composition{}, fmt.Errorf("legal: entitlement composition requires at least one program")
	}
	if accruedBalance < 0 {
		return Composition{}, fmt.Errorf("legal: accrued balance cannot be negative")
	}
	seen := make(map[string]bool, len(programs))
	ordered := append([]EntitlementProgram(nil), programs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	composition := Composition{Programs: ordered, AccruedBalance: accruedBalance, OnLeave: onLeave, JobChange: jobChange}
	chronicle := func(format string, args ...any) {
		composition.Trace = append(composition.Trace, fmt.Sprintf(format, args...))
	}
	statutoryWeeks := 0
	for _, program := range ordered {
		if strings.TrimSpace(program.ID) == "" || seen[program.ID] {
			return Composition{}, fmt.Errorf("legal: program identities must be unique and non-empty")
		}
		seen[program.ID] = true
		if !validAuthority(program.Authority) {
			return Composition{}, fmt.Errorf("legal: program %s authority %q is unknown", program.ID, program.Authority)
		}
		if strings.TrimSpace(program.Release) == "" {
			return Composition{}, fmt.Errorf("legal: program %s needs a release", program.ID)
		}
		if !validInteraction(program.Interaction) {
			return Composition{}, fmt.Errorf("legal: program %s interaction %q collapses composition semantics", program.ID, program.Interaction)
		}
		if program.Weeks < 0 || program.PaidWeeks < 0 || program.PaidWeeks > program.Weeks {
			return Composition{}, fmt.Errorf("legal: program %s weeks are incoherent", program.ID)
		}
		if program.Authority == AuthorityStatutory && program.Eligible && program.Weeks > statutoryWeeks {
			statutoryWeeks = program.Weeks
		}
		if !program.EvidenceKnown {
			chronicle("program %s waits on unknown evidence", program.ID)
		}
	}
	// Company policy never narrows mandatory protection.
	for _, program := range ordered {
		if program.Authority == AuthorityCompany && program.Eligible && program.Weeks < statutoryWeeks {
			composition.State = EntitlementConflict
			chronicle("company program %s narrows statutory protection of %d weeks", program.ID, statutoryWeeks)
		}
	}
	unknown := false
	for _, program := range ordered {
		if !program.EvidenceKnown {
			unknown = true
		}
	}
	switch {
	case composition.State == EntitlementConflict:
	case unknown:
		composition.State = EntitlementReviewRequired
		chronicle("unknown evidence requires review: never approval or denial")
	case onLeave && strings.TrimSpace(jobChange) != "":
		composition.State = EntitlementReviewRequired
		chronicle("job change on leave requires review")
	default:
		composition.State = EntitlementValid
	}
	// Most-protective composition across strategies.
	weeks, paid := 0, 0
	for _, program := range ordered {
		if !program.Eligible {
			continue
		}
		switch program.Interaction {
		case InteractStack:
			weeks += program.Weeks
			paid += program.PaidWeeks
		case InteractOffset:
			if program.Weeks > weeks {
				weeks = program.Weeks
			}
			if program.PaidWeeks > paid {
				paid = program.PaidWeeks
			}
		default:
			if program.Weeks > weeks {
				weeks = program.Weeks
			}
			if program.PaidWeeks > paid {
				paid = program.PaidWeeks
			}
		}
		chronicle("program %s contributes under %s", program.ID, program.Interaction)
	}
	composition.ComposedWeeks = weeks
	composition.ComposedPaid = paid
	if composition.State == "" {
		composition.State = EntitlementUnknown
	}
	composition.Digest = compositionDigest(ordered, composition.State, weeks, paid, accruedBalance, onLeave, jobChange)
	return composition, nil
}

// Verify recomputes the composition seal.
func (composition Composition) Verify() error {
	if composition.Digest == "" {
		return fmt.Errorf("legal: composition seal is missing")
	}
	want := compositionDigest(composition.Programs, composition.State, composition.ComposedWeeks, composition.ComposedPaid, composition.AccruedBalance, composition.OnLeave, composition.JobChange)
	if want != composition.Digest {
		return fmt.Errorf("legal: composition seal is broken")
	}
	return nil
}

// ProgramCatalog guards registered programs for concurrent composition.
type ProgramCatalog struct {
	mu       sync.Mutex
	programs map[string]EntitlementProgram
}

// NewProgramCatalog starts an empty catalog.
func NewProgramCatalog() *ProgramCatalog {
	return &ProgramCatalog{programs: make(map[string]EntitlementProgram)}
}

// Register publishes one program. Duplicates refuse.
func (catalog *ProgramCatalog) Register(program EntitlementProgram) error {
	if catalog == nil {
		return fmt.Errorf("legal: nil program catalog")
	}
	if strings.TrimSpace(program.ID) == "" {
		return fmt.Errorf("legal: program id is required")
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if _, dup := catalog.programs[program.ID]; dup {
		return fmt.Errorf("legal: program %s is already registered", program.ID)
	}
	catalog.programs[program.ID] = program
	return nil
}

// Snapshot lists registered programs in order.
func (catalog *ProgramCatalog) Snapshot() []EntitlementProgram {
	if catalog == nil {
		return nil
	}
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	out := make([]EntitlementProgram, 0, len(catalog.programs))
	for _, program := range catalog.programs {
		out = append(out, program)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
