package vaultrefs

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"text/template"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
)

func validMapping() portainer.VaultMapping {
	return portainer.VaultMapping{ID: "one", Service: "web", Variable: "DB_PASSWORD", Path: "kv/data/apps/demo", Key: "password", Engine: "kv-v2"}
}

func TestValidateReferences(t *testing.T) {
	require.NoError(t, Validate([]portainer.VaultMapping{validMapping()}, []string{"web"}))
	for name, change := range map[string]func(*portainer.VaultMapping){
		"unknown service":       func(m *portainer.VaultMapping) { m.Service = "other" },
		"environment injection": func(m *portainer.VaultMapping) { m.Variable = "DB_PASSWORD\nX=y" },
		"template injection":    func(m *portainer.VaultMapping) { m.Key = `x }}{{ env "HOME"` },
		"url with credentials":  func(m *portainer.VaultMapping) { m.Path = "https://user:password@vault/path" },
		"path traversal":        func(m *portainer.VaultMapping) { m.Path = "kv/data/../../sys" },
		"kv2 logical path":      func(m *portainer.VaultMapping) { m.Path = "kv/apps/demo" },
		"unknown engine":        func(m *portainer.VaultMapping) { m.Engine = "database" },
	} {
		t.Run(name, func(t *testing.T) {
			m := validMapping()
			change(&m)
			require.Error(t, Validate([]portainer.VaultMapping{m}, []string{"web"}))
		})
	}
	m, duplicate := validMapping(), validMapping()
	duplicate.ID = "two"
	require.Error(t, Validate([]portainer.VaultMapping{m, duplicate}, []string{"web"}))
	duplicate.Service = "worker"
	require.NoError(t, Validate([]portainer.VaultMapping{m, duplicate}, []string{"web", "worker"}))
}

func TestAgentExportIsServiceScopedAndMissingKeysFail(t *testing.T) {
	m, other := validMapping(), validMapping()
	other.ID, other.Service, other.Variable, other.Path = "other", "worker", "OTHER_SECRET", "kv/data/other"
	for _, engine := range []string{"kv-v1", "kv-v2"} {
		m.Engine = engine
		exported, err := AgentTemplate([]portainer.VaultMapping{m, other}, "web")
		require.NoError(t, err)
		require.NotContains(t, exported, "OTHER_SECRET")
		require.NotContains(t, exported, "kv/data/other")
		require.Contains(t, exported, "error_on_missing_key = true")
		var content string
		for _, line := range strings.Split(exported, "\n") {
			if strings.HasPrefix(line, "  contents = ") {
				content, err = strconv.Unquote(strings.TrimPrefix(line, "  contents = "))
				require.NoError(t, err)
			}
		}
		data := map[string]any{"password": "synthetic-test-value"}
		secretData := map[string]any{"Data": data}
		if engine == "kv-v2" {
			secretData["Data"] = map[string]any{"data": data}
		}
		tmpl, err := template.New("reference").Option("missingkey=error").Funcs(template.FuncMap{"secret": func(path string) any { require.Equal(t, m.Path, path); return secretData }}).Parse(content)
		require.NoError(t, err)
		var output bytes.Buffer
		require.NoError(t, tmpl.Execute(&output, nil))
		require.Equal(t, "synthetic-test-value", output.String())
		delete(data, "password")
		require.Error(t, tmpl.Execute(&bytes.Buffer{}, nil))
	}
}
