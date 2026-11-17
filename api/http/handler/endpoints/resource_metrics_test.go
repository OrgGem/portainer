package endpoints

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/docker/resourcemetrics"
	"github.com/stretchr/testify/require"
)

func TestMetricsInspectIsNotCached(t *testing.T) {
	allowed := true
	calls := 0
	reader := metricsReader{r: httptest.NewRequest("GET", "/metrics", nil), cache: resourcemetrics.NewCache(), proxy: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !allowed {
			w.WriteHeader(403)
			return
		}
		_, _ = w.Write([]byte(`{"Id":"test","Config":{"Env":["SECRET=hidden"]}}`))
	})}
	var inspect resourcemetrics.Inspect
	_, err := reader.read("/containers/test/json", &inspect, 0)
	require.NoError(t, err)
	require.Equal(t, "test", inspect.ID)
	allowed = false
	_, err = reader.read("/containers/test/json", &inspect, 0)
	require.Error(t, err)
	require.Equal(t, 2, calls)
}

func TestLocalHostMetricsNeverAppliedToRemoteOrAgent(t *testing.T) {
	t.Setenv("PORTAINER_METRICS_HOST_ROOT", "/host")
	t.Setenv("PORTAINER_METRICS_ENDPOINT_URL", "")
	require.Equal(t, "/host", localMetricsRoot(&portainer.Endpoint{Type: portainer.DockerEnvironment, URL: "unix:///var/run/docker.sock"}))
	for _, ep := range []*portainer.Endpoint{{Type: portainer.DockerEnvironment, URL: "tcp://remote:2375"}, {Type: portainer.AgentOnDockerEnvironment, URL: "unix:///var/run/docker.sock"}} {
		require.Empty(t, localMetricsRoot(ep))
	}
}

func TestMetricsRecorderBoundAndCachedMeasurements(t *testing.T) {
	w := &metricsRecorder{header: make(http.Header)}
	_, err := w.Write(make([]byte, 8<<20+1))
	require.Error(t, err)
	require.True(t, w.overflow)
	calls := 0
	r := metricsReader{r: httptest.NewRequest("GET", "/metrics", nil), cache: resourcemetrics.NewCache(), proxy: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(`{"NCPU":2}`)) })}
	var info resourcemetrics.Info
	for i := 0; i < 2; i++ {
		_, err = r.read("/info", &info, time.Second)
		require.NoError(t, err)
	}
	require.Equal(t, 1, calls)
	require.Equal(t, 2, info.NCPU)
}
