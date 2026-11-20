package vaultinject

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/stacks/vaultdeploy"
	"github.com/stretchr/testify/require"
)

func TestPrepareResolvesSelectedServiceAndReusesDeploymentSnapshot(t *testing.T) {
	t.Parallel()

	var logins, backendReads, workerReads atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			logins.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"auth":{"client_token":"synthetic-token"}}`))
		case "/v1/kv/data/backend":
			backendReads.Add(1)
			_, _ = w.Write([]byte(`{"data":{"data":{"password":"synthetic-backend-password"}}}`))
		case "/v1/kv/data/worker":
			workerReads.Add(1)
			_, _ = w.Write([]byte(`{"data":{"data":{"token":"must-not-load-for-selected-service"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	_, store := datastore.MustNewTestStore(t, true, false)
	const endpointID = 8101
	endpoint := &portainer.Endpoint{ID: endpointID, Name: "vault-injection-local", Type: portainer.DockerEnvironment, URL: "unix:///var/run/docker.sock"}
	require.NoError(t, store.Endpoint().Create(endpoint))
	parsed, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})
	address := server.URL
	require.NoError(t, vaultdeploy.SaveConnection(store, endpointID, portainer.VaultConnection{
		Address: address, RoleID: "synthetic-role-id", SecretID: "synthetic-secret-id", CAPEM: string(caPEM),
	}))

	projectPath := t.TempDir()
	const entrypoint = "compose.yaml"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, entrypoint), []byte(`services:
  backend:
    image: nginx:alpine
  worker:
    image: nginx:alpine
`), 0600))
	stack := &portainer.Stack{
		ID: 8101, EndpointID: endpointID, Type: portainer.DockerComposeStack,
		ProjectPath: projectPath, EntryPoint: entrypoint,
		VaultMappings: &portainer.StackVaultMappings{InjectOnDeploy: true, Mappings: []portainer.VaultMapping{
			{ID: "backend-password", Service: "backend", Variable: "DB_PASSWORD", Path: "kv/data/backend", Key: "password", Engine: "kv-v2"},
			{ID: "worker-token", Service: "worker", Variable: "API_TOKEN", Path: "kv/data/worker", Key: "token", Engine: "kv-v2"},
		}},
	}

	deployCtx, err := Prepare(context.Background(), store, stack, endpoint, []string{"backend"})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]string{"backend": {"DB_PASSWORD": "synthetic-backend-password"}}, Environment(deployCtx))
	require.EqualValues(t, 1, logins.Load())
	require.EqualValues(t, 1, backendReads.Load())
	require.Zero(t, workerReads.Load(), "selected-service deployment must not fetch another service's secret")

	// The same context is carried through apply/recovery so both attempts use the
	// same preflight snapshot even if Vault changes between Compose operations.
	retryCtx, err := Prepare(deployCtx, store, stack, endpoint, []string{"backend"})
	require.NoError(t, err)
	require.Equal(t, deployCtx, retryCtx)
	require.EqualValues(t, 1, backendReads.Load())
}

func TestPrepareLeavesDisabledMappingsUnusedAndRejectsInvalidTargetBeforeVault(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"auth":{"client_token":"synthetic-token"}}`))
	}))
	t.Cleanup(server.Close)
	_, store := datastore.MustNewTestStore(t, true, false)
	const endpointID = 8102
	endpoint := &portainer.Endpoint{ID: endpointID, Name: "vault-injection-preflight", Type: portainer.DockerEnvironment, URL: "unix:///var/run/docker.sock"}
	require.NoError(t, store.Endpoint().Create(endpoint))
	parsed, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})
	require.NoError(t, vaultdeploy.SaveConnection(store, endpointID, portainer.VaultConnection{
		Address: server.URL, RoleID: "role", SecretID: "secret", CAPEM: string(caPEM),
	}))
	projectPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  backend:\n    image: nginx:alpine\n"), 0600))
	stack := &portainer.Stack{
		ID: 8102, EndpointID: endpointID, Type: portainer.DockerComposeStack, ProjectPath: projectPath, EntryPoint: "compose.yaml",
		VaultMappings: &portainer.StackVaultMappings{Mappings: []portainer.VaultMapping{{
			ID: "backend-password", Service: "backend", Variable: "DB_PASSWORD", Path: "kv/data/backend", Key: "password", Engine: "kv-v2",
		}}},
	}

	unchangedCtx, err := Prepare(context.Background(), store, stack, endpoint, nil)
	require.NoError(t, err)
	require.Empty(t, Environment(unchangedCtx), "legacy stack mappings do not inject until explicitly enabled")
	require.Zero(t, requests.Load())

	stack.VaultMappings.InjectOnDeploy = true
	stack.VaultMappings.Mappings[0].Service = "removed-service"
	_, err = Prepare(context.Background(), store, stack, endpoint, nil)
	require.ErrorContains(t, err, "select a service declared")
	require.Zero(t, requests.Load(), "all mapping references are checked before contacting Vault")
}

func TestSupportedEndpointIncludesLocalAndStandardAgentOnly(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		endpoint *portainer.Endpoint
		want     bool
	}{
		{"local unix", &portainer.Endpoint{Type: portainer.DockerEnvironment, URL: "unix:///var/run/docker.sock"}, true},
		{"local windows", &portainer.Endpoint{Type: portainer.DockerEnvironment, URL: "npipe:////./pipe/docker_engine"}, true},
		{"standard agent", &portainer.Endpoint{Type: portainer.AgentOnDockerEnvironment, URL: "tcp://agent:9001"}, true},
		{"raw tcp", &portainer.Endpoint{Type: portainer.DockerEnvironment, URL: "tcp://docker:2376"}, false},
		{"edge agent", &portainer.Endpoint{Type: portainer.EdgeAgentOnDockerEnvironment, URL: "tcp://edge:8000"}, false},
		{"nil endpoint", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, SupportedEndpoint(test.endpoint))
		})
	}
}
