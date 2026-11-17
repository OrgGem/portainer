package containers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	dockerclient "github.com/portainer/portainer/api/docker/client"
	"github.com/portainer/portainer/api/http/security"
	"github.com/stretchr/testify/require"
)

func TestContainerAccessResolvesCanonicalAndInheritedOwnership(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.50")
			w.Write([]byte("OK"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Id":"canonical-id","Config":{"Labels":{"com.docker.compose.project":"private"}},"HostConfig":{}}`))
	}))
	defer srv.Close()
	ep := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, URL: srv.URL, UserAccessPolicies: portainer.UserAccessPolicies{7: {StandaloneRole: portainer.StandaloneStandard}}}
	require.NoError(t, store.Endpoint().Create(ep))
	rc := &portainer.ResourceControl{ResourceID: "1_private", Type: portainer.StackResourceControl, AdministratorsOnly: true}
	require.NoError(t, store.ResourceControl().Create(rc))
	h := &Handler{dataStore: store, dockerClientFactory: dockerclient.NewClientFactory(nil, nil)}
	r := httptest.NewRequest("GET", "/docker/1/containers/short/gpus", nil)
	r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: 7, Role: portainer.StandardUserRole}))
	r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{UserID: 7}))
	_, denied := h.authorizeContainer(r, ep, "short")
	require.NotNil(t, denied)
	require.Equal(t, 403, denied.StatusCode)
	require.NoError(t, store.ResourceControl().Delete(rc.ID))
	rc = &portainer.ResourceControl{ResourceID: "canonical-id", Type: portainer.ContainerResourceControl, AdministratorsOnly: true}
	require.NoError(t, store.ResourceControl().Create(rc))
	_, denied = h.authorizeContainer(r, ep, "short")
	require.NotNil(t, denied)
	require.Equal(t, 403, denied.StatusCode)
	ep.UserAccessPolicies[7] = portainer.AccessPolicy{StandaloneRole: portainer.StandaloneEnvironmentAdmin}
	require.NoError(t, store.Endpoint().UpdateEndpoint(ep.ID, ep))
	canonicalID, denied := h.authorizeContainer(r, ep, "short")
	require.Nil(t, denied)
	require.Equal(t, "canonical-id", canonicalID)
}

func TestEnvironmentAdminCannotRecreateManagedBuilder(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.50")
			w.Write([]byte("OK"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Id":"canonical-id","Name":"/buildx_buildkit_portainer-compose-test","Config":{"Labels":{}},"HostConfig":{}}`))
	}))
	defer srv.Close()
	ep := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, URL: srv.URL, UserAccessPolicies: portainer.UserAccessPolicies{7: {StandaloneRole: portainer.StandaloneEnvironmentAdmin}}}
	require.NoError(t, store.Endpoint().Create(ep))
	h := &Handler{dataStore: store, dockerClientFactory: dockerclient.NewClientFactory(nil, nil)}
	for _, admin := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/docker/1/containers/short/recreate", nil)
		r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: 7, Role: portainer.StandardUserRole}))
		r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{UserID: 7, IsAdmin: admin}))
		_, denied := h.authorizeContainer(r, ep, "short")
		if admin {
			require.Nil(t, denied)
		} else {
			require.NotNil(t, denied)
			require.Equal(t, 403, denied.StatusCode)
		}
	}
}
