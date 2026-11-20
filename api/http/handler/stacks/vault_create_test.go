package stacks

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/stacks/vaultdeploy"
	"github.com/stretchr/testify/require"
)

func TestInitialVaultMappingsRequireAdministratorAndSupportedEndpoint(t *testing.T) {
	handler, _, _, endpoint := managedFixture(t)
	input := &portainer.StackVaultMappings{
		InjectOnDeploy: true,
		Mappings: []portainer.VaultMapping{{
			Service: "web", Variable: "DB_PASSWORD", Path: "kv/data/apps/web", Key: "password", Engine: "kv-v2",
		}},
	}
	compose := []byte("services:\n  web:\n    image: nginx\n")

	admin := &security.RestrictedRequestContext{IsAdmin: true, UserID: 2}
	created, herr := handler.initialVaultMappings(admin, endpoint, input, compose)
	require.Nil(t, herr)
	require.NotEmpty(t, created.Mappings[0].ID)
	require.Equal(t, 1, created.Version)
	require.True(t, created.InjectOnDeploy)
	require.Equal(t, portainer.UserID(2), created.UpdatedBy)

	reader := &security.RestrictedRequestContext{UserID: 2, StandaloneRole: portainer.StandaloneReadOnly}
	_, herr = handler.initialVaultMappings(reader, endpoint, input, compose)
	require.NotNil(t, herr, "regular users cannot inject caller-provided mapping data into a newly created stack")

	unsupported := *endpoint
	unsupported.URL = "tcp://docker.example:2376"
	_, herr = handler.initialVaultMappings(admin, &unsupported, input, compose)
	require.NotNil(t, herr, "TCP Docker endpoints are outside the v1 injection scope")

	input.InjectOnDeploy = false
	created, herr = handler.initialVaultMappings(admin, &unsupported, input, compose)
	require.Nil(t, herr, "reference-only mappings remain compatible on unsupported deploy endpoints")
	require.False(t, created.InjectOnDeploy)
}

type vaultRetryDeploymentProbe struct {
	prepareErr error
	prepared   bool
	undeployed bool
	deployed   bool
}

func (probe *vaultRetryDeploymentProbe) PrepareVault(ctx context.Context) (context.Context, error) {
	probe.prepared = true
	return ctx, probe.prepareErr
}

func (probe *vaultRetryDeploymentProbe) Undeploy(context.Context) error {
	probe.undeployed = true
	return nil
}

func (probe *vaultRetryDeploymentProbe) Deploy(context.Context) error {
	probe.deployed = true
	return nil
}

func (*vaultRetryDeploymentProbe) GetResponse() string { return "" }

func TestRetryVaultPreflightFailureDoesNotUndeployExistingStack(t *testing.T) {
	_, store, stack, _ := managedFixture(t)
	stack.Status = portainer.StackStatusDeploying
	stack.DeploymentStartStatus = portainer.StackStatusError
	require.NoError(t, store.Stack().Update(stack.ID, stack))
	probe := &vaultRetryDeploymentProbe{prepareErr: errors.New("synthetic Vault unavailable")}

	stackDeploy(store, stack.ID, probe, nil)

	require.True(t, probe.prepared)
	require.False(t, probe.undeployed, "Vault must be readable before retry removes containers")
	require.False(t, probe.deployed, "the failed Vault preflight must stop the retry")
	stored, err := store.Stack().Read(stack.ID)
	require.NoError(t, err)
	require.Equal(t, portainer.StackStatusError, stored.Status)
}

func TestConfigurationApplyAndRecoveryReuseVaultSnapshot(t *testing.T) {
	h, store, stack, endpoint := managedFixture(t)
	const secret = "synthetic-apply-password"
	var loginCount, readCount atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/approle/login":
			loginCount.Add(1)
			_, _ = w.Write([]byte(`{"auth":{"client_token":"synthetic-token"}}`))
		case "/v1/kv/data/apps/web":
			readCount.Add(1)
			_, _ = w.Write([]byte(`{"data":{"data":{"password":"synthetic-apply-password"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	parsed, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})
	require.NoError(t, vaultdeploy.SaveConnection(store, endpoint.ID, portainer.VaultConnection{
		Address: server.URL, RoleID: "synthetic-role-id", SecretID: "synthetic-secret-id", CAPEM: string(caPEM),
	}))

	stack.VaultMappings = &portainer.StackVaultMappings{InjectOnDeploy: true, Mappings: []portainer.VaultMapping{{
		ID: "web-password", Service: "web", Variable: "DB_PASSWORD", Path: "kv/data/apps/web", Key: "password", Engine: "kv-v2",
	}}}
	stack.Configuration.Targets[0].ActiveRevision = "old"
	stack.Configuration.Revisions = []portainer.StackConfigRevision{
		{ID: "old", TargetID: "web-config", AppliedAt: 1},
		{ID: "new", TargetID: "web-config"},
	}
	require.NoError(t, store.Stack().Update(stack.ID, stack))

	var observed []string
	manager := &configApplyManager{failures: []error{errors.New("synthetic Compose failure")}}
	manager.before = func(_ *portainer.Stack, options portainer.ComposeUpOptions) {
		require.Equal(t, secret, options.VaultEnv["web"]["DB_PASSWORD"])
		observed = append(observed, options.VaultEnv["web"]["DB_PASSWORD"])
	}
	h.ComposeStackManager = manager
	deployErr := h.applyConfigurationRevision(stack, endpoint, &security.RestrictedRequestContext{UserID: 2}, 1, 0, "apply", map[string]string{"web": "sha256:pinned"})
	require.NotNil(t, deployErr)
	require.Equal(t, http.StatusInternalServerError, deployErr.StatusCode)
	require.NotContains(t, deployErr.Error(), secret)
	require.Equal(t, []string{secret, secret}, observed, "apply and automatic recovery must use the same in-memory Vault snapshot")
	require.EqualValues(t, 1, loginCount.Load())
	require.EqualValues(t, 1, readCount.Load())
	stored, err := store.Stack().Read(stack.ID)
	require.NoError(t, err)
	require.Equal(t, "old", stored.Configuration.Targets[0].ActiveRevision)
	require.Empty(t, stored.Configuration.PendingRevision)
}
