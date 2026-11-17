package authorization

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStandaloneRolesAreExplicitAndUsePolicyPrecedence(t *testing.T) {
	input := ResolverInput{User: &portainer.User{ID: 7}, Endpoint: &portainer.Endpoint{ID: 1, TeamAccessPolicies: portainer.TeamAccessPolicies{2: {RoleID: 1}}}, EndpointGroup: portainer.EndpointGroup{ID: 9, TeamAccessPolicies: portainer.TeamAccessPolicies{2: {StandaloneRole: portainer.StandaloneEnvironmentAdmin}}}, UserMemberships: []portainer.TeamMembership{{TeamID: 2}, {TeamID: 3}}}
	access := ResolveStandaloneAccess(input)
	require.Equal(t, portainer.StandaloneStandard, access.Role)
	require.False(t, access.Explicit, "legacy CE RoleId=1 must not promote a team")
	input.Endpoint.TeamAccessPolicies[2] = portainer.AccessPolicy{StandaloneRole: portainer.StandaloneEnvironmentAdmin}
	input.Endpoint.TeamAccessPolicies[3] = portainer.AccessPolicy{StandaloneRole: portainer.StandaloneReadOnly}
	access = ResolveStandaloneAccess(input)
	require.Equal(t, portainer.StandaloneReadOnly, access.Role, "more restricted team priority wins")
	require.Equal(t, portainer.TeamID(3), access.Source.TeamID)
	input.EndpointGroup.UserAccessPolicies = portainer.UserAccessPolicies{7: {StandaloneRole: portainer.StandaloneHelpdesk}}
	require.Equal(t, portainer.StandaloneHelpdesk, ResolveStandaloneAccess(input).Role)
	input.Endpoint.UserAccessPolicies = portainer.UserAccessPolicies{7: {StandaloneRole: portainer.StandaloneOperator}}
	require.Equal(t, portainer.StandaloneOperator, ResolveStandaloneAccess(input).Role)
	delete(input.Endpoint.UserAccessPolicies, 7)
	delete(input.EndpointGroup.UserAccessPolicies, 7)
	input.Endpoint.TeamAccessPolicies = nil
	access = ResolveStandaloneAccess(input)
	require.True(t, access.EnvironmentAdmin())
	require.Equal(t, portainer.EndpointGroupID(9), access.Source.GroupID)
	input.UserMemberships = nil
	require.Nil(t, ResolveStandaloneAccess(input), "membership revocation removes access")
}

func TestStandaloneRolesIntersectStackPolicy(t *testing.T) {
	control := NewPrivateResourceControl("1_app", portainer.StackResourceControl, 99)
	helpdesk := &StandaloneAccess{Role: portainer.StandaloneHelpdesk, Explicit: true}
	require.True(t, StandaloneResourceAccess(helpdesk, 7, nil, control))
	control.StackPolicy = &portainer.StackAccessPolicy{}
	require.False(t, StandaloneResourceAccess(helpdesk, 7, nil, control))
	require.True(t, StandaloneResourceAccess(&StandaloneAccess{Role: portainer.StandaloneEnvironmentAdmin, Explicit: true}, 7, nil, control))
	require.False(t, StandaloneStackActionAllowed(portainer.StandaloneReadOnly, portainer.StackConfigApply))
	require.False(t, StandaloneStackActionAllowed(portainer.StandaloneOperator, portainer.StackEdit))
	require.True(t, StandaloneStackActionAllowed(portainer.StandaloneOperator, portainer.StackConfigApply))
	require.Error(t, ValidateStandalonePolicies(portainer.UserAccessPolicies{7: {StandaloneRole: "root"}}, nil))
}
