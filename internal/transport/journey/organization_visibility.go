package journey

import (
	"context"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s *server) visibleWorkforce(ctx context.Context, principal *trust.Principal, workers []workspace.WorkerSummary, options workspace.WorkforceOptions) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	if principal.HasRole("hcm_admin") || principal.HasRole("comp_admin") {
		return workers, options, nil
	}
	if s.deps.RoleAccess != nil {
		snapshot, err := s.deps.RoleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
		if err != nil {
			return nil, workspace.WorkforceOptions{}, err
		}
		roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
		if policies := roleaccess.PoliciesForRoles(snapshot, roles); len(policies) > 0 {
			visible, visibleOptions := visibleWorkforceForRolePolicies(principal, workers, options, policies)
			return visible, visibleOptions, nil
		}
	}
	if s.deps.Preferences == nil {
		return workers, options, nil
	}
	snapshot, err := s.deps.Preferences.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	if err := preferences.ValidateOrganizationVisibility(snapshot.OrganizationVisibility); err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	policy := preferences.NormalizeOrganizationVisibility(snapshot.OrganizationVisibility)
	if policy.Mode == preferences.OrganizationVisibilityAll {
		return workers, options, nil
	}

	ownUnit := ""
	for _, worker := range workers {
		if workerMatchesPrincipal(worker, principal.Subject()) {
			ownUnit = normalizedOrganizationUnit(worker.OrgUnit)
			break
		}
	}
	qualified := make(map[string]bool, len(policy.OrganizationUnits))
	for _, unit := range policy.OrganizationUnits {
		qualified[normalizedOrganizationUnit(unit)] = true
	}
	visible := make([]workspace.WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		unit := normalizedOrganizationUnit(worker.OrgUnit)
		self := workerMatchesPrincipal(worker, principal.Subject())
		include := self
		switch policy.Mode {
		case preferences.OrganizationVisibilityOwnUnit:
			include = include || ownUnit != "" && unit == ownUnit
		case preferences.OrganizationVisibilityAllowlist:
			include = include || qualified[unit]
		case preferences.OrganizationVisibilityDenylist:
			include = include || !qualified[unit]
		}
		if include {
			visible = append(visible, worker)
		}
	}
	return redactHiddenManagers(visible), visibleWorkforceOptions(options, visible), nil
}

func visibleWorkforceForRolePolicies(principal *trust.Principal, workers []workspace.WorkerSummary, options workspace.WorkforceOptions, policies []roleaccess.VisibilityPolicy) ([]workspace.WorkerSummary, workspace.WorkforceOptions) {
	ownUnit := ""
	for _, worker := range workers {
		if workerMatchesPrincipal(worker, principal.Subject()) {
			ownUnit = normalizedOrganizationUnit(worker.OrgUnit)
			break
		}
	}
	visible := make([]workspace.WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		unit := normalizedOrganizationUnit(worker.OrgUnit)
		include := workerMatchesPrincipal(worker, principal.Subject())
		for _, policy := range policies {
			qualified := map[string]bool{}
			for _, candidate := range policy.OrganizationUnits {
				qualified[normalizedOrganizationUnit(candidate)] = true
			}
			switch policy.Mode {
			case roleaccess.VisibilityAll:
				include = true
			case roleaccess.VisibilityOwnUnit:
				include = include || ownUnit != "" && unit == ownUnit
			case roleaccess.VisibilityAllowlist:
				include = include || qualified[unit]
			case roleaccess.VisibilityDenylist:
				include = include || !qualified[unit]
			}
			if include {
				break
			}
		}
		if include {
			visible = append(visible, worker)
		}
	}
	return redactHiddenManagers(visible), visibleWorkforceOptions(options, visible)
}

func workerMatchesPrincipal(worker workspace.WorkerSummary, subject string) bool {
	subject = strings.ToLower(strings.TrimSpace(subject))
	if subject == "" {
		return false
	}
	for _, candidate := range []string{worker.WorkerRef, worker.WorkerID, worker.WorkerNumber} {
		if strings.ToLower(strings.TrimSpace(candidate)) == subject {
			return true
		}
	}
	return false
}

func normalizedOrganizationUnit(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func redactHiddenManagers(workers []workspace.WorkerSummary) []workspace.WorkerSummary {
	visible := make(map[string]bool, len(workers)*2)
	for _, worker := range workers {
		visible[strings.ToLower(strings.TrimSpace(worker.WorkerRef))] = true
		visible[strings.ToLower(strings.TrimSpace(worker.WorkerID))] = true
	}
	result := append([]workspace.WorkerSummary(nil), workers...)
	for index := range result {
		if result[index].ManagerRef != "" && !visible[strings.ToLower(strings.TrimSpace(result[index].ManagerRef))] {
			result[index].ManagerRef = ""
		}
	}
	return result
}

func visibleWorkforceOptions(options workspace.WorkforceOptions, workers []workspace.WorkerSummary) workspace.WorkforceOptions {
	seen := make(map[string]string)
	for _, worker := range workers {
		if unit := strings.TrimSpace(worker.OrgUnit); unit != "" {
			seen[normalizedOrganizationUnit(unit)] = unit
		}
	}
	options.OrgUnits = make([]string, 0, len(seen))
	for _, unit := range seen {
		options.OrgUnits = append(options.OrgUnits, unit)
	}
	sort.Slice(options.OrgUnits, func(i, j int) bool {
		return strings.ToLower(options.OrgUnits[i]) < strings.ToLower(options.OrgUnits[j])
	})
	return options
}
