package docker

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/http/security"
	"github.com/stretchr/testify/require"
)

func TestVolumeTextSaveTruncatesAndDetectsConflict(t *testing.T) {
	content := []byte("old configuration with a long tail\r\n")
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "node-a", r.Header.Get(portainer.PortainerAgentTargetHeader))
		switch r.URL.Path {
		case "/v2/browse/get":
			require.Equal(t, "data", r.URL.Query().Get("volumeID"))
			require.Equal(t, "/nested/app.conf", r.URL.Query().Get("path"))
			_, _ = w.Write(content)
		case "/v1/browse/data/put":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseMultipartForm(2<<20))
			defer r.MultipartForm.RemoveAll()
			require.Equal(t, "/nested", r.FormValue("Path"))
			f, h, err := r.FormFile("file")
			require.NoError(t, err)
			defer f.Close()
			require.Equal(t, "app.conf", h.Filename)
			content, err = io.ReadAll(f)
			require.NoError(t, err)
			writes++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	transport := &Transport{HTTPTransport: server.Client().Transport.(*http.Transport), endpoint: &portainer.Endpoint{ID: 1, Type: portainer.AgentOnDockerEnvironment}, dockerID: "daemon"}
	call := func(method string, payload any) *http.Response {
		var body io.Reader
		if payload != nil {
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			body = bytes.NewReader(data)
		}
		r := httptest.NewRequest(method, server.URL+"/v2/browse/text?volumeID=data&path=/nested/app.conf", body)
		r.Header.Set(portainer.PortainerAgentTargetHeader, "node-a")
		r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{Role: portainer.AdministratorRole}))
		response, err := transport.proxyVolumeText(r)
		require.NoError(t, err)
		return response
	}
	r := call("GET", nil)
	require.Equal(t, 200, r.StatusCode)
	var file volumeTextFile
	require.NoError(t, json.NewDecoder(r.Body).Decode(&file))
	r.Body.Close()
	r = call("PUT", volumeTextFile{Content: "short\r\n", Version: file.Version})
	require.Equal(t, 200, r.StatusCode)
	r.Body.Close()
	require.Equal(t, "short\r\n", string(content))
	require.Equal(t, 1, writes)
	r = call("PUT", volumeTextFile{Content: "stale", Version: file.Version})
	require.Equal(t, 409, r.StatusCode)
	r.Body.Close()
	require.Equal(t, 1, writes)
	r = call("PUT", volumeTextFile{Content: "", Version: textVersion(content)})
	require.Equal(t, 200, r.StatusCode)
	r.Body.Close()
	require.Empty(t, content)
	require.Equal(t, 2, writes)
	r = call("PUT", map[string]string{"Version": textVersion(content)})
	require.Equal(t, 400, r.StatusCode)
	r.Body.Close()
	require.Equal(t, 2, writes)
}

func TestVolumeTextRefusesUnsafeOrOversizedInput(t *testing.T) {
	for _, p := range []string{"/../secret", "/a/../b", "//a", "a", "/", "/a\\b", "/a\x00"} {
		require.False(t, validVolumeTextPath("data", p), p)
	}
	require.False(t, validVolumeTextPath("../host", "/app.conf"))
	require.True(t, validVolumeTextPath("app_data-1", "/folder/app.json"))
	require.False(t, validVolumeText([]byte{0xff}))
	require.False(t, validVolumeText([]byte{'x', 0}))
	require.True(t, validVolumeText([]byte("\xef\xbb\xbfhello\r\n")))
	for _, tc := range []struct {
		content []byte
		status  int
	}{{bytes.Repeat([]byte("a"), maxVolumeTextBytes+1), 413}, {[]byte{0xff, 0}, 415}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(tc.content) }))
		transport := &Transport{HTTPTransport: srv.Client().Transport.(*http.Transport), endpoint: &portainer.Endpoint{Type: portainer.AgentOnDockerEnvironment}, dockerID: "daemon"}
		r := httptest.NewRequest("GET", srv.URL+"/v2/browse/text?volumeID=data&path=/app.conf", nil)
		r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{Role: portainer.AdministratorRole}))
		resp, err := transport.proxyVolumeText(r)
		require.NoError(t, err)
		require.Equal(t, tc.status, resp.StatusCode)
		resp.Body.Close()
		srv.Close()
	}
}

func TestVolumeTextReadFailuresAndInvalidJSONNeverWrite(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.WriteHeader(403)
	}))
	defer srv.Close()
	tr := &Transport{HTTPTransport: srv.Client().Transport.(*http.Transport), endpoint: &portainer.Endpoint{Type: portainer.AgentOnDockerEnvironment}, dockerID: "daemon"}
	for _, tc := range []struct {
		file, content string
		status        int
	}{{"app.conf", "ok", 403}, {"app.json", "{bad", 400}} {
		data, _ := json.Marshal(volumeTextFile{Content: tc.content, Version: strings.Repeat("a", 64)})
		r := httptest.NewRequest("PUT", srv.URL+"/v2/browse/text?volumeID=data&path="+url.QueryEscape("/"+tc.file), bytes.NewReader(data))
		r = r.WithContext(security.StoreTokenData(r, &portainer.TokenData{Role: portainer.AdministratorRole}))
		resp, err := tr.proxyVolumeText(r)
		require.NoError(t, err)
		require.Equal(t, tc.status, resp.StatusCode)
		resp.Body.Close()
	}
	require.Zero(t, writes)
}
