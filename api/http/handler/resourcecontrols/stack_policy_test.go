package resourcecontrols

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
)

// Simulate an administrator enabling a policy between the handler's first read
// and its ownership write, without relying on scheduler timing.
type policyBeforeWriteStore struct {
	dataservices.DataStore
	before func()
}

func (s policyBeforeWriteStore) UpdateTx(fn func(dataservices.DataStoreTx) error) error {
	s.before()
	return s.DataStore.UpdateTx(fn)
}

func TestOwnershipUpdateCannotOverwriteNewStackPolicy(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprintf("admin=%v", admin), func(t *testing.T) {
			_, store := datastore.MustNewTestStore(t, true, false)
			control := authorization.NewPrivateResourceControl("1_app", portainer.StackResourceControl, 2)
			require.NoError(t, store.ResourceControl().Create(control))
			policy := &portainer.StackAccessPolicy{Version: 1, Grants: []portainer.StackAccessGrant{{UserID: 2, Actions: []portainer.StackAction{portainer.StackView}}}}
			h := &Handler{DataStore: policyBeforeWriteStore{DataStore: store, before: func() {
				current, err := store.ResourceControl().Read(control.ID)
				require.NoError(t, err)
				current.StackPolicy = policy
				require.NoError(t, store.ResourceControl().Update(current.ID, current))
			}}}
			r := httptest.NewRequest(http.MethodPut, "/resource_controls/1", strings.NewReader(`{"Public":true}`))
			r = mux.SetURLVars(r, map[string]string{"id": fmt.Sprint(control.ID)})
			r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{IsAdmin: admin, UserID: 2}))
			herr := h.resourceControlUpdate(httptest.NewRecorder(), r)
			if admin {
				require.Nil(t, herr)
			} else {
				require.NotNil(t, herr)
				require.Equal(t, http.StatusForbidden, herr.StatusCode)
			}
			current, err := store.ResourceControl().Read(control.ID)
			require.NoError(t, err)
			require.Equal(t, policy, current.StackPolicy)
			require.Equal(t, admin, current.Public)
		})
	}
}
