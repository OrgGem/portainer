package security

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	httperrors "github.com/portainer/portainer/api/http/errors"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSavedRolePolicyImmediatelyEnforcesProxyAndHelpers(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	endpoint := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, UserAccessPolicies: portainer.UserAccessPolicies{2: {StandaloneRole: portainer.StandaloneEnvironmentAdmin}}}
	role := authorization.StandaloneRoleDefinition(portainer.StandaloneEnvironmentAdmin)
	role.StandalonePolicy = authorization.DefaultStandalonePolicy(portainer.StandaloneEnvironmentAdmin)
	check := func(method, path string) error {
		r := httptest.NewRequest(method, path, nil)
		r = r.WithContext(StoreTokenData(r, &portainer.TokenData{ID: 2, Role: portainer.StandardUserRole}))
		return CheckStandaloneEndpointRole(r, store, endpoint, false)
	}
	cases := []struct{ feature, method, path string }{
		{"containers.operate", "POST", "/api/endpoints/1/docker/v1.47/containers/id/restart"},
		{"containers.operate", "POST", "/1/docker/containers/id/restart"},
		{"containers.manage", "POST", "/api/docker/1/containers/id/recreate"},
		{"containers.console", "GET", "/api/websocket/exec?endpointId=1"},
		{"containers.console", "POST", "/api/endpoints/1/docker/exec/id/start"},
		{"volumes.files", "PUT", "/api/endpoints/1/docker/v2/browse/text?volumeID=data"},
		{"stacks.view", "GET", "/api/stacks/1"},
		{"config.apply", "POST", "/api/stacks/1/configurations/rev/rollback"},
		{"vault.view", "GET", "/api/stacks/vault/mappings"},
		{"vault.edit", "PUT", "/api/stacks/1/vault"},
		{"metrics.view", "GET", "/api/endpoints/1/containers/id/metrics"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			role.StandalonePolicy = authorization.DefaultStandalonePolicy(roleKey())
			require.NoError(t, store.Role().Update(role.ID, role))
			require.NoError(t, check(tc.method, tc.path))
			role.StandalonePolicy.Features[tc.feature] = false
			require.NoError(t, store.Role().Update(role.ID, role))
			require.ErrorIs(t, check(tc.method, tc.path), ErrStandaloneRoleDenied)
		})
	}
	role.StandalonePolicy.Enabled = false
	require.NoError(t, store.Role().Update(role.ID, role))
	require.ErrorIs(t, check("GET", "/api/endpoints/1/docker/containers/json"), ErrStandaloneRoleDenied)
	r := httptest.NewRequest("GET", "/api/endpoints/1/docker/containers/json", nil)
	r = r.WithContext(StoreTokenData(r, &portainer.TokenData{ID: 1, Role: portainer.AdministratorRole}))
	require.NoError(t, CheckStandaloneEndpointRole(r, store, endpoint, false))
}
func roleKey() portainer.StandaloneRole { return portainer.StandaloneEnvironmentAdmin }

func TestOperatorCanEnterDockerAgentEnvironmentWithDirectOrTeamAccess(t *testing.T) {
	for _, grantSource := range []string{"direct user", "team inherited"} {
		t.Run(grantSource, func(t *testing.T) {
			_, store := datastore.MustNewTestStore(t, true, false)
			group := &portainer.EndpointGroup{ID: 10}
			if grantSource == "team inherited" {
				group.TeamAccessPolicies = portainer.TeamAccessPolicies{
					20: {StandaloneRole: portainer.StandaloneOperator},
				}
				require.NoError(t, store.TeamMembership().Create(&portainer.TeamMembership{
					ID: 30, UserID: 2, TeamID: 20, Role: portainer.TeamMember,
				}))
			}
			require.NoError(t, store.EndpointGroup().Create(group))

			endpoint := &portainer.Endpoint{
				ID: 40, Type: portainer.AgentOnDockerEnvironment, GroupID: group.ID,
			}
			if grantSource == "direct user" {
				endpoint.UserAccessPolicies = portainer.UserAccessPolicies{
					2: {StandaloneRole: portainer.StandaloneOperator},
				}
			}
			require.NoError(t, store.Endpoint().Create(endpoint))

			operator := authorization.StandaloneRoleDefinition(portainer.StandaloneOperator)
			operator.StandalonePolicy = authorization.DefaultStandalonePolicy(portainer.StandaloneOperator)
			require.NoError(t, store.Role().Update(operator.ID, operator))

			request := httptest.NewRequest(http.MethodGet, "/api/endpoints/40/docker/ping", nil)
			request = request.WithContext(StoreTokenData(request, &portainer.TokenData{
				ID: 2, Role: portainer.StandardUserRole,
			}))
			bouncer := NewRequestBouncer(t.Context(), store, nil, nil)
			require.NoError(t, bouncer.AuthorizedEndpointOperation(request, endpoint))

			// The proxy transport sees the path after /endpoints/{id}/docker is stripped.
			transportRequest := httptest.NewRequest(http.MethodGet, "/ping", nil)
			transportRequest = transportRequest.WithContext(StoreTokenData(transportRequest, &portainer.TokenData{
				ID: 2, Role: portainer.StandardUserRole,
			}))
			require.NoError(t, CheckStandaloneEndpointRole(transportRequest, store, endpoint, true))

			// The Agent-prefixed route uses the same narrow version-read permission.
			aliasRequest := httptest.NewRequest(http.MethodGet, "/api/endpoints/40/agent/docker/ping", nil)
			aliasRequest = aliasRequest.WithContext(StoreTokenData(aliasRequest, &portainer.TokenData{
				ID: 2, Role: portainer.StandardUserRole,
			}))
			require.NoError(t, CheckStandaloneEndpointRole(aliasRequest, store, endpoint, false))
		})
	}
}

func TestOperatorAgentPingRequiresEnvironmentViewAndExactReadRoute(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	group := &portainer.EndpointGroup{ID: 10}
	require.NoError(t, store.EndpointGroup().Create(group))
	endpoint := &portainer.Endpoint{
		ID: 40, Type: portainer.AgentOnDockerEnvironment, GroupID: group.ID,
		UserAccessPolicies: portainer.UserAccessPolicies{
			2: {StandaloneRole: portainer.StandaloneOperator},
		},
	}
	require.NoError(t, store.Endpoint().Create(endpoint))
	operator := authorization.StandaloneRoleDefinition(portainer.StandaloneOperator)
	operator.StandalonePolicy = authorization.DefaultStandalonePolicy(portainer.StandaloneOperator)
	require.NoError(t, store.Role().Update(operator.ID, operator))

	check := func(method, path string, upgrade bool) error {
		r := httptest.NewRequest(method, path, nil)
		if upgrade {
			r.Header.Set("Upgrade", "websocket")
		}
		r = r.WithContext(StoreTokenData(r, &portainer.TokenData{ID: 2, Role: portainer.StandardUserRole}))
		return CheckStandaloneEndpointRole(r, store, endpoint, false)
	}

	for _, tc := range []struct {
		name, method, path string
		upgrade            bool
	}{
		{"versioned route", http.MethodGet, "/api/endpoints/40/docker/v1.47/ping", false},
		{"wrong method", http.MethodPost, "/api/endpoints/40/docker/ping", false},
		{"head request", http.MethodHead, "/api/endpoints/40/docker/ping", false},
		{"upgraded request", http.MethodGet, "/api/endpoints/40/docker/ping", true},
		{"extended route", http.MethodGet, "/api/endpoints/40/docker/ping/anything", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, check(tc.method, tc.path, tc.upgrade), ErrStandaloneRoleDenied)
		})
	}
	for _, path := range []string{"/api1/docker/ping", "/api/ping"} {
		require.False(t, isStandaloneAgentPing(httptest.NewRequest(http.MethodGet, path, nil), false), path)
	}

	operator.StandalonePolicy.Features["environment.view"] = false
	require.NoError(t, store.Role().Update(operator.ID, operator))
	require.ErrorIs(t, check(http.MethodGet, "/api/endpoints/40/docker/ping", false), ErrStandaloneRoleDenied)

	operator.StandalonePolicy = authorization.DefaultStandalonePolicy(portainer.StandaloneOperator)
	operator.StandalonePolicy.Enabled = false
	require.NoError(t, store.Role().Update(operator.ID, operator))
	require.ErrorIs(t, check(http.MethodGet, "/api/endpoints/40/docker/ping", false), ErrStandaloneRoleDenied)

	operator.StandalonePolicy = authorization.DefaultStandalonePolicy(portainer.StandaloneOperator)
	require.NoError(t, store.Role().Update(operator.ID, operator))
	endpoint.Type = portainer.DockerEnvironment
	require.ErrorIs(t, check(http.MethodGet, "/api/endpoints/40/docker/ping", false), ErrStandaloneRoleDenied)
}

func TestAgentPingStillRequiresEnvironmentAccess(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	group := &portainer.EndpointGroup{ID: 10}
	require.NoError(t, store.EndpointGroup().Create(group))
	endpoint := &portainer.Endpoint{ID: 40, Type: portainer.AgentOnDockerEnvironment, GroupID: group.ID}
	require.NoError(t, store.Endpoint().Create(endpoint))
	bouncer := NewRequestBouncer(t.Context(), store, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/endpoints/40/docker/ping", nil)
	request = request.WithContext(StoreTokenData(request, &portainer.TokenData{
		ID: 2, Role: portainer.StandardUserRole,
	}))
	require.ErrorIs(t, bouncer.AuthorizedEndpointOperation(request, endpoint), httperrors.ErrEndpointAccessDenied)
}
