package productui

import (
	"fmt"
	"strings"
)

// ActionBinding is one semantic action binding: a bare capability
// reference routed through the capability gateway, carrying the
// current action token proving availability, the expected
// workflow/resource version, an idempotency key, and the named typed
// input. Capability availability is runtime gateway authorization —
// the token proves it — so presentation keeps no static capability
// allowlist; allowlisting capabilities here would duplicate the
// gateway's authorization as a second authority source. Bindings
// carry no endpoint: the client never synthesizes approval,
// execution, repair, or employee-mutation endpoints.
type ActionBinding struct {
	Capability      string
	Token           string
	ExpectedVersion int64
	IdempotencyKey  string
	InputType       string
}

// ActionVerdict is the binding answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type ActionVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidateActionBinding checks one action binding: a bare capability
// reference (endpoint-shaped values refused), a current action
// token, a positive expected version, an idempotency key, and a
// named input type. Violations accumulate in fixed order.
func ValidateActionBinding(binding ActionBinding) ActionVerdict {
	var reasons []string
	if binding.Capability == "" {
		reasons = append(reasons, "missing capability")
	} else if strings.Contains(binding.Capability, "://") || strings.HasPrefix(binding.Capability, "/") {
		reasons = append(reasons, fmt.Sprintf("synthesized endpoint %q", binding.Capability))
	}
	if binding.Token == "" {
		reasons = append(reasons, "missing action token")
	}
	if binding.ExpectedVersion <= 0 {
		reasons = append(reasons, fmt.Sprintf("unsupported action version %d", binding.ExpectedVersion))
	}
	if binding.IdempotencyKey == "" {
		reasons = append(reasons, "missing idempotency key")
	}
	if binding.InputType == "" {
		reasons = append(reasons, "missing input type")
	}
	if len(reasons) > 0 {
		return ActionVerdict{Compatible: false, Reasons: reasons}
	}
	return ActionVerdict{Compatible: true}
}

// ValidateDraftActions validates one draft through its action
// bindings. Every binding must validate; reasons accumulate across
// bindings in composition order.
func ValidateDraftActions(draft PageDraft) ActionVerdict {
	var reasons []string
	for _, binding := range draft.Composition.Actions {
		verdict := ValidateActionBinding(binding)
		reasons = append(reasons, verdict.Reasons...)
	}
	if len(reasons) > 0 {
		return ActionVerdict{Compatible: false, Reasons: reasons}
	}
	return ActionVerdict{Compatible: true}
}
