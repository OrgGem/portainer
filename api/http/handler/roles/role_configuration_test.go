package roles

import (
	"bytes"
	"encoding/json"
	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestRolePoliciesPersistenceValidationAndConflict(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	require.NoError(t, store.UpdateTx(authorization.EnsureStandalonePolicies))
	h := &Handler{DataStore: store}
	p, err := authorization.ReadStandalonePolicy(store, portainer.StandaloneStandard)
	require.NoError(t, err)
	require.True(t, p.Enabled)
	require.True(t, p.Features["containers.operate"])
	p.Features["containers.operate"] = false
	save := func(p *portainer.StandaloneRolePolicy) *httptest.ResponseRecorder {
		b, _ := json.Marshal(p)
		r := mux.SetURLVars(httptest.NewRequest("PUT", "/roles/3/policy", bytes.NewReader(b)), map[string]string{"id": "3"})
		w := httptest.NewRecorder()
		e := h.rolePolicyUpdate(w, r)
		if e != nil {
			w.Code = e.StatusCode
		}
		return w
	}
	require.Equal(t, 200, save(p).Code)
	require.Equal(t, 409, save(p).Code, "stale edits must not overwrite")
	require.NoError(t, store.UpdateTx(authorization.EnsureStandalonePolicies))
	saved, err := authorization.ReadStandalonePolicy(store, portainer.StandaloneStandard)
	require.NoError(t, err)
	require.False(t, saved.Features["containers.operate"])
	require.Equal(t, 2, saved.Version)
	saved.Features["volumes.files"] = true
	require.Equal(t, 400, save(saved).Code, "cannot expand role scope")
	delete(saved.Features, "volumes.files")
	require.Equal(t, 400, save(saved).Code, "missing fields must not silently enable rights")
}
