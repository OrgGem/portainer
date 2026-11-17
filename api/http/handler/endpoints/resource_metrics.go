package endpoints

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/gorilla/mux"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/docker/resourcemetrics"
	"github.com/portainer/portainer/api/http/security"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/request"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type metricsReader struct {
	r     *http.Request
	proxy http.Handler
	cache *resourcemetrics.Cache
	key   string
}

// Bounded recorder avoids returning raw Docker inspect data (Env, labels,
// commands, credentials) or allowing an upstream response to grow indefinitely.
type metricsRecorder struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	overflow bool
}

func (w *metricsRecorder) Header() http.Header { return w.header }
func (w *metricsRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *metricsRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	if w.body.Len()+len(b) > 8<<20 {
		w.overflow = true
		return 0, errors.New("metrics response too large")
	}
	return w.body.Write(b)
}

type metricsHTTPError struct{ status int }

func (e metricsHTTPError) Error() string {
	return fmt.Sprintf("metrics source returned HTTP %d", e.status)
}

func (m metricsReader) read(path string, target any, ttl time.Duration) (time.Time, error) {
	fetch := func() ([]byte, error) {
		r := m.r.Clone(m.r.Context())
		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		r.URL = u
		r.RequestURI = ""
		r.Method = http.MethodGet
		r.Body = nil
		r.ContentLength = 0
		r.Header = make(http.Header)
		r.Header.Set("X-PortainerAgent-Target", m.r.Header.Get("X-PortainerAgent-Target"))
		if rc, err := security.RetrieveRestrictedRequestContext(r); err == nil {
			copy := *rc
			r = r.WithContext(security.StoreRestrictedRequestContext(r, &copy))
		}
		w := &metricsRecorder{header: make(http.Header)}
		m.proxy.ServeHTTP(w, r)
		if w.overflow {
			return nil, errors.New("metrics response too large")
		}
		if w.status != 200 {
			return nil, metricsHTTPError{w.status}
		}
		return w.body.Bytes(), nil
	}
	sample, err := m.cache.Get(m.r.Context(), m.key+path, ttl, fetch)
	if err != nil {
		return time.Time{}, err
	}
	return sample.At, json.Unmarshal(sample.Data, target)
}

func (h *Handler) metricsReader(r *http.Request) (*portainer.Endpoint, *metricsReader, *httperror.HandlerError) {
	id, err := request.RetrieveNumericRouteVariableValue(r, "id")
	if err != nil {
		return nil, nil, httperror.BadRequest("Invalid environment", err)
	}
	ep, err := h.DataStore.Endpoint().Endpoint(portainer.EndpointID(id))
	if h.DataStore.IsErrObjectNotFound(err) {
		return nil, nil, httperror.NotFound("Environment not found", err)
	}
	if err != nil {
		return nil, nil, httperror.InternalServerError("Unable to read environment", err)
	}
	if err = h.requestBouncer.AuthorizedEndpointOperation(r, ep); err != nil {
		return nil, nil, httperror.Forbidden("Permission denied", err)
	}
	if ep.Type != portainer.DockerEnvironment && ep.Type != portainer.AgentOnDockerEnvironment {
		return nil, nil, httperror.BadRequest("Metrics support Docker Standalone and Docker Agent environments", nil)
	}
	p := h.ProxyManager.GetEndpointProxy(ep)
	if p == nil {
		p, err = h.ProxyManager.CreateAndRegisterEndpointProxy(ep)
		if err != nil {
			return nil, nil, httperror.InternalServerError("Unable to connect metrics source", err)
		}
	}
	token, err := security.RetrieveTokenData(r)
	if err != nil {
		return nil, nil, httperror.Forbidden("Permission denied", err)
	}
	// User scope also prevents reuse if Docker changes filtering of system/df.
	key := fmt.Sprintf("%d:%s:%d:%s:", ep.ID, ep.URL, token.ID, r.Header.Get("X-PortainerAgent-Target"))
	return ep, &metricsReader{r: r, proxy: p, cache: h.resourceMetrics, key: key}, nil
}

func localMetricsRoot(ep *portainer.Endpoint) string {
	endpointURL := os.Getenv("PORTAINER_METRICS_ENDPOINT_URL")
	if endpointURL == "" {
		endpointURL = "unix:///var/run/docker.sock"
	}
	if ep.Type != portainer.DockerEnvironment || ep.URL != endpointURL {
		return ""
	}
	return os.Getenv("PORTAINER_METRICS_HOST_ROOT")
}

var metricsContainerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

func (h *Handler) containerMetrics(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	ep, m, herr := h.metricsReader(r)
	if herr != nil {
		return herr
	}
	id := mux.Vars(r)["containerId"]
	if !metricsContainerID.MatchString(id) {
		return httperror.BadRequest("Invalid container ID", nil)
	}
	var inspect resourcemetrics.Inspect
	// Never cache ownership checks. The existing Docker proxy checks resource
	// controls, inherited stack access and current team/role membership here.
	if _, err := m.read("/containers/"+id+"/json", &inspect, 0); err != nil {
		var upstream metricsHTTPError
		if errors.As(err, &upstream) {
			if upstream.status == 403 {
				return httperror.Forbidden("Permission denied", nil)
			}
			if upstream.status == 404 {
				return httperror.NotFound("Container not found", nil)
			}
		}
		return &httperror.HandlerError{StatusCode: http.StatusBadGateway, Message: "Container metrics source unavailable", Err: err}
	}
	var info resourcemetrics.Info
	_, infoErr := m.read("/info", &info, 30*time.Second)
	var stats *resourcemetrics.Stats
	if inspect.State.Running {
		var sample resourcemetrics.Stats
		if _, err := m.read("/containers/"+inspect.ID+"/stats?stream=false", &sample, 8*time.Second); err == nil {
			stats = &sample
		}
	}
	var disk resourcemetrics.DiskUsage
	diskAt, diskErr := m.read("/system/df?type=container&type=volume", &disk, 60*time.Second)
	u := resourcemetrics.Container(inspect, info, stats, nil)
	if diskErr == nil {
		u = resourcemetrics.Container(inspect, info, stats, &disk)
		u.StorageSampledAt = &diskAt
	} else {
		u.Notes = append(u.Notes, "Storage data unavailable")
	}
	if infoErr != nil {
		u.Notes = append(u.Notes, "Host capacity unavailable")
	}
	if inspect.State.Running && stats == nil {
		u.Notes = append(u.Notes, "Live container statistics unavailable")
	}
	if root := localMetricsRoot(ep); root != "" {
		for i, mount := range inspect.Mounts {
			u.Mounts[i].Filesystem = resourcemetrics.HostFilesystem(root, mount.Source)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	return response.JSON(w, u)
}

func (h *Handler) endpointMetrics(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	ep, m, herr := h.metricsReader(r)
	if herr != nil {
		return herr
	}
	var info resourcemetrics.Info
	if _, err := m.read("/info", &info, 30*time.Second); err != nil {
		return &httperror.HandlerError{StatusCode: http.StatusBadGateway, Message: "Environment metrics source unavailable", Err: err}
	}
	u := resourcemetrics.Usage{SampledAt: time.Now().UTC(), CPU: resourcemetrics.Unknown("cores", "host capacity; usage unavailable"), Memory: resourcemetrics.Unknown("bytes", "host capacity; usage unavailable"), State: "host", Source: "Docker Engine capacity", Mounts: []resourcemetrics.Mount{}, Notes: []string{}}
	if info.NCPU > 0 {
		u.CPU.Total = resourcemetrics.Number(float64(info.NCPU))
	}
	if info.MemTotal > 0 {
		u.Memory.Total = resourcemetrics.Number(float64(info.MemTotal))
	}
	if root := localMetricsRoot(ep); root != "" && info.OSType == "linux" {
		sample, err := m.cache.Get(ctx, m.key+"host:"+root, 8*time.Second, func() ([]byte, error) { return json.Marshal(resourcemetrics.Host(ctx, root, info)) })
		if err == nil {
			_ = json.Unmarshal(sample.Data, &u)
		}
	} else {
		u.Notes = append(u.Notes, "Host CPU/RAM usage and individual mounts require the local read-only Linux host metrics mount")
		if ep.Type == portainer.AgentOnDockerEnvironment {
			var storage struct {
				RootDir   string `json:"rootDir"`
				Total     uint64 `json:"totalBytes"`
				Available uint64 `json:"availableBytes"`
			}
			if at, err := m.read("/host/docker-storage", &storage, 60*time.Second); err == nil && storage.Total > 0 && storage.Total >= storage.Available {
				u.StorageSampledAt = &at
				u.Mounts = append(u.Mounts, resourcemetrics.Mount{Target: storage.RootDir, Type: "Docker data filesystem", Data: resourcemetrics.Metric{Used: resourcemetrics.Number(float64(storage.Total - storage.Available)), Total: resourcemetrics.Number(float64(storage.Total)), Unit: "bytes", Basis: "shared filesystem; used includes reserved blocks (Agent source)"}})
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	return response.JSON(w, u)
}
