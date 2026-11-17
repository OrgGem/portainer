package docker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/stretchr/testify/require"
)

func TestReviewCommitAndImageExport(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	endpoint := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, UserAccessPolicies: portainer.UserAccessPolicies{7: {StandaloneRole: portainer.StandaloneStandard}}}
	require.NoError(t, store.Endpoint().Create(endpoint))
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: "private-container", Type: portainer.ContainerResourceControl, AdministratorsOnly: true}))
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
		w.Write([]byte("synthetic response"))
	}))
	defer srv.Close()
	transport := &Transport{endpoint: endpoint, dataStore: store, HTTPTransport: &http.Transport{}}
	call := func(method, path string) {
		r := httptest.NewRequest(method, srv.URL+path, nil)
		r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: 7, Role: portainer.StandardUserRole}))
		response, err := transport.proxyDockerRequest(r, false)
		require.NoError(t, err)
		require.Equal(t, 403, response.StatusCode)
		response.Body.Close()
	}
	call("POST", "/commit?container=private-container")
	require.Equal(t, 0, hits)
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: fmt.Sprintf("%d_protected", endpoint.ID), Type: portainer.StackResourceControl, StackPolicy: &portainer.StackAccessPolicy{}}))
	call("GET", "/images/get")
	call("GET", "/images/sha256:abc/get")
	require.Equal(t, 0, hits)
}
