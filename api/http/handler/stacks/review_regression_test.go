package stacks

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/stacks/deployments"
	"github.com/portainer/portainer/api/stacks/stackutils"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/stretchr/testify/require"
)

type reviewMigrationBouncer struct {
	security.BouncerService
	target  portainer.EndpointID
	checked []portainer.EndpointID
}

func (b *reviewMigrationBouncer) AuthorizedEndpointOperation(_ *http.Request, e *portainer.Endpoint) error {
	b.checked = append(b.checked, e.ID)
	if e.ID == b.target {
		return errors.New("target forbidden")
	}
	return nil
}

func TestReviewMigrationRequiresTargetAuthorization(t *testing.T) {
	h, store := newStackStartHandler(t)
	user := &portainer.User{Username: "legacy-source", Role: portainer.StandardUserRole}
	require.NoError(t, store.User().Create(user))
	source := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, SecuritySettings: portainer.EndpointSecuritySettings{AllowStackManagementForRegularUsers: true}, UserAccessPolicies: portainer.UserAccessPolicies{user.ID: {RoleID: 1}}}
	target := &portainer.Endpoint{ID: 2, Type: portainer.DockerEnvironment, UserAccessPolicies: portainer.UserAccessPolicies{user.ID: {StandaloneRole: portainer.StandaloneReadOnly}}}
	require.NoError(t, store.Endpoint().Create(source))
	require.NoError(t, store.Endpoint().Create(target))
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: stackutils.ResourceControlID(target.ID, "protected"), Type: portainer.StackResourceControl, StackPolicy: &portainer.StackAccessPolicy{}}))
	stack := &portainer.Stack{ID: 1, Name: "source-app", EndpointID: source.ID, Type: portainer.DockerComposeStack, ProjectPath: t.TempDir(), EntryPoint: "compose.yml"}
	require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  app:\n    image: alpine:3.20\n"), 0600))
	require.NoError(t, store.Stack().Create(stack))
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: stackutils.ResourceControlID(source.ID, stack.Name), Type: portainer.StackResourceControl, Public: true}))
	fileService, err := filesystem.NewService(t.TempDir(), "")
	require.NoError(t, err)
	h.FileService = fileService
	bouncer := &reviewMigrationBouncer{target: target.ID}
	h.requestBouncer = bouncer
	deployer := &stubStackDeployer{deployErr: errors.New("review: stop before any real deployment")}
	h.StackDeployer = deployer
	r := operatorRequest("POST", fmt.Sprintf("/stacks/1/migrate?endpointId=%d", source.ID), fmt.Sprintf(`{"EndpointID":%d}`, target.ID), false)
	r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{UserID: user.ID, User: user}))
	r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: user.ID, Role: portainer.StandardUserRole}))
	result := h.stackAccess(portainer.StackEdit, h.stackMigrate)(httptest.NewRecorder(), r)
	require.NotNil(t, result)
	require.False(t, deployer.composeDeployed)
	require.Equal(t, []portainer.EndpointID{source.ID, target.ID}, bouncer.checked)
}

func TestReviewEnvironmentAdminComposeValidation(t *testing.T) {
	fileService, err := filesystem.NewService(t.TempDir(), "")
	require.NoError(t, err)
	stack := &portainer.Stack{Name: "review", Type: portainer.DockerComposeStack, ProjectPath: t.TempDir(), EntryPoint: "compose.yml"}
	require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  app:\n    image: alpine:3.20\n    privileged: true\n"), 0600))
	endpoint := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment}
	user := &portainer.User{Role: portainer.StandardUserRole, StandaloneEndpointRoles: map[portainer.EndpointID]portainer.StandaloneRole{1: portainer.StandaloneEnvironmentAdmin}}
	err = deployments.ValidateStackForUser(stack, endpoint, user, &stubStackDeployer{}, fileService, true)
	require.NoError(t, err)
	user.Role = portainer.AdministratorRole
	require.NoError(t, deployments.ValidateStackForUser(stack, endpoint, user, &stubStackDeployer{}, fileService))
}

func TestReviewStackFlagEnforced(t *testing.T) {
	h, store := newStackStartHandler(t)
	user := &portainer.User{Role: portainer.StandardUserRole}
	require.NoError(t, store.User().Create(user))
	endpoint := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, SecuritySettings: portainer.EndpointSecuritySettings{AllowStackManagementForRegularUsers: false}}
	ctx := &security.RestrictedRequestContext{UserID: user.ID, RoleEndpointID: 1, StandaloneRole: portainer.StandaloneStandard}
	allowed, err := h.userCanManageStacks(ctx, endpoint)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestReviewStaleConfigurationTarget(t *testing.T) {
	h, store, stack, _ := managedFixture(t)
	stack.Configuration.Revisions = []portainer.StackConfigRevision{{ID: "draft", TargetID: "web-config"}}
	require.NoError(t, store.Stack().Update(stack.ID, stack))
	require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  api:\n    image: nginx\n"), 0600))
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"keep stale", `{"Version":1,"Targets":[{"ID":"web-config","Service":"web","Target":"/etc/app.conf"}]}`, 200},
		{"remove stale", `{"Version":1,"Targets":[]}`, 200},
		{"rename service", `{"Version":1,"Targets":[{"ID":"web-config","Service":"api","Target":"/etc/app.conf"}]}`, 409},
		{"add replacement", `{"Version":1,"Targets":[{"ID":"web-config","Service":"web","Target":"/etc/app.conf"},{"ID":"new-config","Service":"api","Target":"/etc/app.conf"}]}`, 200},
	} {
		require.NoError(t, store.Stack().Update(stack.ID, stack))
		err := h.stackConfigTargets(httptest.NewRecorder(), operatorRequest("PUT", "/stacks/1/configurations/targets", tc.body, true))
		if tc.status == 200 {
			require.Nil(t, err, tc.name)
		} else {
			require.NotNil(t, err, tc.name)
			require.Equal(t, tc.status, err.StatusCode, tc.name)
		}
	}
}

func TestReviewIndependentStackMutations(t *testing.T) {
	h, store, stack, _ := managedFixture(t)
	second := *stack
	second.ID = 2
	second.Name = "another"
	second.Configuration = nil
	require.NoError(t, store.Stack().Create(&second))
	enteredA, releaseA, enteredB, doneA, doneB := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	nextA := func(http.ResponseWriter, *http.Request) *httperror.HandlerError {
		close(enteredA)
		<-releaseA
		return nil
	}
	nextB := func(http.ResponseWriter, *http.Request) *httperror.HandlerError { close(enteredB); return nil }
	go func() {
		defer close(doneA)
		h.stackAccess(portainer.StackView, nextA)(httptest.NewRecorder(), operatorRequest("POST", "/stacks/1/start", "{}", true))
	}()
	<-enteredA
	go func() {
		defer close(doneB)
		r := mux.SetURLVars(operatorRequest("POST", "/stacks/2/start", "{}", true), map[string]string{"id": "2"})
		h.stackAccess(portainer.StackView, nextB)(httptest.NewRecorder(), r)
	}()
	select {
	case <-enteredB:
	case <-time.After(time.Second):
		t.Error("Independent stack blocked")
	}
	close(releaseA)
	<-doneA
	<-doneB
}

func TestReviewRetiredConfigurationTargetCannotBeReused(t *testing.T) {
	h, store, stack, _ := managedFixture(t)
	stack.Configuration.Targets = nil
	stack.Configuration.Revisions = []portainer.StackConfigRevision{{ID: "draft", TargetID: "web-config"}}
	require.NoError(t, store.Stack().Update(stack.ID, stack))
	err := h.stackConfigTargets(httptest.NewRecorder(), operatorRequest("PUT", "/stacks/1/configurations/targets", `{"Version":1,"Targets":[{"ID":"web-config","Service":"web","Target":"/etc/app.conf"}]}`, true))
	require.NotNil(t, err)
	require.Equal(t, http.StatusConflict, err.StatusCode)
}
