// Package stackconfig manages immutable application configuration revisions.
package stackconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	portainer "github.com/portainer/portainer/api"
	"go.yaml.in/yaml/v3"
)

const MaxFileSize = 1024 * 1024
const MaxRevisions = 100

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func Root() (string, error) {
	root := os.Getenv("PORTAINER_STACK_CONFIG_ROOT")
	if root == "" || !filepath.IsAbs(root) || strings.ContainsAny(root, "$\r\n") {
		return "", errors.New("PORTAINER_STACK_CONFIG_ROOT must be an absolute directory mounted at the same path on the Docker host")
	}
	return filepath.Clean(root), nil
}

func ValidateEndpoint(stack *portainer.Stack, endpoint *portainer.Endpoint) error {
	if stack.Type != portainer.DockerComposeStack || stack.WorkflowID != 0 || stack.GitConfig != nil || len(stack.AdditionalFiles) != 0 {
		return errors.New("managed configuration requires a file-based, single-file Docker Compose stack")
	}
	if endpoint.Type != portainer.DockerEnvironment || !strings.HasPrefix(endpoint.URL, "unix://") {
		return errors.New("managed configuration requires the local Linux Docker socket and a shared configuration directory")
	}
	return nil
}

func Services(stack *portainer.Stack) ([]string, error) {
	content, err := os.ReadFile(filepath.Join(stack.ProjectPath, stack.EntryPoint))
	if err != nil {
		return nil, err
	}
	return ServicesFromContent(content)
}

// ServicesFromContent returns declared services before a stack is created.
func ServicesFromContent(content []byte) ([]string, error) {
	var compose struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &compose); err != nil {
		return nil, err
	}
	services := make([]string, 0, len(compose.Services))
	for name := range compose.Services {
		services = append(services, name)
	}
	return services, nil
}

func ValidateTargets(targets []portainer.StackConfigTarget, services []string) error {
	if len(targets) > 32 {
		return errors.New("at most 32 configuration targets are allowed")
	}
	known := map[string]bool{}
	for _, service := range services {
		known[service] = true
	}
	ids, destinations := map[string]bool{}, map[string]bool{}
	for _, t := range targets {
		if !identifier.MatchString(t.ID) || !known[t.Service] {
			return errors.New("invalid target identifier or unknown service")
		}
		if !path.IsAbs(t.Target) || path.Clean(t.Target) != t.Target || t.Target == "/" || strings.ContainsAny(t.Target, "$\r\n\\\x00") {
			return errors.New("target must be a clean absolute Linux file path")
		}
		for _, reserved := range []string{"/proc", "/sys", "/dev"} {
			if t.Target == reserved || strings.HasPrefix(t.Target, reserved+"/") {
				return errors.New("target uses a reserved filesystem")
			}
		}
		key := t.Service + ":" + t.Target
		if ids[t.ID] || destinations[key] {
			return errors.New("duplicate target identifier or service path")
		}
		ids[t.ID], destinations[key] = true, true
	}
	return nil
}

func ValidateContent(filename string, content []byte) error {
	if len(content) == 0 || len(content) > MaxFileSize || !utf8.Valid(content) || strings.ContainsRune(string(content), '\x00') {
		return errors.New("configuration must be non-empty UTF-8 text, at most 1 MiB, without NUL bytes")
	}
	if filename != path.Base(filename) || strings.ContainsAny(filename, "\\\r\n") || len(filename) > 128 {
		return errors.New("invalid filename")
	}
	switch strings.ToLower(path.Ext(filename)) {
	case ".json":
		if !json.Valid(content) {
			return errors.New("invalid JSON configuration")
		}
	case ".yaml", ".yml":
		decoder := yaml.NewDecoder(strings.NewReader(string(content)))
		for {
			var value any
			err := decoder.Decode(&value)
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("invalid YAML configuration: %w", err)
			}
		}
	case ".conf":
	default:
		return errors.New("only .yaml, .yml, .json and .conf files are supported")
	}
	return nil
}

func revisionPath(stackID portainer.StackID, revision string) (string, error) {
	if stackID <= 0 {
		return "", errors.New("invalid stack ID")
	}
	if _, err := uuid.Parse(revision); err != nil || len(revision) != 36 {
		return "", errors.New("invalid revision ID")
	}
	return filepath.Join(fmt.Sprint(stackID), revision), nil
}

func WriteRevision(stackID portainer.StackID, revision string, content []byte) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	name, err := revisionPath(stackID, revision)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err := dir.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return "", err
	}
	f, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(content)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = dir.Remove(name)
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func ReadRevision(stackID portainer.StackID, revision portainer.StackConfigRevision) ([]byte, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	name, err := revisionPath(stackID, revision.ID)
	if err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	if len(content) != revision.Size || hex.EncodeToString(sum[:]) != revision.SHA256 {
		return nil, errors.New("configuration revision integrity check failed")
	}
	return content, nil
}

// WriteOverride adds only declared read-only mounts; user file contents never
// participate in Compose interpolation. The caller removes the temporary file.
func WriteOverride(stack *portainer.Stack) (string, error) {
	if stack.Configuration == nil {
		return "", nil
	}
	declaredServices, err := Services(stack)
	if err != nil {
		return "", fmt.Errorf("unable to read Compose services for configuration mounts: %w", err)
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	services := map[string]any{}
	for _, target := range stack.Configuration.Targets {
		// A Compose update may rename or remove a service. Do not resurrect it
		// as an image-less service through the override. Keep metadata unchanged:
		// a failed update restores the old Compose file and its active mounts;
		// after success, an administrator can retire the missing-service target.
		if target.ActiveRevision == "" || !slices.Contains(declaredServices, target.Service) {
			continue
		}
		var revision *portainer.StackConfigRevision
		for i := range stack.Configuration.Revisions {
			r := &stack.Configuration.Revisions[i]
			if r.ID == target.ActiveRevision && r.TargetID == target.ID {
				revision = r
				break
			}
		}
		if revision == nil {
			return "", errors.New("active configuration revision is missing")
		}
		if _, err := ReadRevision(stack.ID, *revision); err != nil {
			return "", err
		}
		name, err := revisionPath(stack.ID, revision.ID)
		if err != nil {
			return "", err
		}
		service, ok := services[target.Service].(map[string]any)
		if !ok {
			service = map[string]any{"volumes": []any{}}
		}
		service["volumes"] = append(service["volumes"].([]any), map[string]any{
			"type": "bind", "source": filepath.ToSlash(filepath.Join(root, name)), "target": target.Target, "read_only": true,
			"bind": map[string]any{"create_host_path": false},
		})
		services[target.Service] = service
	}
	if len(services) == 0 {
		return "", nil
	}
	data, err := yaml.Marshal(map[string]any{"services": services})
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(root, "compose-config-*.yaml")
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
