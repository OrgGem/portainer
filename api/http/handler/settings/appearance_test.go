package settings

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/database"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/segmentio/encoding/json"
	"github.com/stretchr/testify/require"
)

type appearanceTestSnapshotService struct{}

type appearancePublicOnlyBouncer struct{ security.BouncerService }

func (appearancePublicOnlyBouncer) AdminAccess(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "administrator access required", http.StatusForbidden)
	})
}

func (appearancePublicOnlyBouncer) PublicAccess(handler http.Handler) http.Handler {
	return handler
}

var _ security.BouncerService = appearancePublicOnlyBouncer{}

func (appearanceTestSnapshotService) Start(context.Context)            {}
func (appearanceTestSnapshotService) SetSnapshotInterval(string) error { return nil }
func (appearanceTestSnapshotService) SnapshotEndpoint(*portainer.Endpoint) error {
	return nil
}
func (appearanceTestSnapshotService) FillSnapshotData(*portainer.Endpoint, bool) error {
	return nil
}

func TestGeneratePublicSettingsAppearance(t *testing.T) {
	t.Parallel()

	t.Run("legacy settings receive defaults", func(t *testing.T) {
		t.Parallel()
		got := generatePublicSettings(&portainer.Settings{}).Appearance
		require.Equal(t, portainer.DefaultAppearance(), got)
	})

	t.Run("configured appearance is public", func(t *testing.T) {
		t.Parallel()
		appearance := portainer.DefaultAppearance()
		appearance.Branding.Name = "Example"
		appearance.Branding.FaviconURL = "/favicon.png"
		appearance.Colors.Dark.Accent = "#123456"
		appearance.DefaultTheme = "dark"

		got := generatePublicSettings(&portainer.Settings{Appearance: &appearance}).Appearance
		require.Equal(t, appearance, got)
	})
}

func TestAppearanceRoutesKeepAdminAndPublicAccess(t *testing.T) {
	t.Parallel()
	_, store := datastore.MustNewTestStore(t, true, false)
	handler := NewHandler(appearancePublicOnlyBouncer{})
	handler.DataStore = store

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		request := httptest.NewRequest(method, "/settings", bytes.NewBufferString(`{}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusForbidden, response.Code, method)
	}

	request := httptest.NewRequest(http.MethodGet, "/settings/public", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"Appearance"`)
}

func TestSettingsUpdateAppearancePersistenceAndAtomicValidation(t *testing.T) {
	t.Parallel()
	_, store, teardown, err := datastore.NewTestStore(t, true, false)
	require.NoError(t, err)
	var reopened *datastore.Store
	initialClosed := false
	t.Cleanup(func() {
		if reopened != nil {
			_ = reopened.Close()
		} else if !initialClosed {
			teardown()
		}
	})
	fileService, err := filesystem.NewService(t.TempDir(), "")
	require.NoError(t, err)
	handler := NewHandler(testhelpers.NewTestRequestBouncer())
	handler.DataStore = store
	handler.FileService = fileService
	handler.SnapshotService = appearanceTestSnapshotService{}

	appearance := portainer.DefaultAppearance()
	appearance.Branding.Name = "Example Platform"
	appearance.Colors.Light.Accent = "#326CE5"
	appearance.Layout.SidebarWidth = 320
	appearance.DefaultTheme = "dark"
	update := func(body []byte) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	initialPayload, err := json.Marshal(struct {
		LogoURL    string                       `json:"LogoURL"`
		Appearance portainer.AppearanceSettings `json:"Appearance"`
	}{LogoURL: "https://example.test/logo.png", Appearance: appearance})
	require.NoError(t, err)
	response := update(initialPayload)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	saved, err := store.Settings().Settings()
	require.NoError(t, err)
	require.Equal(t, "https://example.test/logo.png", saved.LogoURL)
	require.NotNil(t, saved.Appearance)
	require.Equal(t, appearance, *saved.Appearance)

	// A request from an existing settings panel that omits Appearance must not
	// erase a saved appearance configuration.
	response = update([]byte(`{"SnapshotInterval":"20m"}`))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	saved, err = store.Settings().Settings()
	require.NoError(t, err)
	require.Equal(t, appearance, *saved.Appearance)
	require.Equal(t, "20m", saved.SnapshotInterval)

	invalidPayload, err := json.Marshal(struct {
		LogoURL    string                       `json:"LogoURL"`
		Appearance portainer.AppearanceSettings `json:"Appearance"`
	}{LogoURL: "https://example.test/should-not-save.png", Appearance: func() portainer.AppearanceSettings {
		invalid := appearance
		invalid.Colors.Light.Accent = "not-a-color"
		return invalid
	}()})
	require.NoError(t, err)
	response = update(invalidPayload)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	saved, err = store.Settings().Settings()
	require.NoError(t, err)
	require.Equal(t, "https://example.test/logo.png", saved.LogoURL)
	require.Equal(t, appearance, *saved.Appearance)

	// Reopen the isolated database to ensure Appearance survives a Portainer
	// process restart rather than only remaining in the in-memory test store.
	storePath := store.GetConnection().GetStorePath()
	teardown()
	initialClosed = true
	defaultKubectlShellImage := portainer.DefaultKubectlShellImage
	reopenedFileService, err := filesystem.NewService(storePath, "")
	require.NoError(t, err)
	connection, err := database.NewDatabase("boltdb", storePath, nil, false)
	require.NoError(t, err)
	reopened = datastore.NewStore(&portainer.CLIFlags{KubectlShellImage: &defaultKubectlShellImage}, reopenedFileService, connection)
	_, err = reopened.Open()
	require.NoError(t, err)
	saved, err = reopened.Settings().Settings()
	require.NoError(t, err)
	require.Equal(t, "https://example.test/logo.png", saved.LogoURL)
	require.Equal(t, appearance, *saved.Appearance)
}
