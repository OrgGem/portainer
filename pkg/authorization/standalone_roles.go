package authorization

import (
	"fmt"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
)

type StandaloneAccess struct {
	Role     portainer.StandaloneRole
	Explicit bool
	Source   AccessSource
}

func SupportsStandaloneRoles(endpoint *portainer.Endpoint) bool {
	return endpoint.Type == portainer.DockerEnvironment || endpoint.Type == portainer.AgentOnDockerEnvironment
}

func StandaloneRoleDefinition(role portainer.StandaloneRole) *portainer.Role {
	definitions := map[portainer.StandaloneRole]portainer.Role{
		portainer.StandaloneEnvironmentAdmin: {ID: 1, Name: "Environment administrator", Description: "Manage Docker resources in assigned environments; no global settings, host administration or ownership changes", Priority: 1},
		portainer.StandaloneOperator:         {ID: 5, Name: "Operator", Description: "Operate existing Docker workloads and consoles; no resource creation/deletion or Compose edits", Priority: 2},
		portainer.StandaloneHelpdesk:         {ID: 2, Name: "Helpdesk", Description: "Read all resources in assigned environments; no console or file access", Priority: 3},
		portainer.StandaloneStandard:         {ID: 3, Name: "Standard user", Description: "Manage assigned and public resources; stack operation policies also apply", Priority: 4},
		portainer.StandaloneReadOnly:         {ID: 4, Name: "Read-only user", Description: "Read assigned and public resources; no writes, console or file access", Priority: 5},
	}
	value, ok := definitions[role]
	if !ok {
		return nil
	}
	return &value
}

func StandaloneRoleDefinitions() []portainer.Role {
	roles := []portainer.Role{}
	for _, role := range []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneOperator, portainer.StandaloneHelpdesk, portainer.StandaloneStandard, portainer.StandaloneReadOnly} {
		roles = append(roles, *StandaloneRoleDefinition(role))
	}
	return roles
}

func StandaloneStackActionAllowed(role portainer.StandaloneRole, action portainer.StackAction) bool {
	switch role {
	case "", portainer.StandaloneEnvironmentAdmin, portainer.StandaloneStandard:
		return true
	case portainer.StandaloneOperator:
		return action != portainer.StackEdit
	case portainer.StandaloneHelpdesk, portainer.StandaloneReadOnly:
		return action == portainer.StackView
	default:
		return false
	}
}

func (a *StandaloneAccess) AllResources() bool {
	return a != nil && a.Explicit && (a.Role == portainer.StandaloneEnvironmentAdmin || a.Role == portainer.StandaloneOperator || a.Role == portainer.StandaloneHelpdesk)
}

func (a *StandaloneAccess) EnvironmentAdmin() bool {
	return a != nil && a.Explicit && a.Role == portainer.StandaloneEnvironmentAdmin
}

func StandaloneRolePriority(role portainer.StandaloneRole) int {
	switch role {
	case portainer.StandaloneEnvironmentAdmin:
		return 1
	case portainer.StandaloneOperator:
		return 2
	case portainer.StandaloneHelpdesk:
		return 3
	case portainer.StandaloneStandard:
		return 4
	case portainer.StandaloneReadOnly:
		return 5
	default:
		return 0
	}
}

func ValidateStandalonePolicies(users portainer.UserAccessPolicies, teams portainer.TeamAccessPolicies) error {
	validate := func(p portainer.AccessPolicy) error {
		if p.StandaloneRole != "" && StandaloneRolePriority(p.StandaloneRole) == 0 {
			return fmt.Errorf("unknown Docker Standalone role: %s", p.StandaloneRole)
		}
		return nil
	}
	for _, p := range users {
		if err := validate(p); err != nil {
			return err
		}
	}
	for _, p := range teams {
		if err := validate(p); err != nil {
			return err
		}
	}
	return nil
}

func HasStandalonePolicies(users portainer.UserAccessPolicies, teams portainer.TeamAccessPolicies) bool {
	for _, p := range users {
		if p.StandaloneRole != "" {
			return true
		}
	}
	for _, p := range teams {
		if p.StandaloneRole != "" {
			return true
		}
	}
	return false
}

// Match the existing access viewer precedence. At a team layer, the highest
// priority number (the more restricted scope) wins; ties use the lowest team ID.
func ResolveStandaloneAccess(input ResolverInput) *StandaloneAccess {
	from := func(p portainer.AccessPolicy, source AccessSource) *StandaloneAccess {
		role := p.StandaloneRole
		if role == "" {
			role = portainer.StandaloneStandard
		}
		return &StandaloneAccess{Role: role, Explicit: p.StandaloneRole != "", Source: source}
	}
	if p, ok := input.Endpoint.UserAccessPolicies[input.User.ID]; ok {
		return from(p, AccessSource{})
	}
	if p, ok := input.EndpointGroup.UserAccessPolicies[input.User.ID]; ok {
		return from(p, AccessSource{GroupID: input.EndpointGroup.ID})
	}
	teams := func(policies portainer.TeamAccessPolicies, group portainer.EndpointGroupID) *StandaloneAccess {
		var best *StandaloneAccess
		for _, membership := range input.UserMemberships {
			p, ok := policies[membership.TeamID]
			if !ok {
				continue
			}
			next := from(p, AccessSource{GroupID: group, TeamID: membership.TeamID})
			if best == nil || StandaloneRolePriority(next.Role) > StandaloneRolePriority(best.Role) || (next.Role == best.Role && ((next.Explicit && !best.Explicit) || (next.Explicit == best.Explicit && next.Source.TeamID < best.Source.TeamID))) {
				best = next
			}
		}
		return best
	}
	if access := teams(input.Endpoint.TeamAccessPolicies, 0); access != nil {
		return access
	}
	return teams(input.EndpointGroup.TeamAccessPolicies, input.EndpointGroup.ID)
}

func StandaloneAccessForUser(store dataservices.DataStoreTx, userID portainer.UserID, endpoint *portainer.Endpoint) (*StandaloneAccess, error) {
	memberships, err := store.TeamMembership().TeamMembershipsByUserID(userID)
	if err != nil {
		return nil, err
	}
	group := &portainer.EndpointGroup{}
	if endpoint.GroupID != 0 {
		group, err = store.EndpointGroup().Read(endpoint.GroupID)
		if err != nil {
			return nil, err
		}
	}
	return ResolveStandaloneAccess(ResolverInput{User: &portainer.User{ID: userID}, Endpoint: endpoint, EndpointGroup: *group, UserMemberships: memberships}), nil
}

func StandaloneResourceAccess(access *StandaloneAccess, userID portainer.UserID, teams []portainer.TeamID, control *portainer.ResourceControl) bool {
	if access.EnvironmentAdmin() {
		return true
	}
	if control != nil && control.StackPolicy != nil {
		return UserCanAccessResource(userID, teams, control)
	}
	return access.AllResources() || UserCanAccessResource(userID, teams, control)
}
