package backup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/portainer/portainer/api/adminmonitor"
	"github.com/portainer/portainer/api/backup/s3backup"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/stretchr/testify/require"
)

func newS3BackupHandler(t *testing.T) *Handler {
	t.Helper()
	_, store := datastore.MustNewTestStore(t, true, false)
	return NewHandler(
		testhelpers.NewTestRequestBouncer(),
		store,
		offlinegate.NewOfflineGate(),
		t.TempDir(),
		func() {},
		adminmonitor.New(time.Hour, nil),
	)
}

func s3AdminRequest(method, path, body string, isAdmin bool) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	requestContext := security.StoreRestrictedRequestContext(request, &security.RestrictedRequestContext{IsAdmin: isAdmin})
	return request.WithContext(requestContext)
}

func TestS3BackupSettingsRequireAdministrator(t *testing.T) {
	t.Parallel()
	handler := newS3BackupHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodGet, "/backup/s3/settings", "", false))
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestS3BackupSettingsRoutesPersistConfigurationAndRejectCredentials(t *testing.T) {
	t.Parallel()
	handler := newS3BackupHandler(t)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodGet, "/backup/s3/settings", "", true))
	require.Equal(t, http.StatusOK, response.Code)
	var initial s3backup.Settings
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &initial))
	require.Equal(t, "00:00", initial.DailyTime)
	require.Empty(t, initial.BucketName)
	require.NotContains(t, response.Body.String(), "accessKeyID")
	require.NotContains(t, response.Body.String(), "secretAccessKey")

	staticCredentialPayload := `{"bucketName":"portainer-backups","region":"ap-southeast-1","prefix":"daily","enabled":true,"dailyTime":"04:15","accessKeyID":"static-key","secretAccessKey":"static-secret"}`
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodPost, "/backup/s3/settings", staticCredentialPayload, true))
	require.Equal(t, http.StatusBadRequest, response.Code)

	payload, err := json.Marshal(s3backup.Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
		Prefix:     "daily",
		Enabled:    true,
		DailyTime:  "04:15",
	})
	require.NoError(t, err)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodPost, "/backup/s3/settings", string(payload), true))
	require.Equal(t, http.StatusOK, response.Code)

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodGet, "/backup/s3/settings", "", true))
	require.Equal(t, http.StatusOK, response.Code)
	var saved s3backup.Settings
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	require.Equal(t, s3backup.Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
		Prefix:     "daily",
		Enabled:    true,
		DailyTime:  "04:15",
	}, saved.Configuration)
	require.NotContains(t, response.Body.String(), "secretAccessKey")
}

func TestS3BackupExecuteRejectsMissingDestination(t *testing.T) {
	t.Parallel()
	handler := newS3BackupHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodPost, "/backup/s3/execute", `{}`, true))
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestS3BackupSettingsPreserveLegacyRunWithoutDestination(t *testing.T) {
	t.Parallel()
	handler := newS3BackupHandler(t)
	legacyAt := time.Date(2026, time.November, 19, 4, 15, 0, 0, time.UTC)
	legacyState := map[string]any{
		"configuration": s3backup.Configuration{
			BucketName: "portainer-backups",
			Region:     "ap-southeast-1",
			Prefix:     "daily",
			Enabled:    true,
			DailyTime:  "04:15",
		},
		"lastRun": map[string]any{
			"at":     legacyAt,
			"status": "success",
			"key":    "daily/legacy-backup.tar.gz",
		},
		"lastScheduled": legacyAt,
		"revision":      1,
	}
	require.NoError(t, handler.dataStore.Connection().UpdateObject("settings", []byte("CUSTOM_S3_BACKUP"), legacyState))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, s3AdminRequest(http.MethodGet, "/backup/s3/settings", "", true))
	require.Equal(t, http.StatusOK, response.Code)
	var settings s3backup.Settings
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &settings))
	require.NotNil(t, settings.LastRun)
	require.Equal(t, "success", settings.LastRun.Status)
	require.Equal(t, "daily/legacy-backup.tar.gz", settings.LastRun.Key)
	require.Nil(t, settings.LastRun.Destination)
	require.NotContains(t, response.Body.String(), `"destination"`)
}
