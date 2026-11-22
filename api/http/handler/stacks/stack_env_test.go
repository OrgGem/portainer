package stacks

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/pkg/fips"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStackEnv(t *testing.T) {
	t.Parallel()

	existing := []portainer.Pair{
		{Name: "DB_HOST", Value: "postgres"},
		{Name: "DB_PASS", Value: "super-secret"},
	}

	uiReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPut, "/stacks/1", nil)
		req.AddCookie(&http.Cookie{Name: portainer.AuthCookieKey, Value: "jwt-cookie"})
		return req
	}

	apiKeyReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPut, "/stacks/1", nil)
		req.Header.Set(portainer.APIKeyHeader, "ptr_test_key")
		return req
	}

	bearerReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPut, "/stacks/1", nil)
		req.Header.Set("Authorization", "Bearer test-jwt")
		return req
	}

	t.Run("preserves existing env when incoming is nil", func(t *testing.T) {
		t.Parallel()
		got := resolveStackEnv(uiReq(), existing, nil, false, false)
		assert.Equal(t, existing, got)
	})

	t.Run("clears env on UI cookie request when incoming is empty slice", func(t *testing.T) {
		t.Parallel()
		got := resolveStackEnv(uiReq(), existing, []portainer.Pair{}, false, false)
		assert.Empty(t, got)
	})

	t.Run("preserves existing env on X-API-KEY request when incoming is empty slice", func(t *testing.T) {
		t.Parallel()
		got := resolveStackEnv(apiKeyReq(), existing, []portainer.Pair{}, false, false)
		assert.Equal(t, existing, got)
	})

	t.Run("preserves existing env on Authorization Bearer request when incoming is empty slice", func(t *testing.T) {
		t.Parallel()
		got := resolveStackEnv(bearerReq(), existing, []portainer.Pair{}, false, false)
		assert.Equal(t, existing, got)
	})

	t.Run("preserves existing env on git redeploy when incoming is empty slice", func(t *testing.T) {
		t.Parallel()
		got := resolveStackEnv(uiReq(), existing, []portainer.Pair{}, false, true)
		assert.Equal(t, existing, got)
	})

	t.Run("clears env on API key request when clearEnv is explicitly true", func(t *testing.T) {
		t.Parallel()
		got := resolveStackEnv(apiKeyReq(), existing, []portainer.Pair{}, true, true)
		assert.Empty(t, got)
	})

	t.Run("restores stored values when incoming contains MCP [REDACTED] placeholders", func(t *testing.T) {
		t.Parallel()
		incoming := []portainer.Pair{
			{Name: "DB_HOST", Value: "postgres-v2"},
			{Name: "DB_PASS", Value: redactedEnvPlaceholder},
			{Name: "NEW_VAR", Value: "enabled"},
		}

		got := resolveStackEnv(apiKeyReq(), existing, incoming, false, false)
		expected := []portainer.Pair{
			{Name: "DB_HOST", Value: "postgres-v2"},
			{Name: "DB_PASS", Value: "super-secret"},
			{Name: "NEW_VAR", Value: "enabled"},
		}
		assert.Equal(t, expected, got)
	})
}

func Test_updateStackInTx_PreservesEnvOnAPIUpdate(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	initialEnv := []portainer.Pair{
		{Name: "APP_ENV", Value: "production"},
		{Name: "SECRET_TOKEN", Value: "s3cr3t"},
	}

	t.Run("omitting Env in Compose stack update preserves existing Env", func(t *testing.T) {
		t.Parallel()

		payload := &updateComposeStackPayload{
			StackFileContent: "version: '3'\nservices:\n  web:\n    image: nginx:1.27",
		}
		stack := &portainer.Stack{
			ID:         1,
			Name:       "compose-preserve-env",
			EntryPoint: "docker-compose.yml",
			Type:       portainer.DockerComposeStack,
			Env:        initialEnv,
		}
		setup := setupUpdateStackInTxTest(t, stack, payload)

		err := setup.store.UpdateTx(func(tx dataservices.DataStoreTx) error {
			var handlerErr *httperror.HandlerError
			_, _, _, handlerErr = setup.handler.updateStackInTx(tx, setup.req, setup.stack.ID, setup.endpoint.ID)
			if handlerErr != nil {
				return handlerErr
			}
			return nil
		})
		require.NoError(t, err)

		stored, err := setup.store.Stack().Read(setup.stack.ID)
		require.NoError(t, err)
		assert.Equal(t, initialEnv, stored.Env)
	})

	t.Run("MCP X-API-KEY request sending empty Env array preserves existing Env", func(t *testing.T) {
		t.Parallel()

		payload := &updateComposeStackPayload{
			StackFileContent: "version: '3'\nservices:\n  web:\n    image: nginx:1.27",
			Env:              []portainer.Pair{},
		}
		stack := &portainer.Stack{
			ID:         1,
			Name:       "compose-mcp-empty-env",
			EntryPoint: "docker-compose.yml",
			Type:       portainer.DockerComposeStack,
			Env:        initialEnv,
		}
		setup := setupUpdateStackInTxTest(t, stack, payload)
		setup.req.Header.Set(portainer.APIKeyHeader, "ptr_mcp_token")

		err := setup.store.UpdateTx(func(tx dataservices.DataStoreTx) error {
			var handlerErr *httperror.HandlerError
			_, _, _, handlerErr = setup.handler.updateStackInTx(tx, setup.req, setup.stack.ID, setup.endpoint.ID)
			if handlerErr != nil {
				return handlerErr
			}
			return nil
		})
		require.NoError(t, err)

		stored, err := setup.store.Stack().Read(setup.stack.ID)
		require.NoError(t, err)
		assert.Equal(t, initialEnv, stored.Env)
	})

	t.Run("UI request sending empty Env array clears existing Env", func(t *testing.T) {
		t.Parallel()

		rawBody := []byte(`{"StackFileContent":"version: '3'\nservices:\n  web:\n    image: nginx:1.27","Env":[]}`)
		stack := &portainer.Stack{
			ID:         1,
			Name:       "compose-ui-clear-env",
			EntryPoint: "docker-compose.yml",
			Type:       portainer.DockerComposeStack,
			Env:        initialEnv,
		}
		setup := setupUpdateStackInTxTest(t, stack, &updateComposeStackPayload{
			StackFileContent: "version: '3'\nservices:\n  web:\n    image: nginx:1.27",
		})
		setup.req = mockCreateStackRequestWithSecurityContext(
			http.MethodPut,
			fmt.Sprintf("/stacks/%d?endpointId=%d", setup.stack.ID, setup.endpoint.ID),
			bytes.NewBuffer(rawBody),
		)

		err := setup.store.UpdateTx(func(tx dataservices.DataStoreTx) error {
			var handlerErr *httperror.HandlerError
			_, _, _, handlerErr = setup.handler.updateStackInTx(tx, setup.req, setup.stack.ID, setup.endpoint.ID)
			if handlerErr != nil {
				return handlerErr
			}
			return nil
		})
		require.NoError(t, err)

		stored, err := setup.store.Stack().Read(setup.stack.ID)
		require.NoError(t, err)
		assert.Empty(t, stored.Env)
	})

	t.Run("omitting Env in Swarm stack update preserves existing Env", func(t *testing.T) {
		t.Parallel()

		payload := &updateSwarmStackPayload{
			StackFileContent: "version: '3'\nservices:\n  web:\n    image: nginx:1.27",
		}
		stack := &portainer.Stack{
			ID:         1,
			Name:       "swarm-preserve-env",
			EntryPoint: "docker-compose.yml",
			Type:       portainer.DockerSwarmStack,
			Env:        initialEnv,
		}
		setup := setupUpdateStackInTxTest(t, stack, payload)
		setup.handler.SwarmStackManager = swarmStackManager{}

		err := setup.store.UpdateTx(func(tx dataservices.DataStoreTx) error {
			var handlerErr *httperror.HandlerError
			_, _, _, handlerErr = setup.handler.updateStackInTx(tx, setup.req, setup.stack.ID, setup.endpoint.ID)
			if handlerErr != nil {
				return handlerErr
			}
			return nil
		})
		require.NoError(t, err)

		stored, err := setup.store.Stack().Read(setup.stack.ID)
		require.NoError(t, err)
		assert.Equal(t, initialEnv, stored.Env)
	})
}
