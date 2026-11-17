package security

import (
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/uac"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
)

func TestReviewRouteClassification(t *testing.T) {
	for _, prefix := range []string{"/api/endpoints/1/docker", "/endpoints/1/docker", "/1/docker", "/api/endpoints/1/agent/docker", "/1/agent/docker"} {
		r := httptest.NewRequest("POST", prefix+"/containers/id/restart", nil)
		require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneStandard, r, false), prefix)
		require.False(t, StandaloneRoleAllowsRequest(portainer.StandaloneReadOnly, r, false), prefix)
	}
	for _, role := range []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneOperator, portainer.StandaloneHelpdesk, portainer.StandaloneStandard, portainer.StandaloneReadOnly} {
		for _, path := range []string{"/api/docker/1/dashboard", "/api/docker/1/images", "/api/docker/1/containers/abc/gpus"} {
			require.True(t, StandaloneRoleAllowsRequest(role, httptest.NewRequest("GET", path, nil), false), string(role)+path)
		}
	}
	for _, path := range []string{"/api/endpoints/1/docker/images/org/app:tag/json", "/api/endpoints/1/docker/distribution/org/app:tag/json"} {
		require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneEnvironmentAdmin, httptest.NewRequest("GET", path, nil), false), path)
	}
	require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneStandard, httptest.NewRequest("GET", "/api/endpoints/1/docker/images/alpine:latest/json", nil), false))
	for _, path := range []string{"/api/endpoints/1/docker/images/get", "/api/endpoints/1/docker/images/sha256:abc/get"} {
		r := httptest.NewRequest("GET", path, nil)
		require.False(t, IsStackBoundaryRead(r), path)
		require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneStandard, r, false), path)
	}
	require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneEnvironmentAdmin, httptest.NewRequest("GET", "/api/endpoints/1/docker/v2/browse/ls?volumeID=test", nil), false))
	require.False(t, StandaloneRoleAllowsRequest(portainer.StandaloneEnvironmentAdmin, httptest.NewRequest("GET", "/api/endpoints/1/docker/v2/browse/ls?path=/", nil), false))
	require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneEnvironmentAdmin, httptest.NewRequest("GET", "/api/endpoints/1/docker/v2/dockerhub/0", nil), false))
}

func TestReviewLegacyExplicitTie(t *testing.T) {
	input := authorization.ResolverInput{User: &portainer.User{ID: 7}, Endpoint: &portainer.Endpoint{ID: 1, TeamAccessPolicies: portainer.TeamAccessPolicies{10: {RoleID: 1}, 20: {StandaloneRole: portainer.StandaloneStandard}}}, UserMemberships: []portainer.TeamMembership{{TeamID: 10}, {TeamID: 20}}}
	resolved := authorization.ResolveStandaloneAccess(input)
	require.Equal(t, portainer.StandaloneStandard, resolved.Role)
	require.True(t, resolved.Explicit)
	input.Endpoint.TeamAccessPolicies[10], input.Endpoint.TeamAccessPolicies[20] = input.Endpoint.TeamAccessPolicies[20], input.Endpoint.TeamAccessPolicies[10]
	require.True(t, authorization.ResolveStandaloneAccess(input).Explicit)
}

func TestReviewDashboardFilter(t *testing.T) {
	for _, role := range []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneOperator, portainer.StandaloneHelpdesk} {
		user := &portainer.User{ID: 7, Role: portainer.StandardUserRole, StandaloneEndpointRoles: map[portainer.EndpointID]portainer.StandaloneRole{1: role}}
		control := &portainer.ResourceControl{AdministratorsOnly: true}
		result, err := uac.FilterByResourceControl([]string{"private"}, user, nil, func(string) (*portainer.ResourceControl, error) { return control, nil }, &authorization.StandaloneAccess{Role: role, Explicit: true})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.True(t, (&authorization.StandaloneAccess{Role: role, Explicit: true}).AllResources())
	}
}
