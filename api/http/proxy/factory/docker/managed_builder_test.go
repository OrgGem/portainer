package docker

import (
	"github.com/docker/docker/api/types/container"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedBuilderProtectsWritesWithoutBlockingApplicationOperations(t *testing.T) {
	_, ds := datastore.MustNewTestStore(t, true, false)
	marker := &container.Config{Env: []string{"PORTAINER_MANAGED_COMPOSE_BUILDER=1"}}
	routes := RoutesDefinition{
		{"GET", "/containers/short/json"}:    container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{ID: "canonical", Name: "/renamed"}, Config: marker},
		{"GET", "/containers/app/json"}:      container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{ID: "app", Name: "/app"}, Config: &container.Config{}},
		{"POST", "/containers/app/update"}:   struct{}{},
		{"POST", "/containers/short/update"}: struct{}{},
		{"GET", "/containers/short/stats"}:   struct{}{},
	}
	srv, version := mockDockerAPIServer(t, routes)
	defer srv.Close()
	ep := &portainer.Endpoint{ID: 1, URL: srv.URL, Type: portainer.DockerEnvironment, UserAccessPolicies: portainer.UserAccessPolicies{7: {StandaloneRole: portainer.StandaloneOperator}}}
	require.NoError(t, ds.Endpoint().Create(ep))
	transport := &Transport{endpoint: ep, dataStore: ds, HTTPTransport: &http.Transport{}}
	for _, tc := range []struct {
		method, path, id string
		admin            bool
		status           int
	}{
		{"POST", "/containers/short/update", "short", false, 403},
		{"POST", "/containers/short/exec", "short", false, 403},
		{"POST", "/exec/existing/start", "short", false, 403},
		{"DELETE", "/containers/short", "short", false, 403},
		{"GET", "/containers/short/attach/ws", "short", false, 403},
		{"POST", "/containers/app/update", "app", false, 200},
		{"GET", "/containers/short/stats", "short", false, 200},
		{"POST", "/containers/short/update", "short", true, 200},
	} {
		t.Run(tc.method+tc.path+http.StatusText(tc.status), func(t *testing.T) {
			role := portainer.StandardUserRole
			if tc.admin {
				role = portainer.AdministratorRole
			}
			req := httptest.NewRequest(tc.method, srv.URL+"/v"+version+tc.path, nil)
			req = req.WithContext(security.StoreTokenData(req, &portainer.TokenData{ID: 7, Role: role}))
			resp, err := transport.restrictedResourceOperation(req, tc.id, tc.id, portainer.ContainerResourceControl, false)
			require.NoError(t, err)
			require.Equal(t, tc.status, resp.StatusCode)
			resp.Body.Close()
		})
	}
}
