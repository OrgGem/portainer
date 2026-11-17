package users

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/stretchr/testify/require"
)

func TestReviewMeReturnsScopedRoles(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	user := &portainer.User{ID: 2, Username: "review", Role: portainer.StandardUserRole}
	require.NoError(t, store.User().Create(user))
	endpoint := &portainer.Endpoint{ID: 1, Type: portainer.DockerEnvironment, UserAccessPolicies: portainer.UserAccessPolicies{user.ID: {StandaloneRole: portainer.StandaloneReadOnly}}}
	require.NoError(t, store.Endpoint().Create(endpoint))
	h := &Handler{DataStore: store}
	r := httptest.NewRequest("GET", "/users/me", nil)
	r = r.WithContext(security.StoreRestrictedRequestContext(r, &security.RestrictedRequestContext{UserID: user.ID, User: user}))
	r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: user.ID, Role: portainer.StandardUserRole}))
	me := httptest.NewRecorder()
	require.Nil(t, h.userInspectMe(me, r))
	var meUser portainer.User
	require.NoError(t, json.Unmarshal(me.Body.Bytes(), &meUser))
	require.Equal(t, portainer.StandaloneReadOnly, meUser.StandaloneEndpointRoles[endpoint.ID])
	byID := httptest.NewRecorder()
	require.Nil(t, h.userInspect(byID, mux.SetURLVars(r, map[string]string{"id": fmt.Sprint(user.ID)})))
	var idUser portainer.User
	require.NoError(t, json.Unmarshal(byID.Body.Bytes(), &idUser))
	require.Equal(t, portainer.StandaloneReadOnly, idUser.StandaloneEndpointRoles[endpoint.ID])
}
