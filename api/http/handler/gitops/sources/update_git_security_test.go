package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/datastore"
	gittypes "github.com/portainer/portainer/api/git/types"
	"github.com/portainer/portainer/api/internal/testhelpers"

	"github.com/segmentio/encoding/json"
	"github.com/stretchr/testify/require"
)

type stubGitService struct {
	portainer.GitService
	listRefsErr error
	lastUser    string
	lastPass    string
	callCount   int
}

func (s *stubGitService) ListRefs(_ context.Context, _, username, password string, _, _ bool) ([]string, error) {
	s.callCount++
	s.lastUser = username
	s.lastPass = password
	if s.listRefsErr != nil {
		return nil, s.listRefsErr
	}
	return []string{"refs/heads/main"}, nil
}

func seedAuthenticatedGitSource(t *testing.T, store dataservices.DataStore) portainer.SourceID {
	t.Helper()
	var srcID portainer.SourceID
	require.NoError(t, store.UpdateTx(func(tx dataservices.DataStoreTx) error {
		src := &portainer.Source{
			Name: "private-repo",
			Type: portainer.SourceTypeGit,
			Git: &gittypes.GitSource{
				URL: "https://github.com/org/private-repo.git",
				Authentication: &gittypes.GitAuthentication{
					Username: "alice",
					Password: "stored-valid-pat",
				},
			},
		}
		if err := tx.Source().Create(adminUserContext, src); err != nil {
			return err
		}
		srcID = src.ID
		return tx.User().Create(&portainer.User{ID: 1, Role: portainer.AdministratorRole})
	}))
	return srcID
}

func TestGitSourceUpdate_NeverOverwritesWithEmptyOrRedactedPAT(t *testing.T) {
	t.Parallel()

	t.Run("sanitized empty password or empty username preserves stored credentials", func(t *testing.T) {
		t.Parallel()
		_, store := datastore.MustNewTestStore(t, false, true)
		srcID := seedAuthenticatedGitSource(t, store)
		h := newTestHandler(t, store)

		body, err := json.Marshal(GitSourceUpdatePayload{
			Name: new("renamed-repo"),
			Authentication: &GitAuthenticationUpdatePayload{
				Username: new(""),
				Password: new(""),
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, buildUpdateReq(t, 1, int(srcID), body))
		require.Equal(t, http.StatusOK, rr.Code)

		var stored *portainer.Source
		require.NoError(t, store.ViewTx(func(tx dataservices.DataStoreTx) error {
			var readErr error
			stored, readErr = tx.Source().Read(adminUserContext, srcID)
			return readErr
		}))
		require.NotNil(t, stored.Git.Authentication)
		require.Equal(t, "alice", stored.Git.Authentication.Username)
		require.Equal(t, "stored-valid-pat", stored.Git.Authentication.Password)
	})

	t.Run("MCP [REDACTED] password preserves stored PAT while updating username", func(t *testing.T) {
		t.Parallel()
		_, store := datastore.MustNewTestStore(t, false, true)
		srcID := seedAuthenticatedGitSource(t, store)
		h := newTestHandler(t, store)

		body, err := json.Marshal(GitSourceUpdatePayload{
			Authentication: &GitAuthenticationUpdatePayload{
				Username: new("bob"),
				Password: new("[REDACTED]"),
			},
		})
		require.NoError(t, err)

		req := buildUpdateReq(t, 1, int(srcID), body)
		req.Header.Set(portainer.APIKeyHeader, "ptr_mcp_token")

		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		var stored *portainer.Source
		require.NoError(t, store.ViewTx(func(tx dataservices.DataStoreTx) error {
			var readErr error
			stored, readErr = tx.Source().Read(adminUserContext, srcID)
			return readErr
		}))
		require.NotNil(t, stored.Git.Authentication)
		require.Equal(t, "bob", stored.Git.Authentication.Username)
		require.Equal(t, "stored-valid-pat", stored.Git.Authentication.Password)
	})

	t.Run("MCP request sending empty authentication object preserves stored credentials", func(t *testing.T) {
		t.Parallel()
		_, store := datastore.MustNewTestStore(t, false, true)
		srcID := seedAuthenticatedGitSource(t, store)
		h := newTestHandler(t, store)

		body, err := json.Marshal(GitSourceUpdatePayload{
			Name:           new("mcp-updated"),
			Authentication: &GitAuthenticationUpdatePayload{},
		})
		require.NoError(t, err)

		req := buildUpdateReq(t, 1, int(srcID), body)
		req.Header.Set(portainer.APIKeyHeader, "ptr_mcp_token")

		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		var stored *portainer.Source
		require.NoError(t, store.ViewTx(func(tx dataservices.DataStoreTx) error {
			var readErr error
			stored, readErr = tx.Source().Read(adminUserContext, srcID)
			return readErr
		}))
		require.NotNil(t, stored.Git.Authentication)
		require.Equal(t, "alice", stored.Git.Authentication.Username)
		require.Equal(t, "stored-valid-pat", stored.Git.Authentication.Password)
	})
}

func TestGitSourceUpdate_ChecksConnectionBeforeOverwritingPAT(t *testing.T) {
	t.Parallel()

	t.Run("failed connection check rejects update and leaves stored PAT untouched", func(t *testing.T) {
		t.Parallel()
		_, store := datastore.MustNewTestStore(t, false, true)
		srcID := seedAuthenticatedGitSource(t, store)

		gitMock := &stubGitService{listRefsErr: errors.New("authentication failed: invalid PAT")}
		h := NewHandler(testhelpers.NewTestRequestBouncer(), store, gitMock, nil, nil)

		body, err := json.Marshal(GitSourceUpdatePayload{
			Authentication: &GitAuthenticationUpdatePayload{
				Password: new("bad-invalid-pat"),
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, buildUpdateReq(t, 1, int(srcID), body))
		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, 1, gitMock.callCount)
		require.Equal(t, "alice", gitMock.lastUser, "empty username should inherit stored username during connection test")
		require.Equal(t, "bad-invalid-pat", gitMock.lastPass)

		var stored *portainer.Source
		require.NoError(t, store.ViewTx(func(tx dataservices.DataStoreTx) error {
			var readErr error
			stored, readErr = tx.Source().Read(adminUserContext, srcID)
			return readErr
		}))
		require.NotNil(t, stored.Git.Authentication)
		require.Equal(t, "alice", stored.Git.Authentication.Username)
		require.Equal(t, "stored-valid-pat", stored.Git.Authentication.Password, "stored PAT must NOT be overwritten when connection fails")
	})

	t.Run("successful connection check updates PAT and preserves stored username when omitted", func(t *testing.T) {
		t.Parallel()
		_, store := datastore.MustNewTestStore(t, false, true)
		srcID := seedAuthenticatedGitSource(t, store)

		gitMock := &stubGitService{}
		h := NewHandler(testhelpers.NewTestRequestBouncer(), store, gitMock, nil, nil)

		body, err := json.Marshal(GitSourceUpdatePayload{
			Authentication: &GitAuthenticationUpdatePayload{
				Password: new("new-verified-pat"),
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, buildUpdateReq(t, 1, int(srcID), body))
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, 1, gitMock.callCount)

		var stored *portainer.Source
		require.NoError(t, store.ViewTx(func(tx dataservices.DataStoreTx) error {
			var readErr error
			stored, readErr = tx.Source().Read(adminUserContext, srcID)
			return readErr
		}))
		require.NotNil(t, stored.Git.Authentication)
		require.Equal(t, "alice", stored.Git.Authentication.Username)
		require.Equal(t, "new-verified-pat", stored.Git.Authentication.Password)
		require.Equal(t, portainer.SourceStatusHealthy, stored.Status)
	})
}
