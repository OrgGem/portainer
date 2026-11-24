package registries

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/http/security"

	"github.com/segmentio/encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_registryUpdate(t *testing.T) {
	t.Parallel()

	handler, store := newTestHandler(t)

	registry := &portainer.Registry{Type: portainer.ProGetRegistry}

	err := store.Registry().Create(registry)
	require.NoError(t, err)

	payload := registryUpdatePayload{
		Name:           new("Updated test registry"),
		URL:            new("http://example.org/feed"),
		BaseURL:        new("http://example.org"),
		Authentication: new(true),
		Username:       new("username"),
		Password:       new("password"),
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodPut, "/registries/1", bytes.NewReader(payloadBytes))
	w := httptest.NewRecorder()

	restrictedContext := &security.RestrictedRequestContext{IsAdmin: true, UserID: 1}

	ctx := security.StoreRestrictedRequestContext(r, restrictedContext)
	r = r.WithContext(ctx)

	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	updatedRegistry := portainer.Registry{}
	err = json.NewDecoder(w.Body).Decode(&updatedRegistry)
	require.NoError(t, err)

	// Registry type should remain intact
	assert.Equal(t, registry.Type, updatedRegistry.Type)

	assert.Equal(t, *payload.Name, updatedRegistry.Name)
	assert.Equal(t, *payload.URL, updatedRegistry.URL)
	assert.Equal(t, *payload.BaseURL, updatedRegistry.BaseURL)
	assert.Equal(t, *payload.Authentication, updatedRegistry.Authentication)
	assert.Equal(t, *payload.Username, updatedRegistry.Username)
	assert.Empty(t, updatedRegistry.Password)
}

func TestECRRoleLifecycle(t *testing.T) {
	handler, store := newTestHandler(t)
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{IsAdmin: true, UserID: 1}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	request("POST", "/registries", `{"Name":"shared","Type":7,"URL":"123456789012.dkr.ecr.us-east-1.amazonaws.com","Authentication":true,"Ecr":{"Region":"us-east-1","UseIAMRole":true}}`, 200)
	load := func() *portainer.Registry {
		r, err := store.Registry().Read(1)
		require.NoError(t, err)
		return r
	}
	reg := load()
	require.True(t, reg.Ecr.UseIAMRole)
	reg.AccessToken, reg.AccessTokenExpiry = "AWS:cached-password", 12345
	require.NoError(t, store.Registry().Update(1, reg))

	w := request("PUT", "/registries/1", `{"Name":"renamed","Ecr":{"Region":"us-east-1"}}`, 200)
	require.NotContains(t, w.Body.String(), "cached-password")
	require.True(t, load().Ecr.UseIAMRole)
	require.Equal(t, reg.AccessToken, load().AccessToken)
	request("PUT", "/registries/1", `{"Ecr":{"UseIAMRole":false}}`, 400)
	require.True(t, load().Ecr.UseIAMRole)
	request("PUT", "/registries/1", `{"Username":"key","Password":"secret","Ecr":{"UseIAMRole":false}}`, 200)
	reg = load()
	require.False(t, reg.Ecr.UseIAMRole)
	require.Equal(t, "secret", reg.Password)
	require.Empty(t, reg.AccessToken)
	reg.AccessToken, reg.AccessTokenExpiry = "AWS:from-static-key", 12345
	require.NoError(t, store.Registry().Update(1, reg))
	request("PUT", "/registries/1", `{"Ecr":{"UseIAMRole":true}}`, 200)
	reg = load()
	require.True(t, reg.Ecr.UseIAMRole)
	require.Empty(t, reg.Username)
	require.Empty(t, reg.Password)
	require.Empty(t, reg.ManagementConfiguration.Password)
	require.Empty(t, reg.AccessToken)
	require.Zero(t, reg.AccessTokenExpiry)
	request("PUT", "/registries/1", `{"URL":"example.com"}`, 400)
	request("PUT", "/registries/1", `{"Authentication":false}`, 200)
	require.False(t, load().Ecr.UseIAMRole)
}
