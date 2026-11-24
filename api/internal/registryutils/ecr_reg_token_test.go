package registryutils_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/internal/registryutils"
	zerolog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func newECRRegistry(id portainer.RegistryID, accessToken string, expiry int64) portainer.Registry {
	return portainer.Registry{
		ID:                id,
		Type:              portainer.EcrRegistry,
		Name:              "test-ecr",
		Username:          "AKIAIOSFODNN7EXAMPLE",
		Password:          "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Ecr:               portainer.EcrData{Region: "us-east-1"},
		AccessToken:       accessToken,
		AccessTokenExpiry: expiry,
	}
}

func TestRefreshAndPersistECRTokens(t *testing.T) {
	t.Run("does not modify token for ECR registry with valid token", func(t *testing.T) {
		_, ds := datastore.MustNewTestStore(t, true, false)
		reg := newECRRegistry(1, "valid-token", time.Now().Add(time.Hour).Unix())
		require.NoError(t, ds.UpdateTx(func(tx dataservices.DataStoreTx) error {
			registryutils.RefreshAndPersistECRTokens(tx, []portainer.Registry{reg})
			return nil
		}))
		require.Equal(t, "valid-token", reg.AccessToken)
	})

	t.Run("does not block and leaves token empty when ECR token refresh fails", func(t *testing.T) {
		var logOutput strings.Builder
		setupLogOutput(t, &logOutput)

		_, ds := datastore.MustNewTestStore(t, true, false)
		reg := newECRRegistry(1, "", 0)
		require.NoError(t, ds.UpdateTx(func(tx dataservices.DataStoreTx) error {
			registryutils.RefreshAndPersistECRTokens(tx, []portainer.Registry{reg})
			return nil
		}))
		require.Empty(t, reg.AccessToken)
		require.Contains(t, logOutput.String(), "test-ecr")
		require.Contains(t, logOutput.String(), "Failed to get valid ECR registry token")
	})
}

func setupLogOutput(t *testing.T, w io.Writer) {
	t.Helper()

	oldLogger := zerolog.Logger
	zerolog.Logger = zerolog.Output(w)
	t.Cleanup(func() {
		zerolog.Logger = oldLogger
	})
}

func TestGetRegEffectiveCredential(t *testing.T) {
	t.Parallel()

	t.Run("returns username and password directly for non-ECR registry", func(t *testing.T) {
		t.Parallel()
		reg := &portainer.Registry{
			Type:     portainer.DockerHubRegistry,
			Username: "user",
			Password: "pass",
		}
		username, password, err := registryutils.GetRegEffectiveCredential(reg)
		require.NoError(t, err)
		require.Equal(t, "user", username)
		require.Equal(t, "pass", password)
	})

	t.Run("parses ECR access token when token is valid", func(t *testing.T) {
		t.Parallel()
		reg := newECRRegistry(1, "AWS:ecr-password", time.Now().Add(time.Hour).Unix())
		username, password, err := registryutils.GetRegEffectiveCredential(&reg)
		require.NoError(t, err)
		require.Equal(t, "AWS", username)
		require.Equal(t, "ecr-password", password)
	})

	t.Run("returns error for ECR registry with missing token and invalid credentials", func(t *testing.T) {
		t.Parallel()
		reg := newECRRegistry(1, "", 0)
		_, _, err := registryutils.GetRegEffectiveCredential(&reg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "test-ecr")
	})
}

func TestIAMRoleRenewsECRToken(t *testing.T) {
	var requests atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/credentials" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"AccessKeyId": "workload-key", "SecretAccessKey": "workload-secret",
				"Token": "workload-session", "Expiration": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			})
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Credential=workload-key/") ||
			r.Header.Get("X-Amz-Security-Token") != "workload-session" ||
			r.Header.Get("X-Amz-Target") != "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken" {
			http.Error(w, "unexpected ECR identity or request", http.StatusForbidden)
			return
		}
		n := requests.Add(1)
		token := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("AWS:password-%d", n)))
		_ = json.NewEncoder(w).Encode(map[string]any{"authorizationData": []map[string]any{
			{"authorizationToken": token, "expiresAt": time.Now().Add(12 * time.Hour).Unix()},
		}})
	}))
	defer mock.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", mock.URL+"/credentials")
	t.Setenv("AWS_ENDPOINT_URL", mock.URL)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, ds := datastore.MustNewTestStore(t, true, false)
	reg := portainer.Registry{Type: portainer.EcrRegistry, Authentication: true,
		Ecr: portainer.EcrData{Region: "us-east-1", UseIAMRole: true}}
	require.NoError(t, ds.Registry().Create(&reg))
	for _, want := range []string{"password-1", "password-1", "password-2"} {
		if want == "password-2" {
			reg.AccessTokenExpiry = time.Now().Add(-time.Second).Unix()
		}
		require.NoError(t, registryutils.EnsureRegTokenValid(ds, &reg))
		user, password, err := registryutils.GetRegEffectiveCredential(&reg)
		require.NoError(t, err)
		require.Equal(t, "AWS", user)
		require.Equal(t, want, password)
	}
	require.EqualValues(t, 2, requests.Load())
	stored, err := ds.Registry().Read(reg.ID)
	require.NoError(t, err)
	require.Equal(t, "AWS:password-2", stored.AccessToken)
	require.Empty(t, stored.Username)
	require.Empty(t, stored.Password)

	stored.Ecr.UseIAMRole = false
	stored.Username, stored.Password = "explicit-key", "explicit-secret"
	stored.AccessToken, stored.AccessTokenExpiry = "", 0
	require.NoError(t, ds.Registry().Update(stored.ID, stored))
	reg.AccessTokenExpiry = 0
	require.ErrorContains(t, registryutils.EnsureRegTokenValid(ds, &reg), "authentication changed")
	stored, err = ds.Registry().Read(reg.ID)
	require.NoError(t, err)
	require.False(t, stored.Ecr.UseIAMRole)
	require.Empty(t, stored.AccessToken)

	t.Setenv("AWS_ACCESS_KEY_ID", "static-server-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "static-server-secret")
	_, _, err = registryutils.GetRegEffectiveCredential(&reg)
	require.ErrorContains(t, err, "requires temporary AWS credentials")
}
