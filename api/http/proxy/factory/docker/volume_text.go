package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	portainer "github.com/portainer/portainer/api"
)

const maxVolumeTextBytes = 1 << 20

var volumeTextName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var volumeTextLocks [64]sync.Mutex

type volumeTextFile struct {
	Content string `json:"Content"`
	Version string `json:"Version"`
}

func textResponse(status int, value any) (*http.Response, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}, "Cache-Control": []string{"no-store"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
}
func textError(status int, message string) (*http.Response, error) {
	return textResponse(status, map[string]string{"message": message})
}

func validVolumeTextPath(volume, filePath string) bool {
	if !volumeTextName.MatchString(volume) || len(volume) > 255 || len(filePath) > 4096 || !strings.HasPrefix(filePath, "/") || filePath == "/" || path.Clean(filePath) != filePath || strings.Contains(filePath, "\\") {
		return false
	}
	for _, c := range filePath {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

func validVolumeText(data []byte) bool {
	if len(data) > maxVolumeTextBytes || !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' || b == 127 {
			return false
		}
	}
	return true
}
func textVersion(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func agentTextRequest(r *http.Request, method, requestPath string, query url.Values, body io.Reader) *http.Request {
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Path = requestPath
	u.RawPath = ""
	u.RawQuery = query.Encode()
	clone.URL = &u
	clone.Method = method
	clone.RequestURI = ""
	clone.ContentLength = 0
	clone.Body = nil
	// Preserve the agent signature/target, but never forward client cache,
	// compression or conditional headers into the read-before-write check.
	clone.Header = r.Header.Clone()
	for _, key := range []string{"Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "Accept-Encoding", "Content-Type", "Content-Length"} {
		clone.Header.Del(key)
	}
	clone.Header.Set("Accept-Encoding", "identity")
	if body != nil {
		clone.Body = io.NopCloser(body)
	}
	return clone
}

func (t *Transport) readVolumeText(r *http.Request, volume, filePath, resourceID string) ([]byte, *http.Response, error) {
	read := agentTextRequest(r, http.MethodGet, "/v2/browse/get", url.Values{"volumeID": {volume}, "path": {filePath}}, nil)
	read.Header.Set("Range", fmt.Sprintf("bytes=0-%d", maxVolumeTextBytes))
	result, err := t.restrictedResourceOperation(read, resourceID, volume, portainer.VolumeResourceControl, true)
	if err != nil {
		return nil, nil, err
	}
	if result.StatusCode != http.StatusOK && result.StatusCode != http.StatusPartialContent {
		// ServeContent returns 416 for an empty file with a Range header.
		if result.StatusCode == http.StatusRequestedRangeNotSatisfiable && result.Header.Get("Content-Range") == "bytes */0" {
			result.Body.Close()
			return []byte{}, nil, nil
		}
		return nil, result, nil
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, maxVolumeTextBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxVolumeTextBytes {
		failure, err := textError(413, "Quick edit supports files up to 1 MiB")
		return nil, failure, err
	}
	if !validVolumeText(data) {
		failure, err := textError(415, "Quick edit supports UTF-8 text files only")
		return nil, failure, err
	}
	return data, nil, nil
}

func (t *Transport) proxyVolumeText(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		return textError(405, "Use GET or PUT for text files")
	}
	if t.endpoint.Type != portainer.AgentOnDockerEnvironment {
		return textError(400, "Quick edit requires a Docker Agent environment")
	}
	q := r.URL.Query()
	volume, filePath := q.Get("volumeID"), q.Get("path")
	if len(q["volumeID"]) != 1 || len(q["path"]) != 1 || !validVolumeTextPath(volume, filePath) {
		return textError(400, "Invalid volume or file path")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	resourceID, err := t.getVolumeResourceID(volume)
	if err != nil {
		return nil, err
	}
	var update volumeTextFile
	if r.Method == http.MethodPut {
		// JSON escaping can expand UTF-8/control bytes by up to six times.
		data, err := io.ReadAll(io.LimitReader(r.Body, 6*maxVolumeTextBytes+1025))
		if err != nil {
			return nil, err
		}
		if len(data) > 6*maxVolumeTextBytes+1024 {
			return textError(413, "Quick edit supports files up to 1 MiB")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var payload struct {
			Content *string
			Version string
		}
		if !utf8.Valid(data) || decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF || payload.Content == nil || len(payload.Version) != 64 {
			return textError(400, "Content and file Version are required")
		}
		update = volumeTextFile{Content: *payload.Content, Version: payload.Version}
		if len([]byte(update.Content)) > maxVolumeTextBytes {
			return textError(413, "Quick edit supports files up to 1 MiB")
		}
		if !validVolumeText([]byte(update.Content)) {
			return textError(415, "Quick edit supports UTF-8 text files only")
		}
		if strings.EqualFold(path.Ext(filePath), ".json") && !json.Valid(bytes.TrimPrefix([]byte(update.Content), []byte{0xef, 0xbb, 0xbf})) {
			return textError(400, "Invalid JSON; correct the document before saving")
		}
		// Serialize editor saves in this Portainer process. Application writes and
		// legacy uploads remain outside this lock; this is not filesystem CAS.
		hash := fnv.New32a()
		fmt.Fprintf(hash, "%d:%s:%s:%s", t.endpoint.ID, r.Header.Get(portainer.PortainerAgentTargetHeader), volume, filePath)
		lock := &volumeTextLocks[hash.Sum32()%uint32(len(volumeTextLocks))]
		lock.Lock()
		defer lock.Unlock()
		if ctx.Err() != nil {
			return textError(408, "Timed out waiting to save; your draft has been kept")
		}
	}
	current, denied, err := t.readVolumeText(r, volume, filePath, resourceID)
	if err != nil || denied != nil {
		return denied, err
	}
	if r.Method == http.MethodGet {
		return textResponse(200, volumeTextFile{Content: string(current), Version: textVersion(current)})
	}
	if textVersion(current) != update.Version {
		return textError(409, "File changed since it was opened. Reload the current file before saving; your draft has been kept.")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err = form.WriteField("Path", path.Dir(filePath)); err != nil {
		return nil, err
	}
	part, err := form.CreateFormFile("file", path.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err = io.WriteString(part, update.Content); err != nil {
		return nil, err
	}
	if err = form.Close(); err != nil {
		return nil, err
	}
	// Agent 2.45.3 v2 upload uses O_WRONLY without O_TRUNC. Its supported v1
	// writer uses os.WriteFile and correctly truncates shorter/empty content,
	// retaining the existing inode's permissions. Authorization was performed
	// above via the v2 volume path; this internal compatibility request is never
	// exposed as a new general-purpose v1 proxy grant.
	write := agentTextRequest(r, http.MethodPost, "/v1/browse/"+volume+"/put", nil, &body)
	write.Header.Set("Content-Type", form.FormDataContentType())
	write.ContentLength = int64(body.Len())
	result, err := t.executeDockerRequest(write)
	if err != nil {
		return textError(502, "Save result is uncertain. Reload the file before retrying.")
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		result.Body.Close()
		return textError(502, "Unable to save file. Your draft has been kept; reload before retrying.")
	}
	result.Body.Close()
	verified, denied, err := t.readVolumeText(r, volume, filePath, resourceID)
	if err != nil || denied != nil || !bytes.Equal(verified, []byte(update.Content)) {
		if denied != nil {
			denied.Body.Close()
		}
		return textError(409, "Save could not be verified. The file may have changed; reload before retrying. Your draft has been kept.")
	}
	return textResponse(200, volumeTextFile{Content: string(verified), Version: textVersion(verified)})
}
