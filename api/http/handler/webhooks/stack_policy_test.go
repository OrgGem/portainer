package webhooks

import (
	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/datastore"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedEndpointRejectsPublicWebhook(t *testing.T) {
	_, store := datastore.MustNewTestStore(t, true, false)
	require.NoError(t, store.ResourceControl().Create(&portainer.ResourceControl{ResourceID: "2_protected", Type: portainer.StackResourceControl, StackPolicy: &portainer.StackAccessPolicy{}}))
	require.NoError(t, store.Webhook().Create(&portainer.Webhook{Token: "test-token", EndpointID: 2, WebhookType: portainer.ServiceWebhook}))
	h := &Handler{DataStore: store}
	r := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/webhooks/test-token", nil), map[string]string{"token": "test-token"})
	err := h.webhookExecute(httptest.NewRecorder(), r)
	require.NotNil(t, err)
	require.Equal(t, http.StatusForbidden, err.StatusCode)
}
