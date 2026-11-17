package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/stacks/stackconfig"
	"github.com/portainer/portainer/pkg/authorization"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/stretchr/testify/require"
)

func managedFixture(t *testing.T) (*Handler, *datastore.Store, *portainer.Stack, *portainer.Endpoint) {
	t.Helper()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", t.TempDir())
	h, store := newStackStartHandler(t)
	endpoint, err := mockCreateEndpoint(store)
	require.NoError(t, err)
	endpoint.Type, endpoint.URL = portainer.DockerEnvironment, "unix:///var/run/docker.sock"
	require.NoError(t, store.Endpoint().UpdateEndpoint(endpoint.ID, endpoint))
	stack := &portainer.Stack{ID: 1, Name: "app", EndpointID: endpoint.ID, Type: portainer.DockerComposeStack, Status: portainer.StackStatusActive, ProjectPath: t.TempDir(), EntryPoint: "compose.yml", Configuration: &portainer.StackConfiguration{Version: 1, Targets: []portainer.StackConfigTarget{{ID: "web-config", Service: "web", Target: "/etc/app.conf"}}}}
	require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  web:\n    image: nginx\n"), 0600))
	require.NoError(t, store.Stack().Create(stack))
	control := authorization.NewPrivateResourceControl("1_app", portainer.StackResourceControl, 2)
	control.StackPolicy = &portainer.StackAccessPolicy{Version: 1, Grants: []portainer.StackAccessGrant{{UserID: 2, Actions: []portainer.StackAction{portainer.StackView, portainer.StackConfigUpload}}}}
	require.NoError(t, store.ResourceControl().Create(control))
	return h, store, stack, endpoint
}

func operatorRequest(method, url, body string, admin bool) *http.Request {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	role := portainer.StandardUserRole
	if admin {
		role = portainer.AdministratorRole
	}
	r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{IsAdmin: admin, UserID: 2, User: &portainer.User{ID: 2, Role: role, Username: "operator"}}))
	r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: 2, Role: role}))
	return mux.SetURLVars(r, map[string]string{"id": "1"})
}

func TestManagedStackActionMatrix(t *testing.T) {
	h, _, _, _ := managedFixture(t)
	for _, tc := range []struct {
		action  portainer.StackAction
		allowed bool
	}{{portainer.StackView, true}, {portainer.StackConfigUpload, true}, {portainer.StackConfigApply, false}, {portainer.StackEdit, false}, {portainer.StackDeploy, false}} {
		called := false
		next := func(http.ResponseWriter, *http.Request) *httperror.HandlerError { called = true; return nil }
		err := h.stackAccess(tc.action, next)(httptest.NewRecorder(), operatorRequest("POST", "/stacks/1", `{}`, false))
		require.Equal(t, tc.allowed, err == nil, string(tc.action))
		require.Equal(t, tc.allowed, called)
	}
	for _, url := range []string{"/stacks/1?endpointId=2", "/stacks/1/migrate", "/stacks/1/git/redeploy"} {
		err := h.stackAccess(portainer.StackView, func(http.ResponseWriter, *http.Request) *httperror.HandlerError {
			t.Fatal("must not execute")
			return nil
		})(httptest.NewRecorder(), operatorRequest("POST", url, `{}`, false))
		require.NotNil(t, err, url)
	}
}

func TestManagedConfigUploadIsDraftAndCannotApply(t *testing.T) {
	h, store, _, _ := managedFixture(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("POST", "/stacks/1/configurations", `{"Version":1,"TargetID":"web-config","Filename":"app.conf","Content":"listen=8080"}`, false))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stack, err := store.Stack().Read(1)
	require.NoError(t, err)
	require.Len(t, stack.Configuration.Revisions, 1)
	require.Empty(t, stack.Configuration.Targets[0].ActiveRevision)
	require.Zero(t, stack.Configuration.Revisions[0].AppliedAt)
	require.Equal(t, portainer.StackStatusActive, stack.Status)
	_, err = stackconfig.ReadRevision(1, stack.Configuration.Revisions[0])
	require.NoError(t, err)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("POST", "/stacks/1/configurations/"+stack.Configuration.Revisions[0].ID+"/apply", `{"Version":2}`, false))
	require.Equal(t, http.StatusForbidden, w.Code)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, operatorRequest("POST", "/stacks/1/configurations", `{"Version":1,"TargetID":"web-config","Filename":"app.conf","Content":"stale"}`, false))
	require.Equal(t, http.StatusConflict, w.Code)
}

type configApplyManager struct {
	portainer.ComposeStackManager
	calls    []string
	failures []error
	before   func(*portainer.Stack, portainer.ComposeUpOptions)
}

func (m *configApplyManager) Up(_ context.Context, s *portainer.Stack, _ *portainer.Endpoint, opts portainer.ComposeUpOptions) error {
	if m.before != nil {
		m.before(s, opts)
	}
	m.calls = append(m.calls, s.Configuration.Targets[0].ActiveRevision)
	if len(m.failures) >= len(m.calls) {
		return m.failures[len(m.calls)-1]
	}
	return nil
}

func TestManagedConfigCommitAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name            string
		failures        []error
		active, pending string
		status          portainer.StackStatus
	}{
		{"success", nil, "new", "", portainer.StackStatusActive},
		{"failed-apply-restored", []error{errors.New("unhealthy")}, "old", "", portainer.StackStatusActive},
		{"failed-recovery", []error{errors.New("unhealthy"), errors.New("daemon unavailable")}, "old", "new", portainer.StackStatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, store, stack, endpoint := managedFixture(t)
			stack.Configuration.Targets[0].ActiveRevision = "old"
			stack.Configuration.Revisions = []portainer.StackConfigRevision{{ID: "old", TargetID: "web-config", AppliedAt: 1}, {ID: "new", TargetID: "web-config"}}
			require.NoError(t, store.Stack().Update(1, stack))
			manager := &configApplyManager{failures: tc.failures}
			manager.before = func(_ *portainer.Stack, opts portainer.ComposeUpOptions) {
				persisted, err := store.Stack().Read(1)
				require.NoError(t, err)
				require.Equal(t, "old", persisted.Configuration.Targets[0].ActiveRevision)
				require.Equal(t, "new", persisted.Configuration.PendingRevision)
				require.True(t, opts.ConfigOnly)
				require.True(t, opts.ForceRecreate)
				require.Equal(t, "sha256:pinned", opts.ServiceImages["web"])
			}
			h.ComposeStackManager = manager
			err := h.applyConfigurationRevision(stack, endpoint, &security.RestrictedRequestContext{UserID: 2}, 1, 0, "apply", map[string]string{"web": "sha256:pinned"})
			require.Equal(t, len(tc.failures) == 0, err == nil)
			persisted, e := store.Stack().Read(1)
			require.NoError(t, e)
			require.Equal(t, tc.active, persisted.Configuration.Targets[0].ActiveRevision)
			require.Equal(t, tc.pending, persisted.Configuration.PendingRevision)
			require.Equal(t, tc.status, persisted.Status)
			if len(tc.failures) > 0 {
				require.Zero(t, persisted.Configuration.Revisions[1].AppliedAt)
				require.Equal(t, []string{"new", "old"}, manager.calls)
			} else {
				require.NotZero(t, persisted.Configuration.Revisions[1].AppliedAt)
			}
			if tc.pending != "" {
				manager.before = nil
				manager.failures = nil
				// Recovery uses the persisted image even when no container survives.
				w := httptest.NewRecorder()
				h.ServeHTTP(w, operatorRequest("POST", "/stacks/1/configurations/recover", `{}`, true))
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var result portainer.StackConfiguration
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
				require.Empty(t, result.PendingRevision)
				require.Equal(t, "old", result.Targets[0].ActiveRevision)
			}
		})
	}
}
