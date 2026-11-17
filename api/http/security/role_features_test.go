package security

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
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
