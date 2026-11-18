package useractivity

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/apikey"
	"github.com/portainer/portainer/api/authlog"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/internal/testhelpers"
	"github.com/portainer/portainer/api/jwt"

	"github.com/stretchr/testify/require"
)

func TestAuthLogsFilterHalfOpenTimeAndPaginateJSON(t *testing.T) {
	dataDir := t.TempDir()
	logs := authlog.NewStore(dataDir)
	base := time.Now().UTC().Truncate(time.Hour).Unix()
	events := []authlog.Event{
		activityTestEvent("at-after", base, authlog.Internal, authlog.Success, "alice"),
		activityTestEvent("different-context", base+1, authlog.LDAP, authlog.Success, "alice"),
		activityTestEvent("at-before", base+3, authlog.Internal, authlog.Success, "alice"),
		activityTestEvent("second-match", base+2, authlog.Internal, authlog.Success, "alice"),
	}
	for _, event := range events {
		require.NoError(t, logs.Append(event))
	}

	handler := NewHandler(testhelpers.NewTestRequestBouncer(), nil, logs)
	request := httptest.NewRequest(http.MethodGet, "/useractivity/authlogs?after="+itoa(base)+"&before="+itoa(base+3)+"&contexts=1&types=1&sortBy=timestamp&sortDesc=false&limit=1&offset=0&username=lic", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	var body result[authlog.Event]
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, 2, body.TotalCount)
	require.Len(t, body.Logs, 1)
	require.Equal(t, "at-after", body.Logs[0].ID, "after is inclusive and results sort ascending")
	require.Equal(t, authlog.SourceStatus{Available: true}, body.SourceStatus)
}

func TestAuthLogsCSVFiltersAndExportsMoreThanTwoThousandRows(t *testing.T) {
	const matching = 2200
	dataDir := t.TempDir()
	logs := authlog.NewStore(dataDir)
	base := time.Now().UTC().Truncate(time.Hour).Unix()
	for i := range matching + 5 {
		context := authlog.Internal
		typeValue := authlog.Success
		username := "user-" + zeroPad(i, 4)
		if i == 0 {
			username = "=SUM(1,1)"
		}
		if i >= matching {
			context = authlog.LDAP
		}
		require.NoError(t, logs.Append(activityTestEvent("event-"+zeroPad(i, 4), base+int64(i), context, typeValue, username)))
	}

	handler := NewHandler(testhelpers.NewTestRequestBouncer(), nil, logs)
	query := "/useractivity/authlogs.csv?after=" + itoa(base) + "&before=" + itoa(base+int64(matching+5)) + "&contexts=1&types=1&sortBy=username&sortDesc=false&offset=2000&limit=25"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, query, nil))

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, `attachment; filename="authentication-logs.csv"`, response.Header().Get("Content-Disposition"))
	content := response.Body.String()
	require.True(t, strings.HasPrefix(content, "\ufeff"), "CSV should begin with a UTF-8 BOM")
	require.Contains(t, content, "\r\n", "RFC 4180 records use CRLF")
	require.NotContains(t, strings.ReplaceAll(content, "\r\n", ""), "\n", "CSV must not contain bare LF line endings")

	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(content, "\ufeff"))).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, matching+1, "CSV applies the same filters but ignores table paging")
	require.Equal(t, []string{"Time", "User", "Method", "Event", "Result / HTTP status", "Origin", "Rate limited sample", "Suppressed rate limited"}, records[0])
	require.Equal(t, "'=SUM(1,1)", records[1][1], "formula-leading values are escaped and quoted by the CSV writer")
	require.Equal(t, "false", records[1][6])
	require.Equal(t, "0", records[1][7])
	require.Equal(t, "user-0001", records[2][1], "CSV uses the same ascending username sort")

	jsonResponse := httptest.NewRecorder()
	jsonPath := strings.Replace(query, ".csv?", "?", 1)
	handler.ServeHTTP(jsonResponse, httptest.NewRequest(http.MethodGet, jsonPath, nil))
	var page result[authlog.Event]
	require.NoError(t, json.Unmarshal(jsonResponse.Body.Bytes(), &page))
	require.Equal(t, matching, page.TotalCount)
	require.Len(t, page.Logs, 25)
}

func TestAuthLogsCSVRejectsCorruptSourceBeforeWritingCSVHeaders(t *testing.T) {
	dataDir := t.TempDir()
	logs := authlog.NewStore(dataDir)
	filename := time.Now().UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(dataDir, "logs", "authentication", filename)
	require.NoError(t, os.WriteFile(path, []byte("not-json\n"), 0600))
	handler := NewHandler(testhelpers.NewTestRequestBouncer(), nil, logs)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/useractivity/authlogs.csv", nil))

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Header().Get("Content-Type"), "text/csv")
	require.Empty(t, response.Header().Get("Content-Disposition"))
	require.False(t, strings.HasPrefix(response.Body.String(), "\ufeff"))
}

func TestAllUserActivityRoutesRequireGlobalAdministrator(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	admin := &portainer.User{ID: 1, Username: "global-admin", Role: portainer.AdministratorRole}
	operator := &portainer.User{ID: 2, Username: "environment-operator", Role: portainer.StandardUserRole}
	require.NoError(t, store.User().Create(admin))
	require.NoError(t, store.User().Create(operator))

	jwtService, err := jwt.NewService("1h", store)
	require.NoError(t, err)
	apiKeyService := apikey.NewAPIKeyService(store.APIKeyRepository(), store.User())
	bouncer := security.NewRequestBouncer(t.Context(), store, jwtService, apiKeyService)
	adminToken, _, err := jwtService.GenerateToken(&portainer.TokenData{ID: admin.ID, Username: admin.Username, Role: admin.Role})
	require.NoError(t, err)
	operatorToken, _, err := jwtService.GenerateToken(&portainer.TokenData{ID: operator.ID, Username: operator.Username, Role: operator.Role})
	require.NoError(t, err)
	handler := NewHandler(bouncer, store, authlog.NewStore(t.TempDir()))

	for _, path := range []string{"/useractivity/authlogs", "/useractivity/authlogs.csv", "/useractivity/logs", "/useractivity/logs.csv"} {
		t.Run(path, func(t *testing.T) {
			adminResponse := httptest.NewRecorder()
			adminRequest := httptest.NewRequest(http.MethodGet, path, nil)
			adminRequest.Header.Set("Authorization", "Bearer "+adminToken)
			handler.ServeHTTP(adminResponse, adminRequest)
			require.Equal(t, http.StatusOK, adminResponse.Code)

			operatorResponse := httptest.NewRecorder()
			operatorRequest := httptest.NewRequest(http.MethodGet, path, nil)
			operatorRequest.Header.Set("Authorization", "Bearer "+operatorToken)
			handler.ServeHTTP(operatorResponse, operatorRequest)
			require.Equal(t, http.StatusForbidden, operatorResponse.Code)
		})
	}
}

func TestActivitySnapshotUsesSafeMetadataConfigurationAndCurrentDeployment(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: 1, Name: "production"}))
	knownActor := &portainer.User{Username: "known-actor"}
	require.NoError(t, store.User().Create(knownActor))
	base := time.Now().UTC().Unix()
	stack := &portainer.Stack{
		ID: 7, Name: "payments", EndpointID: 1,
		CreationDate: base, CreatedBy: "creator",
		UpdateDate: base + 4, UpdatedBy: "updater",
		Env:                   []portainer.Pair{{Name: "API_TOKEN", Value: "fixture-token-secret"}},
		CurrentDeploymentInfo: &portainer.StackDeploymentInfo{RepositoryURL: "https://user:repo-token-secret@example.invalid/repo"},
		Configuration: &portainer.StackConfiguration{Events: []portainer.StackConfigEvent{
			{RevisionID: "rev", Action: "upload", UserID: 900, Time: base + 1, Success: true},
			{RevisionID: "rev2", Action: "apply", UserID: knownActor.ID, Time: base + 2, Success: false},
		}},
		DeploymentStatus: []portainer.StackDeploymentStatus{
			{Status: portainer.StackStatusDeploying, Time: base + 3, Message: "raw deployment error fixture"},
		},
	}
	require.NoError(t, store.Stack().Create(stack))

	rows, err := activitySnapshot(t.Context(), store)
	require.NoError(t, err)
	require.Len(t, rows, 5, "metadata, two configuration events, current deployment, and last update are projected")
	byAction := make(map[string]Activity)
	for _, row := range rows {
		byAction[row.Source+"/"+row.Action] = row
		require.Equal(t, "production", row.Context)
		require.Equal(t, 1, row.EnvironmentID)
		require.Equal(t, 7, row.StackID)
		require.Equal(t, "payments", row.StackName)
		require.Equal(t, []string{"summary"}, sortedStringKeys(row.Payload))
	}
	require.Equal(t, "creator", byAction["metadata/create"].Username)
	require.Equal(t, "unknown", byAction["metadata/create"].Status)
	require.Empty(t, byAction["configuration/upload"].Username, "unknown user IDs stay unknown")
	require.Equal(t, "failure", byAction["configuration/apply"].Status)
	require.Equal(t, "known-actor", byAction["configuration/apply"].Username)
	require.Empty(t, byAction["deployment/deployment"].Username)
	require.Equal(t, "in_progress", byAction["deployment/deployment"].Status)
	require.Equal(t, "updater", byAction["metadata/update"].Username)

	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	for _, secret := range []string{"fixture-token-secret", "repo-token-secret", "raw deployment error fixture"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func activityTestEvent(id string, timestamp int64, context, eventType int, username string) authlog.Event {
	return authlog.Event{ID: id, Timestamp: timestamp, Username: username, Context: context, Type: eventType, Origin: "198.51.100.1", StatusCode: http.StatusOK}
}

func itoa(value int64) string { return strconv.FormatInt(value, 10) }

func zeroPad(value, width int) string { return fmt.Sprintf("%0*d", width, value) }

func sortedStringKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
