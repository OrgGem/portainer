package security

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestStandaloneRolesResolveFreshPoliciesAndRejectUnsupportedEnvironments(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	group := &portainer.EndpointGroup{ID: 10, TeamAccessPolicies: portainer.TeamAccessPolicies{20: {StandaloneRole: portainer.StandaloneReadOnly}}}
	require.NoError(t, store.EndpointGroup().Create(group))
	membership := &portainer.TeamMembership{ID: 30, UserID: 2, TeamID: 20, Role: portainer.TeamMember}
	require.NoError(t, store.TeamMembership().Create(membership))
	endpoint := &portainer.Endpoint{ID: 40, Type: portainer.DockerEnvironment, GroupID: group.ID}
	require.NoError(t, store.Endpoint().Create(endpoint))
	r := httptest.NewRequest("POST", "/api/endpoints/40/docker/containers/id/restart", nil)
	r = r.WithContext(StoreTokenData(r, &portainer.TokenData{ID: 2, Role: portainer.StandardUserRole}))
	require.ErrorIs(t, CheckStandaloneEndpointRole(r, store, endpoint, true), ErrStandaloneRoleDenied)
	protected, err := EndpointHasUnattendedRestrictions(store, endpoint.ID)
	require.NoError(t, err)
	require.True(t, protected)
	group.TeamAccessPolicies[20] = portainer.AccessPolicy{StandaloneRole: portainer.StandaloneOperator}
	require.NoError(t, store.EndpointGroup().Update(group.ID, group))
	require.NoError(t, CheckStandaloneEndpointRole(r, store, endpoint, true))
	endpoint.Type = portainer.AgentOnDockerEnvironment
	require.NoError(t, CheckStandaloneEndpointRole(r, store, endpoint, true))
	endpoint.Type = portainer.KubernetesLocalEnvironment
	require.ErrorIs(t, CheckStandaloneEndpointRole(r, store, endpoint, false), ErrStandaloneRoleDenied)
	endpoint.Type = portainer.EdgeAgentOnDockerEnvironment
	require.ErrorIs(t, CheckStandaloneEndpointRole(r, store, endpoint, true), ErrStandaloneRoleDenied)
	endpoint.Type = portainer.DockerEnvironment
	require.NoError(t, store.TeamMembership().Delete(membership.ID))
	access, err := EndpointStandaloneAccess(r, store, endpoint)
	require.NoError(t, err)
	require.Nil(t, access, "the environment access gate must now reject the user")
}

func TestStandaloneDockerRoleMatrix(t *testing.T) {
	roles := []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneOperator, portainer.StandaloneHelpdesk, portainer.StandaloneStandard, portainer.StandaloneReadOnly}
	for _, tc := range []struct {
		method, path string
		allowed      [5]bool
	}{
		{"GET", "/v1.47/containers/json", [5]bool{true, true, true, true, true}},
		{"GET", "/containers/id/logs", [5]bool{true, true, true, true, true}},
		{"GET", "/host/docker-storage", [5]bool{true, true, true, true, true}},
		{"POST", "/host/docker-storage", [5]bool{}},
		{"GET", "/host/unknown", [5]bool{}},
		{"POST", "/containers/id/restart", [5]bool{true, true, false, true, false}},
		{"POST", "/containers/id/exec", [5]bool{true, true, false, true, false}},
		{"POST", "/exec/id/start", [5]bool{true, true, false, true, false}},
		{"POST", "/containers/create", [5]bool{true, false, false, true, false}},
		{"DELETE", "/containers/id", [5]bool{true, false, false, true, false}},
		{"GET", "/containers/id/archive", [5]bool{true, false, false, true, false}},
		{"POST", "/plugins/create", [5]bool{}},
		{"POST", "/swarm/init", [5]bool{}},
		{"POST", "/unknown/future-operation", [5]bool{}},
	} {
		for i, role := range roles {
			t.Run(string(role)+" "+tc.method+" "+tc.path, func(t *testing.T) {
				require.Equal(t, tc.allowed[i], StandaloneRoleAllowsRequest(role, httptest.NewRequest(tc.method, tc.path, nil), true))
			})
		}
	}
}

func TestStandalonePortainerRoleMatrix(t *testing.T) {
	for _, role := range []portainer.StandaloneRole{portainer.StandaloneReadOnly, portainer.StandaloneHelpdesk} {
		for _, url := range []string{"/stacks/1/start", "/stacks/1/configurations/abc/apply", "/stacks/create/standalone/string?endpointId=1"} {
			require.False(t, StandaloneRoleAllowsRequest(role, httptest.NewRequest("POST", url, nil), false))
		}
		require.False(t, StandaloneRoleAllowsRequest(role, httptest.NewRequest("GET", "/websocket/exec", nil), false))
	}
	require.True(t, StandaloneRoleAllowsRequest(portainer.StandaloneOperator, httptest.NewRequest("POST", "/stacks/1/stop", nil), false))
	require.False(t, StandaloneRoleAllowsRequest(portainer.StandaloneOperator, httptest.NewRequest("PUT", "/stacks/1", nil), false))
	require.False(t, StandaloneRoleAllowsRequest(portainer.StandaloneEnvironmentAdmin, httptest.NewRequest("POST", "/endpoints/1/dockerhub", nil), false))
	require.False(t, StandaloneRoleAllowsRequest(portainer.StandaloneEnvironmentAdmin, httptest.NewRequest("POST", "/containers/id%2Frestart", nil), true))
}

func TestVolumeEditorRolesDoNotGrantHostBrowsing(t *testing.T) {
	for _, role := range []portainer.StandaloneRole{portainer.StandaloneEnvironmentAdmin, portainer.StandaloneStandard, portainer.StandaloneOperator, portainer.StandaloneHelpdesk, portainer.StandaloneReadOnly} {
		for _, method := range []string{"GET", "PUT"} {
			r := httptest.NewRequest(method, "/api/endpoints/1/docker/v2/browse/text?volumeID=data&path=/app.conf", nil)
			require.Equal(t, role == portainer.StandaloneEnvironmentAdmin, StandaloneRoleAllowsRequest(role, r, false))
		}
		for _, p := range []string{"/v2/browse/text?path=/etc/passwd", "/v2/browse/text?volumeID=&path=/x", "/v2/browse/text?volumeID=a&volumeID=b&path=/x", "/v1/browse/data/put", "/v2/browse/unknown?volumeID=data"} {
			require.False(t, StandaloneRoleAllowsRequest(role, httptest.NewRequest("PUT", p, nil), true), string(role)+p)
		}
	}
}
