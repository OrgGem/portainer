package stackconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestActiveOverridesFollowComposeServicesWithoutChangingHistory(t *testing.T) {
	for _, change := range []struct{ name, compose string }{
		{"rename", "services:\n  api:\n    image: nginx\n  side:\n    image: nginx\n"},
		{"delete", "services:\n  side:\n    image: nginx\n"},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Setenv("PORTAINER_STACK_CONFIG_ROOT", t.TempDir())
			stack := &portainer.Stack{ID: 1, ProjectPath: t.TempDir(), EntryPoint: "compose.yaml", Configuration: &portainer.StackConfiguration{Version: 7}}
			for _, service := range []string{"web", "side"} {
				id := uuid.NewString()
				sum, err := WriteRevision(stack.ID, id, []byte("active config"))
				require.NoError(t, err)
				stack.Configuration.Targets = append(stack.Configuration.Targets, portainer.StackConfigTarget{ID: service, Service: service, Target: "/etc/app.conf", ActiveRevision: id})
				stack.Configuration.Revisions = append(stack.Configuration.Revisions, portainer.StackConfigRevision{ID: id, TargetID: service, Size: len("active config"), SHA256: sum, AppliedAt: 123})
			}
			before, err := json.Marshal(stack.Configuration)
			require.NoError(t, err)
			file := filepath.Join(stack.ProjectPath, stack.EntryPoint)
			require.NoError(t, os.WriteFile(file, []byte(change.compose), 0600))
			readServices := func() map[string]any {
				t.Helper()
				override, err := WriteOverride(stack)
				require.NoError(t, err)
				defer os.Remove(override)
				content, err := os.ReadFile(override)
				require.NoError(t, err)
				var parsed struct {
					Services map[string]any `yaml:"services"`
				}
				require.NoError(t, yaml.Unmarshal(content, &parsed))
				return parsed.Services
			}
			services := readServices()
			require.Len(t, services, 1)
			require.Contains(t, services, "side")
			require.NotContains(t, services, "web")
			require.NotContains(t, services, "api", "configuration is never silently transferred to a renamed service")
			// Ordinary deployment rollback restores the old Compose. The original
			// active pointers must still produce both original read-only mounts.
			require.NoError(t, os.WriteFile(file, []byte("services:\n  web:\n    image: nginx\n  side:\n    image: nginx\n"), 0600))
			require.Len(t, readServices(), 2)
			after, err := json.Marshal(stack.Configuration)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
			// No declared service with an active target means no override at all.
			require.NoError(t, os.WriteFile(file, []byte("services:\n  api:\n    image: nginx\n"), 0600))
			override, err := WriteOverride(stack)
			require.NoError(t, err)
			require.Empty(t, override)
			require.NoError(t, os.Remove(file))
			_, err = WriteOverride(stack)
			require.Error(t, err, "unreadable Compose must not silently discard mounts")
		})
	}
}

func TestConfigContentValidation(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		valid         bool
	}{
		{"app.yaml", "key: value", true}, {"app.json", `{"a":1}`, true}, {"app.conf", "listen=8080", true}, {"app.yml", "a: [", false},
		{"../app.conf", "a", false}, {"C:\\app.conf", "a", false}, {"app.exe", "a", false}, {"app.json", "{", false}, {"app.conf", "a\x00b", false}, {"app.conf", "", false},
	} {
		t.Run(tc.name+tc.content, func(t *testing.T) {
			err := ValidateContent(tc.name, []byte(tc.content))
			require.Equal(t, tc.valid, err == nil)
		})
	}
	require.Error(t, ValidateContent("a.conf", []byte(strings.Repeat("x", MaxFileSize+1))))
}

func TestConfigTargetsCannotEscapeOrOverlap(t *testing.T) {
	for _, target := range []string{"../a", "/etc/../a", "/", "/dev/sda", "/proc/1/mem", "/sys/a", "/etc/$HOME", "/etc/a\n"} {
		require.Error(t, ValidateTargets([]portainer.StackConfigTarget{{ID: "app", Service: "web", Target: target}}, []string{"web"}))
	}
	require.Error(t, ValidateTargets([]portainer.StackConfigTarget{{ID: "app", Service: "other", Target: "/etc/a.conf"}}, []string{"web"}))
	target := portainer.StackConfigTarget{ID: "app", Service: "web", Target: "/etc/a.conf"}
	require.NoError(t, ValidateTargets([]portainer.StackConfigTarget{target}, []string{"web"}))
	require.Error(t, ValidateTargets([]portainer.StackConfigTarget{target, target}, []string{"web"}))
}

func TestConfigRevisionImmutabilityIntegrityAndOverlay(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PORTAINER_STACK_CONFIG_ROOT", root)
	id := uuid.NewString()
	content := []byte("password: ${NOT_INTERPOLATED}")
	sum, err := WriteRevision(1, id, content)
	require.NoError(t, err)
	_, err = WriteRevision(1, id, []byte("overwrite"))
	require.Error(t, err)
	_, err = WriteRevision(1, "../../escape", content)
	require.Error(t, err)
	revision := portainer.StackConfigRevision{ID: id, TargetID: "app", SHA256: sum, Size: len(content)}
	data, err := ReadRevision(1, revision)
	require.NoError(t, err)
	require.Equal(t, content, data)
	stack := &portainer.Stack{ID: 1, Configuration: &portainer.StackConfiguration{Targets: []portainer.StackConfigTarget{{ID: "app", Service: "web", Target: "/etc/app.conf", ActiveRevision: id}}, Revisions: []portainer.StackConfigRevision{revision}}}
	stack.ProjectPath, stack.EntryPoint = t.TempDir(), "compose.yaml"
	require.NoError(t, os.WriteFile(filepath.Join(stack.ProjectPath, stack.EntryPoint), []byte("services:\n  web:\n    image: nginx\n"), 0600))
	file, err := WriteOverride(stack)
	require.NoError(t, err)
	defer os.Remove(file)
	override, err := os.ReadFile(file)
	require.NoError(t, err)
	var parsed struct {
		Services map[string]struct {
			Volumes []struct {
				Source   string `yaml:"source"`
				Target   string `yaml:"target"`
				ReadOnly bool   `yaml:"read_only"`
				Bind     struct {
					CreateHostPath bool `yaml:"create_host_path"`
				} `yaml:"bind"`
			} `yaml:"volumes"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(override, &parsed))
	require.Len(t, parsed.Services, 1)
	mount := parsed.Services["web"].Volumes[0]
	require.True(t, mount.ReadOnly)
	require.False(t, mount.Bind.CreateHostPath)
	require.Equal(t, "/etc/app.conf", mount.Target)
	require.NotContains(t, string(override), "NOT_INTERPOLATED")
	path := filepath.Join(root, "1", id)
	require.NoError(t, os.Chmod(path, 0600))
	require.NoError(t, os.WriteFile(path, []byte("corrupted"), 0600))
	_, err = ReadRevision(1, revision)
	require.Error(t, err)
	_, err = WriteOverride(stack)
	require.Error(t, err)
}

func TestConfigRejectsUnsupportedDeployment(t *testing.T) {
	stack := &portainer.Stack{Type: portainer.DockerComposeStack}
	endpoint := &portainer.Endpoint{Type: portainer.DockerEnvironment, URL: "unix:///var/run/docker.sock"}
	require.NoError(t, ValidateEndpoint(stack, endpoint))
	endpoint.URL = "tcp://host:2376"
	require.Error(t, ValidateEndpoint(stack, endpoint))
	endpoint.URL = "unix:///var/run/docker.sock"
	stack.WorkflowID = 1
	require.Error(t, ValidateEndpoint(stack, endpoint))
}
