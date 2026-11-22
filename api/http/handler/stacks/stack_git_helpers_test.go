package stacks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/dataservices/source"
	"github.com/portainer/portainer/api/datastore"
	gittypes "github.com/portainer/portainer/api/git/types"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/portainer/portainer/api/stacks/stackutils"
	"github.com/portainer/portainer/pkg/fips"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/segmentio/encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStackGitService struct {
	portainer.GitService
	latestCommitErr error
	lastUser        string
	lastPass        string
	callCount       int
}

func (m *mockStackGitService) LatestCommitID(_ context.Context, _, _, username, password string, _ bool) (string, error) {
	m.callCount++
	m.lastUser = username
	m.lastPass = password
	if m.latestCommitErr != nil {
		return "", m.latestCommitErr
	}
	return "abc123def456", nil
}

func setupGitStackUpdateTest(t *testing.T, gitMock portainer.GitService) (*Handler, *datastore.Store, *portainer.Stack, portainer.SourceID, *portainer.Endpoint) {
	t.Helper()

	_, store := datastore.MustNewTestStore(t, false, false)

	user := &portainer.User{ID: 1, Username: "admin", Role: portainer.AdministratorRole}
	require.NoError(t, store.User().Create(user))

	endpoint := &portainer.Endpoint{
		ID:   10,
		Name: "docker-env",
		Type: portainer.DockerEnvironment,
	}
	require.NoError(t, store.Endpoint().Create(endpoint))

	src := &portainer.Source{
		Name: "app-repo",
		Type: portainer.SourceTypeGit,
		Git: &gittypes.GitSource{
			URL:           "https://gitlab.internal/org/app-repo.git",
			TLSSkipVerify: true,
			Authentication: &gittypes.GitAuthentication{
				Username: "deploy-bot",
				Password: "stored-valid-pat",
			},
		},
	}
	adminCtx := source.InsecureNewAdminContext()
	require.NoError(t, store.Source().Create(adminCtx, src))

	const stackID = portainer.StackID(100)
	wf := &portainer.Workflow{
		Artifacts: []portainer.Artifact{{
			StackID: stackID,
			Files: []portainer.ArtifactFile{{
				SourceID: src.ID,
				Ref:      "refs/heads/production",
				Path:     "deploy/docker-compose.yml",
				Hash:     "initialhash",
			}},
		}},
	}
	require.NoError(t, store.Workflow().Create(wf))

	stack := &portainer.Stack{
		ID:              stackID,
		Name:            "git-prod-stack",
		Type:            portainer.DockerComposeStack,
		EndpointID:      endpoint.ID,
		WorkflowID:      wf.ID,
		EntryPoint:      "deploy/docker-compose.yml",
		AdditionalFiles: []string{"deploy/docker-compose.override.yml"},
		AutoUpdate:      &portainer.AutoUpdateSettings{Webhook: "11111111-2222-3333-4444-555555555555"},
		Env:             []portainer.Pair{{Name: "APP_SECRET", Value: "keep-me"}},
		Option:          &portainer.StackOption{Prune: true},
	}
	require.NoError(t, store.Stack().Create(stack))

	rc := &portainer.ResourceControl{
		ID:                 1,
		ResourceID:         stackutils.ResourceControlID(endpoint.ID, stack.Name),
		Type:               portainer.StackResourceControl,
		AdministratorsOnly: false,
	}
	require.NoError(t, store.ResourceControl().Create(rc))

	handler := NewHandler(testhelpers.NewTestRequestBouncer(), nil)
	handler.DataStore = store
	handler.GitService = gitMock

	return handler, store, stack, src.ID, endpoint
}

func sendStackGitUpdateReq(t *testing.T, handler *Handler, stackID portainer.StackID, endpointID portainer.EndpointID, rawJSON []byte, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/stacks/%d/git?endpointId=%d", stackID, endpointID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(rawJSON))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set(portainer.APIKeyHeader, apiKey)
	}
	rrc := &security.RestrictedRequestContext{
		IsAdmin: true,
		UserID:  1,
		User:    &portainer.User{ID: 1, Username: "admin", Role: portainer.AdministratorRole},
	}
	req = req.WithContext(security.StoreRestrictedRequestContext(req, rrc))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestStackUpdateGit_MCPPartialPayloadPreservesSourceAndMetadata(t *testing.T) {
	t.Parallel()

	gitMock := &mockStackGitService{}
	handler, store, stack, srcID, endpoint := setupGitStackUpdateTest(t, gitMock)

	// Simulate MCP sending empty PAT, empty Username, RepositoryAuthentication=false,
	// empty ReferenceName, empty AdditionalFiles=[], omitted AutoUpdate/Prune/TLSSkipVerify.
	body := []byte(`{"AdditionalFiles":[],"Env":[]}`)
	rr := sendStackGitUpdateReq(t, handler, stack.ID, endpoint.ID, body, "ptr_mcp_key")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	adminCtx := source.InsecureNewAdminContext()
	storedSrc, err := store.Source().Read(adminCtx, srcID)
	require.NoError(t, err)
	require.NotNil(t, storedSrc.Git)
	require.True(t, storedSrc.Git.TLSSkipVerify, "TLSSkipVerify must be preserved")
	require.NotNil(t, storedSrc.Git.Authentication, "Git Authentication must NOT be cleared")
	assert.Equal(t, "deploy-bot", storedSrc.Git.Authentication.Username)
	assert.Equal(t, "stored-valid-pat", storedSrc.Git.Authentication.Password)

	storedStack, err := store.Stack().Read(stack.ID)
	require.NoError(t, err)
	assert.Equal(t, "deploy/docker-compose.yml", storedStack.EntryPoint)
	assert.Equal(t, []string{"deploy/docker-compose.override.yml"}, storedStack.AdditionalFiles)
	require.NotNil(t, storedStack.AutoUpdate)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", storedStack.AutoUpdate.Webhook)
	assert.Equal(t, []portainer.Pair{{Name: "APP_SECRET", Value: "keep-me"}}, storedStack.Env)
	require.NotNil(t, storedStack.Option)
	assert.True(t, storedStack.Option.Prune)

	storedWf, err := store.Workflow().Read(storedStack.WorkflowID)
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/production", storedWf.Artifacts[0].Files[0].Ref, "Branch ReferenceName must be preserved")
}

func TestStackUpdateGit_ChecksConnectionBeforeOverwritingPAT(t *testing.T) {
	t.Parallel()

	t.Run("invalid PAT fails connection check and does NOT overwrite stored PAT", func(t *testing.T) {
		t.Parallel()

		gitMock := &mockStackGitService{latestCommitErr: errors.New("401 Unauthorized")}
		handler, store, stack, srcID, endpoint := setupGitStackUpdateTest(t, gitMock)

		payload, err := json.Marshal(map[string]any{
			"RepositoryAuthentication": true,
			"RepositoryPassword":       "invalid-new-pat",
		})
		require.NoError(t, err)

		rr := sendStackGitUpdateReq(t, handler, stack.ID, endpoint.ID, payload, "ptr_mcp_key")
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		require.Equal(t, 1, gitMock.callCount)
		assert.Equal(t, "deploy-bot", gitMock.lastUser, "empty username should inherit stored username")
		assert.Equal(t, "invalid-new-pat", gitMock.lastPass)

		storedSrc, err := store.Source().Read(source.InsecureNewAdminContext(), srcID)
		require.NoError(t, err)
		require.NotNil(t, storedSrc.Git.Authentication)
		assert.Equal(t, "deploy-bot", storedSrc.Git.Authentication.Username)
		assert.Equal(t, "stored-valid-pat", storedSrc.Git.Authentication.Password, "stored PAT must remain untouched on failed connection")
	})

	t.Run("valid PAT passes connection check, updates PAT, and retains stored username when omitted", func(t *testing.T) {
		t.Parallel()

		gitMock := &mockStackGitService{}
		handler, store, stack, srcID, endpoint := setupGitStackUpdateTest(t, gitMock)

		payload, err := json.Marshal(map[string]any{
			"RepositoryAuthentication": true,
			"RepositoryPassword":       "verified-new-pat",
		})
		require.NoError(t, err)

		rr := sendStackGitUpdateReq(t, handler, stack.ID, endpoint.ID, payload, "ptr_mcp_key")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.Equal(t, 1, gitMock.callCount)

		storedSrc, err := store.Source().Read(source.InsecureNewAdminContext(), srcID)
		require.NoError(t, err)
		require.NotNil(t, storedSrc.Git.Authentication)
		assert.Equal(t, "deploy-bot", storedSrc.Git.Authentication.Username)
		assert.Equal(t, "verified-new-pat", storedSrc.Git.Authentication.Password)
	})
}

func TestStackUpdate_BlocksAccidentalGitDetachOnTokenRequest(t *testing.T) {
	t.Parallel()
	fips.InitFIPS(false)

	payload := &updateComposeStackPayload{
		StackFileContent: "version: '3'\nservices:\n  web:\n    image: nginx:latest",
	}
	stack := &portainer.Stack{
		ID:         1,
		Name:       "git-backed-compose",
		EntryPoint: "docker-compose.yml",
		Type:       portainer.DockerComposeStack,
		WorkflowID: 42,
		AutoUpdate: &portainer.AutoUpdateSettings{Webhook: "webhook-uuid"},
	}
	setup := setupUpdateStackInTxTest(t, stack, payload)
	setup.req.Header.Set(portainer.APIKeyHeader, "ptr_mcp_token")

	var handlerErr *httperror.HandlerError
	_ = setup.store.UpdateTx(func(tx dataservices.DataStoreTx) error {
		_, _, _, handlerErr = setup.handler.updateStackInTx(tx, setup.req, setup.stack.ID, setup.endpoint.ID)
		if handlerErr != nil {
			return handlerErr
		}
		return nil
	})
	require.NotNil(t, handlerErr)
	assert.Equal(t, http.StatusConflict, handlerErr.StatusCode)

	stored, err := setup.store.Stack().Read(setup.stack.ID)
	require.NoError(t, err)
	assert.Equal(t, portainer.WorkflowID(42), stored.WorkflowID, "WorkflowID must not be detached")
	require.NotNil(t, stored.AutoUpdate, "AutoUpdate must not be wiped")
}

func TestStackUpdate_KubernetesFileStack_PreservesNameWhenOmitted(t *testing.T) {
	t.Parallel()

	handler, stack, endpoint := setupStackUpdateKubernetesTest(t, nil)

	payload := kubernetesFileStackUpdatePayload{
		StackFileContent: kubernetesFileStackUpdatedTestManifest,
		// StackName intentionally omitted
	}
	jsonPayload, err := json.Marshal(payload)
	require.NoError(t, err)

	req := mockUpdateKubernetesStackFileContentRequest(stack.ID, endpoint.ID, jsonPayload)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var updated *portainer.Stack
	require.NoError(t, handler.DataStore.ViewTx(func(tx dataservices.DataStoreTx) error {
		var readErr error
		updated, readErr = tx.Stack().Read(stack.ID)
		return readErr
	}))
	assert.Equal(t, "k8s-file-stack", updated.Name, "omitting StackName must not erase stack.Name")
}

func TestResolveGitAuthFromRedeployPayload_PreservesUsernameAndRejectsRedacted(t *testing.T) {
	t.Parallel()

	existing := &gittypes.GitAuthentication{
		Username: "existing-user",
		Password: "existing-pass",
	}
	cfg := &gittypes.RepoConfig{Authentication: existing}

	t.Run("new PAT with empty username keeps existing username", func(t *testing.T) {
		t.Parallel()
		got := resolveGitAuthFromRedeployPayload(cfg, stackGitRedeployPayload{
			RepositoryAuthentication: true,
			RepositoryUsername:       "",
			RepositoryPassword:       "new-pat",
		})
		assert.Equal(t, gittypes.GitAuthentication{Username: "existing-user", Password: "new-pat"}, got)
	})

	t.Run("[REDACTED] PAT keeps existing credentials", func(t *testing.T) {
		t.Parallel()
		got := resolveGitAuthFromRedeployPayload(cfg, stackGitRedeployPayload{
			RepositoryAuthentication: true,
			RepositoryUsername:       "new-user",
			RepositoryPassword:       "[REDACTED]",
		})
		assert.Equal(t, *existing, got)
	})
}
