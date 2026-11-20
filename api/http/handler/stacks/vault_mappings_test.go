package stacks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
)

const vaultMappingPayload = `{"Version":0,"Mappings":[{"ID":"reference","Service":"web","Variable":"DB_PASSWORD","Path":"kv/data/apps/demo","Key":"password","Engine":"kv-v2"}]}`

func TestVaultMappingCRUDAndReferenceOnlyResponses(t *testing.T) {
	h, store, stack, _ := managedFixture(t)
	stack.Env = []portainer.Pair{{Name: "PRIVATE_ENV", Value: "must-never-appear"}}
	require.NoError(t, store.Stack().Update(stack.ID, stack))
	call := func(method, path, payload string, admin bool, status int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, operatorRequest(method, path, payload, admin))
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	call("PUT", "/stacks/1/vault", vaultMappingPayload, false, http.StatusForbidden)
	w := call("PUT", "/stacks/1/vault", vaultMappingPayload, true, http.StatusOK)
	var config portainer.StackVaultMappings
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &config))
	require.Equal(t, 1, config.Version)
	require.Equal(t, portainer.UserID(2), config.UpdatedBy)
	call("PUT", "/stacks/1/vault", vaultMappingPayload, true, http.StatusConflict)
	for _, body := range []string{
		`{"Version":1,"Value":"must-never-appear","Mappings":[]}`,
		`{"Version":1,"Mappings":[{"Service":"web","Variable":"DB_PASSWORD","Path":"kv/data/apps/demo","Key":"password","Engine":"kv-v2","Token":"must-never-appear"}]}`,
	} {
		w = call("PUT", "/stacks/1/vault", body, true, http.StatusBadRequest)
		require.NotContains(t, w.Body.String(), "must-never-appear")
	}
	for _, url := range []string{"/stacks/1/vault", "/stacks/1/vault/template?service=web", "/stacks/vault/mappings"} {
		w = call("GET", url, "", true, http.StatusOK)
		require.NotContains(t, w.Body.String(), "must-never-appear")
		require.NotContains(t, w.Body.String(), "PRIVATE_ENV")
		require.Contains(t, w.Body.String(), "kv/data/apps/demo")
	}
	call("GET", "/stacks/1/vault/template?service=missing", "", true, http.StatusBadRequest)
	call("PUT", "/stacks/1/vault", `{"Version":1,"Mappings":[]}`, true, http.StatusOK)
	w = call("GET", "/stacks/1/vault", "", true, http.StatusOK)
	require.NotContains(t, w.Body.String(), "DB_PASSWORD")
}

func TestVaultInventoryFiltersCountsPaginationAndAccess(t *testing.T) {
	h, store, stack, endpoint := managedFixture(t)
	endpoint.UserAccessPolicies = portainer.UserAccessPolicies{2: {StandaloneRole: portainer.StandaloneReadOnly}}
	require.NoError(t, store.Endpoint().UpdateEndpoint(endpoint.ID, endpoint))
	mappings := []portainer.VaultMapping{{ID: "a", Service: "web", Variable: "DB_PASSWORD", Path: "kv/data/apps/demo", Key: "password", Engine: "kv-v2"}, {ID: "b", Service: "worker", Variable: "API_TOKEN", Path: "kv/data/apps/worker", Key: "token", Engine: "kv-v2"}}
	stack.VaultMappings = &portainer.StackVaultMappings{Version: 1, Mappings: mappings}
	require.NoError(t, store.Stack().Update(stack.ID, stack))
	private := &portainer.Stack{ID: 2, EndpointID: endpoint.ID, Name: "private-stack", Type: portainer.DockerComposeStack, VaultMappings: &portainer.StackVaultMappings{Mappings: []portainer.VaultMapping{{ID: "private", Service: "private-service", Variable: "PRIVATE_VARIABLE", Path: "kv/data/private", Key: "private"}}}}
	require.NoError(t, store.Stack().Create(private))
	require.NoError(t, store.ResourceControl().Create(authorization.NewPrivateResourceControl("1_private-stack", portainer.StackResourceControl, 99)))
	otherEndpoint := &portainer.Endpoint{ID: 3, Name: "private-environment", Type: portainer.DockerEnvironment}
	require.NoError(t, store.Endpoint().Create(otherEndpoint))
	privateOther := *private
	privateOther.ID, privateOther.EndpointID = 3, 3
	require.NoError(t, store.Stack().Create(&privateOther))
	for _, tc := range []struct {
		query       string
		total, rows int
	}{
		{"", 2, 2}, {"?limit=1", 2, 1}, {"?offset=1&limit=1", 2, 1},
		{"?variable=password&path=APPS&key=pass&service=WEB&environmentId=1&stackId=1", 1, 1},
		{"?environmentId=3", 0, 0}, {"?stackId=2", 0, 0}, {"?path=private", 0, 0},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, operatorRequest("GET", "/stacks/vault/mappings"+tc.query, "", false))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "private", "filters and counts must not leak unauthorized references")
		var result vaultInventoryResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, tc.total, result.Summary.Mappings, tc.query)
		require.Len(t, result.Items, tc.rows, tc.query)
		require.Len(t, result.Environments, 1)
		require.Len(t, result.Stacks, 1)
	}
	for _, query := range []string{"?limit=0", "?limit=201", "?offset=-1", "?environmentId=nan"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, operatorRequest("GET", "/stacks/vault/mappings"+query, "", false))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	control, err := store.ResourceControl().ResourceControlByResourceIDAndType("1_app", portainer.StackResourceControl)
	require.NoError(t, err)
	control.StackPolicy.Grants = nil
	require.NoError(t, store.ResourceControl().Update(control.ID, control))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("GET", "/stacks/vault/mappings", "", false))
	var result vaultInventoryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Zero(t, result.Summary.Mappings)
	require.Empty(t, result.Stacks)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("GET", "/stacks/1/vault", "", false))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestVaultMappingsCanRemoveStaleServicesOneAtATime(t *testing.T) {
	h, store, stack, _ := managedFixture(t)
	stack.VaultMappings = &portainer.StackVaultMappings{Version: 2, Mappings: []portainer.VaultMapping{
		{ID: "old-a", Service: "removed-a", Variable: "PASSWORD", Path: "kv/data/a", Key: "password", Engine: "kv-v2"},
		{ID: "old-b", Service: "removed-b", Variable: "PASSWORD", Path: "kv/data/b", Key: "password", Engine: "kv-v2"},
	}}
	require.NoError(t, store.Stack().Update(stack.ID, stack))
	remaining := stack.VaultMappings.Mappings[1:]
	body, err := json.Marshal(map[string]any{"Version": 2, "Mappings": remaining})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("PUT", "/stacks/1/vault", string(body), true))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	remaining[0].Path = "kv/data/changed"
	body, err = json.Marshal(map[string]any{"Version": 3, "Mappings": remaining})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("PUT", "/stacks/1/vault", string(body), true))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestVaultInjectionToggleIsVersionedAndRequiresSupportedEndpoint(t *testing.T) {
	h, store, _, endpoint := managedFixture(t)
	call := func(method, path, payload string, status int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, operatorRequest(method, path, payload, true))
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}

	w := call("PUT", "/stacks/1/vault", `{"Version":0,"InjectOnDeploy":true,"Mappings":[{"ID":"database-password","Service":"web","Variable":"DB_PASSWORD","Path":"kv/data/apps/web","Key":"password","Engine":"kv-v2"}]}`, http.StatusOK)
	var config portainer.StackVaultMappings
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &config))
	require.True(t, config.InjectOnDeploy)
	require.Equal(t, 1, config.Version)

	w = call("GET", "/stacks/1/vault", "", http.StatusOK)
	var state vaultStackResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &state))
	require.True(t, state.InjectionSupported)

	// An ordinary mapping edit that omits the optional toggle preserves the
	// existing deploy behavior.
	w = call("PUT", "/stacks/1/vault", `{"Version":1,"Mappings":[{"ID":"database-password","Service":"web","Variable":"DB_PASSWORD","Path":"kv/data/apps/web","Key":"password","Engine":"kv-v2"}]}`, http.StatusOK)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &config))
	require.True(t, config.InjectOnDeploy)

	endpoint.Type = portainer.DockerEnvironment
	endpoint.URL = "tcp://docker.example:2376"
	require.NoError(t, store.Endpoint().UpdateEndpoint(endpoint.ID, endpoint))
	w = call("GET", "/stacks/1/vault", "", http.StatusOK)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &state))
	require.False(t, state.InjectionSupported)

	// Unsupported environments must still be able to disable an existing
	// mapping, then save reference-only changes.
	w = call("PUT", "/stacks/1/vault", `{"Version":2,"InjectOnDeploy":true,"Mappings":[]}`, http.StatusBadRequest)
	w = call("PUT", "/stacks/1/vault", `{"Version":2,"InjectOnDeploy":false,"Mappings":[]}`, http.StatusOK)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &config))
	require.False(t, config.InjectOnDeploy)
}
