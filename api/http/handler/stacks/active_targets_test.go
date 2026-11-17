package stacks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/stretchr/testify/require"
)

func TestMissingServiceRejectsApplyAndRollbackIncludingActiveRevision(t *testing.T) {
	for _, action := range []string{"apply", "rollback"} {
		for _, revision := range []string{"active", "previous"} {
			t.Run(action+"/"+revision, func(t *testing.T) {
				h, store, stack, _ := managedFixture(t)
				stack.Configuration.Targets[0].ActiveRevision = "active"
				stack.Configuration.Revisions = []portainer.StackConfigRevision{{ID: "active", TargetID: "web-config", AppliedAt: 1}, {ID: "previous", TargetID: "web-config", AppliedAt: 1}}
				require.NoError(t, store.Stack().Update(stack.ID, stack))
				require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  side:\n    image: nginx\n"), 0600))
				r := mux.SetURLVars(operatorRequest("POST", "/stacks/1/configurations/"+revision+"/"+action, `{"Version":1}`, true), map[string]string{"id": "1", "revision": revision})
				err := h.stackConfigApply(httptest.NewRecorder(), r)
				require.NotNil(t, err)
				require.Equal(t, http.StatusConflict, err.StatusCode)
				stored, readErr := store.Stack().Read(stack.ID)
				require.NoError(t, readErr)
				require.Equal(t, stack.Configuration, stored.Configuration)
			})
		}
	}
}

func TestRetainedStaleTargetDoesNotAuthorizeNewTargetForMissingService(t *testing.T) {
	h, _, stack, _ := managedFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  side:\n    image: nginx\n"), 0600))
	err := h.stackConfigTargets(httptest.NewRecorder(), operatorRequest("PUT", "/stacks/1/configurations/targets", `{"Version":1,"Targets":[{"ID":"web-config","Service":"web","Target":"/etc/app.conf"},{"ID":"new","Service":"web","Target":"/etc/other.conf"}]}`, true))
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
}

func TestAsyncDeploymentKeepsTargetsLockedUntilPostDeployCompletes(t *testing.T) {
	for _, deployErr := range []error{nil, errors.New("deployment failed")} {
		name := "success"
		if deployErr != nil {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			h, store, stack, _ := managedFixture(t)
			stack.Status = portainer.StackStatusDeploying
			stack.DeploymentStartStatus = portainer.StackStatusActive
			require.NoError(t, store.Stack().Update(stack.ID, stack))
			called := false
			stackDeploy(store, stack.ID, &stubStackDeploymentConfiger{deployErr: deployErr}, func(_ context.Context, err error) {
				called = true
				require.Equal(t, deployErr, err)
				current, err := store.Stack().Read(stack.ID)
				require.NoError(t, err)
				require.Equal(t, portainer.StackStatusDeploying, current.Status)
				errHandler := h.stackAccess(portainer.StackEdit, func(http.ResponseWriter, *http.Request) *httperror.HandlerError {
					t.Fatal("target mutation must wait until Compose cleanup/rollback completes")
					return nil
				})(httptest.NewRecorder(), operatorRequest("PUT", "/stacks/1/configurations/targets", `{}`, true))
				require.NotNil(t, errHandler)
				require.Equal(t, http.StatusConflict, errHandler.StatusCode)
			})
			require.True(t, called)
			current, err := store.Stack().Read(stack.ID)
			require.NoError(t, err)
			expected := portainer.StackStatusActive
			if deployErr != nil {
				expected = portainer.StackStatusError
			}
			require.Equal(t, expected, current.Status)
		})
	}
}
