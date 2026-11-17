package users

import (
	"encoding/json"
	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/pkg/authorization"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestEffectiveViewerShowsSavedTeamPolicyAndDisabledRole(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	user := &portainer.User{ID: 2, Role: portainer.StandardUserRole}
	require.NoError(t, store.User().Update(user.ID, user))
	require.NoError(t, store.Team().Update(4, &portainer.Team{ID: 4, Name: "Operations"}))
	require.NoError(t, store.TeamMembership().Create(&portainer.TeamMembership{UserID: user.ID, TeamID: 4}))
	require.NoError(t, store.EndpointGroup().Update(5, &portainer.EndpointGroup{ID: 5, Name: "Production", TeamAccessPolicies: portainer.TeamAccessPolicies{4: {StandaloneRole: portainer.StandaloneOperator}}}))
	require.NoError(t, store.Endpoint().Create(&portainer.Endpoint{ID: 1, Name: "Host", GroupID: 5, Type: portainer.DockerEnvironment}))
	role := authorization.StandaloneRoleDefinition(portainer.StandaloneOperator)
	role.StandalonePolicy = authorization.DefaultStandalonePolicy(portainer.StandaloneOperator)
	role.StandalonePolicy.Enabled = false
	role.StandalonePolicy.Version = 8
	require.NoError(t, store.Role().Update(role.ID, role))
	r := mux.SetURLVars(httptest.NewRequest("GET", "/users/2/effective-access", nil), map[string]string{"id": "2"})
	r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{ID: 1, Role: portainer.AdministratorRole}))
	w := httptest.NewRecorder()
	h := &Handler{DataStore: store}
	require.Nil(t, h.userEffectiveAccess(w, r))
	var entries []EffectiveAccessEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &entries))
	require.Len(t, entries, 1)
	require.Equal(t, "Operations", entries[0].TeamName)
	require.Equal(t, "Production", entries[0].GroupName)
	require.False(t, entries[0].Policy.Enabled)
	require.Equal(t, 8, entries[0].Policy.Version)
	require.Len(t, entries[0].Features, len(authorization.StandaloneFeatures))
	require.NotEmpty(t, entries[0].Restrictions)
	require.NoError(t, h.decorateStandaloneRoles(user))
	require.Empty(t, user.EndpointAuthorizations[1])
}
