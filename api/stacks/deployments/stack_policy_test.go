package deployments

import (
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/stretchr/testify/require"
)

func TestManagedEndpointRejectsAutomaticDeployment(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	require.NoError(t, store.Stack().Create(&portainer.Stack{ID: 1, EndpointID: 2, Type: portainer.DockerComposeStack, Status: portainer.StackStatusActive, WorkflowID: 5}))
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: "2_protected", Type: portainer.StackResourceControl, StackPolicy: &portainer.StackAccessPolicy{}}))
	// No deployer or Git client is provided: denial must happen before any effects.
	err := RedeployWhenChanged(t.Context(), 1, nil, store, nil, nil)
	require.ErrorIs(t, err, security.ErrStackDockerAccessDenied)
}

func TestComposeDeploymentResolvesEnvironmentAdminFromPolicies(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	user := &portainer.User{Role: portainer.StandardUserRole}
	require.NoError(t, store.User().Create(user))
	endpoint := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, UserAccessPolicies: portainer.UserAccessPolicies{user.ID: {StandaloneRole: portainer.StandaloneEnvironmentAdmin}}}
	ctx := &security.RestrictedRequestContext{UserID: user.ID}
	config, err := CreateComposeStackDeploymentConfigTx(store, ctx, &portainer.Stack{}, endpoint, nil, nil, false, false, false)
	require.NoError(t, err)
	require.True(t, config.isAdmin)
	endpoint.UserAccessPolicies[user.ID] = portainer.AccessPolicy{StandaloneRole: portainer.StandaloneStandard}
	// Previously decorated response data must not restore a revoked admin grant.
	user.StandaloneEndpointRoles = map[portainer.EndpointID]portainer.StandaloneRole{1: portainer.StandaloneEnvironmentAdmin}
	require.NoError(t, store.User().Update(user.ID, user))
	config, err = CreateComposeStackDeploymentConfigTx(store, ctx, &portainer.Stack{}, endpoint, nil, nil, false, false, false)
	require.NoError(t, err)
	require.False(t, config.isAdmin)
}
