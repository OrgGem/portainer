package security

import (
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/stretchr/testify/require"
)

func TestStackBoundaryBlocksBypasses(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: "1_app", Type: portainer.StackResourceControl, StackPolicy: &portainer.StackAccessPolicy{}}))
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/v1.47/containers/json", true}, {"POST", "/v1.47/containers/create", false}, {"POST", "/containers/a/exec", false},
		{"GET", "/containers/a/attach/ws", false}, {"GET", "/containers/a/archive", false}, {"PUT", "/containers/a/archive", false},
		{"GET", "/websocket/exec", false}, {"POST", "/images/create", false}, {"POST", "/networks/a/connect", false}, {"POST", "/volumes/prune", false},
		{"POST", "/build", false}, {"GET", "/v2/browse", false}, {"POST", "/stacks/create/standalone/string", false},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r = r.WithContext(StoreTokenData(r, &portainer.TokenData{ID: 2, Role: portainer.StandardUserRole}))
		err := CheckStackDockerAccess(r, store, 1)
		require.Equal(t, tc.allowed, err == nil, tc.path)
		require.NoError(t, CheckStackDockerAccess(r, store, 2), "unprotected endpoint remains unchanged")
		r = r.WithContext(StoreTokenData(r, &portainer.TokenData{ID: 1, Role: portainer.AdministratorRole}))
		require.NoError(t, CheckStackDockerAccess(r, store, 1))
	}
}

func TestStackBoundaryContextIsEndpointBound(t *testing.T) {
	r := httptest.NewRequest("POST", "/stacks/1/start", nil)
	require.False(t, isControlledStackRoute(r, 1))
	r = WithAuthorizedStackOperation(r, 1)
	require.True(t, isControlledStackRoute(r, 1))
	require.False(t, isControlledStackRoute(r, 2))
}
