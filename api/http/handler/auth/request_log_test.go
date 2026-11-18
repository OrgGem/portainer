package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/authlog"
	"github.com/portainer/portainer/api/crypto"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/portainer/portainer/api/jwt"
	"github.com/portainer/portainer/api/kubernetes/cli"

	httperrors "github.com/portainer/portainer/api/http/errors"
	"github.com/portainer/portainer/pkg/libhttp"
	"github.com/stretchr/testify/require"
)

func TestAuthenticationRequestLogsMalformedAndRateLimitedRequestsExactlyOnce(t *testing.T) {
	t.Parallel()

	trustedProxies, err := libhttp.ParseTrustedProxies([]string{"127.0.0.1/32"})
	require.NoError(t, err)
	limiter := security.NewRateLimiter(1, time.Hour, time.Hour, trustedProxies)
	dataDir := t.TempDir()
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), limiter, nil, nil)
	handler.AuthLog = authlog.NewStore(dataDir)

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader("not-json password-secret oauth-code-secret"))
		req.RemoteAddr = "127.0.0.1:4567"
		req.Header.Set("X-Forwarded-For", "198.51.100.23")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	events, status, err := handler.AuthLog.Read(context.Background())
	require.NoError(t, err)
	require.True(t, status.Available)
	require.False(t, status.Partial)
	require.Len(t, events, 2)
	require.Equal(t, http.StatusBadRequest, events[0].StatusCode)
	require.Equal(t, http.StatusForbidden, events[1].StatusCode)
	require.True(t, events[1].RateLimited)
	require.Equal(t, "Rate-limited requests are sampled globally; suppressed requests are counted on the next sample.", status.Message)
	for _, event := range events {
		require.Equal(t, authlog.Unknown, event.Context)
		require.Equal(t, authlog.Failure, event.Type)
		require.Empty(t, event.Username)
		require.Equal(t, "198.51.100.23", event.Origin)
	}

	contents := readAuthLogFiles(t, dataDir)
	require.NotContains(t, contents, "password-secret")
	require.NotContains(t, contents, "oauth-code-secret")
	require.NotContains(t, contents, "not-json")
}

func TestRateLimitedAuthenticationEventsAreSampledAndAggregatedGlobally(t *testing.T) {
	t.Parallel()

	handler := &Handler{}
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	shouldSample, suppressed := handler.sampleRateLimited(now)
	require.True(t, shouldSample)
	require.Zero(t, suppressed)

	// Concurrent rejections from arbitrary origins must share a bounded,
	// synchronized sampler rather than creating per-client state.
	const rejections = 1000
	var sampled int
	var sampledMu sync.Mutex
	var wg sync.WaitGroup
	for range rejections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shouldSample, _ := handler.sampleRateLimited(now.Add(time.Second))
			if shouldSample {
				sampledMu.Lock()
				sampled++
				sampledMu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Zero(t, sampled)
	require.Equal(t, uint64(rejections), handler.rateLimitLogSuppressed)

	shouldSample, suppressed = handler.sampleRateLimited(now.Add(rateLimitLogInterval))
	require.True(t, shouldSample)
	require.Equal(t, uint64(rejections), suppressed)
	require.Zero(t, handler.rateLimitLogSuppressed)

	// The sampler retains only its fixed deadline and a saturating counter.
	require.Equal(t, now.Add(rateLimitLogInterval*2), handler.rateLimitLogNext)
}

func TestSuppressedRateLimitedRequestsAreReportedInNextPersistedSample(t *testing.T) {
	t.Parallel()

	trustedProxies, err := libhttp.ParseTrustedProxies([]string{"127.0.0.1/32"})
	require.NoError(t, err)
	limiter := security.NewRateLimiter(1, time.Hour, time.Hour, trustedProxies)
	dataDir := t.TempDir()
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), limiter, nil, nil)
	handler.AuthLog = authlog.NewStore(dataDir)

	post := func() int {
		request := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader("malformed"))
		request.RemoteAddr = "127.0.0.1:4567"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}

	require.Equal(t, http.StatusBadRequest, post(), "the first malformed attempt is logged normally")
	require.Equal(t, http.StatusForbidden, post(), "the first limiter rejection is sampled")
	for range 20 {
		require.Equal(t, http.StatusForbidden, post())
	}

	handler.rateLimitLogMu.Lock()
	handler.rateLimitLogNext = time.Now().Add(-time.Second)
	handler.rateLimitLogMu.Unlock()
	require.Equal(t, http.StatusForbidden, post(), "the next interval emits the aggregate")

	events, status, err := handler.AuthLog.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Rate-limited requests are sampled globally; suppressed requests are counted on the next sample.", status.Message)
	require.Len(t, events, 3, "20 denied requests within one sampling interval cause no per-request writes")
	require.False(t, events[0].RateLimited)
	require.True(t, events[1].RateLimited)
	require.Zero(t, events[1].SuppressedRateLimited)
	require.True(t, events[2].RateLimited)
	require.Equal(t, uint64(20), events[2].SuppressedRateLimited)
}

func TestAuthenticationRequestLogAppendFailureDoesNotChangeLoginResponse(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "not-a-directory")
	require.NoError(t, os.WriteFile(storePath, []byte("fixture"), 0600))
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), security.NewRateLimiter(10, time.Minute, time.Hour, nil), nil, nil)
	handler.AuthLog = authlog.NewStore(storePath)

	req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader("malformed-request"))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.NotContains(t, rr.Body.String(), storePath)
}

func TestAuthenticationMethodsLogSuccessAndFailureWithoutCredentials(t *testing.T) {
	for _, test := range []struct {
		name      string
		method    portainer.AuthenticationMethod
		context   int
		path      string
		username  string
		badSecret string
		badStatus int
		goodBody  string
		badBody   string
		userID    portainer.UserID
	}{
		{
			name: "internal", method: portainer.AuthenticationInternal, context: authlog.Internal,
			path: "/auth", username: "internal-user", userID: 1,
			badSecret: "wrong-password-secret", badStatus: http.StatusUnprocessableEntity,
			badBody:  `{"Username":"internal-user","Password":"wrong-password-secret"}`,
			goodBody: `{"Username":"internal-user","Password":"correct-password-secret"}`,
		},
		{
			name: "ldap", method: portainer.AuthenticationLDAP, context: authlog.LDAP,
			path: "/auth", username: "ldap-user", userID: 2,
			badSecret: "wrong-password-secret", badStatus: http.StatusUnprocessableEntity,
			badBody:  `{"Username":"ldap-user","Password":"wrong-password-secret"}`,
			goodBody: `{"Username":"ldap-user","Password":"correct-password-secret"}`,
		},
		{
			name: "oauth", method: portainer.AuthenticationOAuth, context: authlog.OAuth,
			path: "/auth/oauth/validate", username: "oauth-user", userID: 3,
			badSecret: "oauth-error-code-secret", badStatus: http.StatusInternalServerError,
			badBody:  `{"Code":"oauth-error-code-secret"}`,
			goodBody: `{"Code":"oauth-code-secret"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			_, store := datastore.MustNewTestStore(t, true, false)
			settings, err := store.Settings().Settings()
			require.NoError(t, err)
			settings.AuthenticationMethod = test.method
			require.NoError(t, store.Settings().UpdateSettings(settings))
			if test.method == portainer.AuthenticationLDAP {
				// LDAP does not treat user ID 1 as an internal-only initial admin.
				require.NoError(t, store.User().Create(&portainer.User{Username: "bootstrap-admin", Role: portainer.AdministratorRole}))
			}

			user := &portainer.User{ID: test.userID, Username: test.username, Role: portainer.StandardUserRole}
			if test.method == portainer.AuthenticationInternal {
				user.Role = portainer.AdministratorRole
				user.Password, err = (crypto.Service{}).Hash("correct-password-secret")
				require.NoError(t, err)
			}
			require.NoError(t, store.User().Create(user))

			jwtService, err := jwt.NewService("1h", store)
			require.NoError(t, err)
			factory, err := cli.NewClientFactory(nil, nil, nil, "", "", "")
			require.NoError(t, err)
			handler := NewHandler(testhelpers.NewTestRequestBouncer(), security.NewRateLimiter(10, time.Minute, time.Hour, nil), alwaysStrong{}, factory)
			handler.DataStore = store
			handler.CryptoService = crypto.Service{}
			handler.JWTService = jwtService
			handler.KubernetesClientFactory = factory
			handler.AuthLog = authlog.NewStore(dataDir)

			if test.method == portainer.AuthenticationLDAP {
				handler.LDAPService = testLDAPService{}
			}
			if test.method == portainer.AuthenticationOAuth {
				handler.OAuthService = testOAuthService{}
			}

			badRequest := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.badBody))
			badResponse := httptest.NewRecorder()
			handler.ServeHTTP(badResponse, badRequest)
			goodRequest := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.goodBody))
			goodResponse := httptest.NewRecorder()
			handler.ServeHTTP(goodResponse, goodRequest)

			require.Equal(t, test.badStatus, badResponse.Code)
			require.Equal(t, http.StatusOK, goodResponse.Code)
			events, status, err := handler.AuthLog.Read(context.Background())
			require.NoError(t, err)
			require.Equal(t, authlog.SourceStatus{Available: true}, status)
			require.Len(t, events, 2)
			require.Equal(t, test.context, events[0].Context)
			require.Equal(t, authlog.Failure, events[0].Type)
			require.Equal(t, badResponse.Code, events[0].StatusCode)
			require.Equal(t, test.context, events[1].Context)
			require.Equal(t, authlog.Success, events[1].Type)
			require.Equal(t, goodResponse.Code, events[1].StatusCode)
			require.Equal(t, test.username, events[1].Username)

			contents := readAuthLogFiles(t, dataDir)
			require.NotContains(t, contents, test.badSecret)
			require.NotContains(t, contents, "wrong-password-secret")
			require.NotContains(t, contents, "correct-password-secret")
			require.NotContains(t, contents, "oauth-error-code-secret")
		})
	}
}

type alwaysStrong struct{}

func (alwaysStrong) Check(string) bool { return true }

type testLDAPService struct{ portainer.LDAPService }

func (testLDAPService) AuthenticateUser(_, password string, _ *portainer.LDAPSettings) error {
	if password == "wrong-password-secret" {
		return httperrors.ErrUnauthorized
	}
	return nil
}

type testOAuthService struct{ portainer.OAuthService }

func (testOAuthService) Authenticate(_ context.Context, code string, _ *portainer.OAuthSettings) (string, error) {
	if code == "oauth-error-code-secret" {
		return "", errors.New("access_token=oauth-access-token-secret")
	}
	return "oauth-user", nil
}

func readAuthLogFiles(t *testing.T, datastorePath string) string {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(datastorePath, "logs", "authentication"))
	require.NoError(t, err)
	var contents strings.Builder
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(datastorePath, "logs", "authentication", file.Name()))
		require.NoError(t, err)
		contents.Write(data)
	}
	return contents.String()
}
