package portainer

import (
	"encoding/json"
	"testing"
)

func TestComposeBuildPolicyModesDefaultOffAndRemainBackwardCompatible(t *testing.T) {
	t.Parallel()

	defaults := DefaultComposeBuildPolicy()
	if defaults.AllowHostNetwork || defaults.UseLegacyBuild {
		t.Fatalf("build mode features must be opt-in by default, got %+v", defaults)
	}

	var settings Settings
	if err := json.Unmarshal([]byte(`{"ComposeBuildPolicy":{"CPUPercent":50,"MemoryPercent":50,"MaxConcurrentBuilds":1,"AutoConfigureBuilderCgroup":false}}`), &settings); err != nil {
		t.Fatalf("decode pre-existing settings: %v", err)
	}

	got := settings.EffectiveComposeBuildPolicy()
	if got.AllowHostNetwork || got.UseLegacyBuild {
		t.Fatalf("settings without the new fields must keep both features disabled, got %+v", got)
	}
}
