package settings

import (
	"net/http/httptest"
	"testing"

	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
)

func TestComposeBuildPolicyDefaultsAndPayloadValidation(t *testing.T) {
	s := &portainer.Settings{}
	hideFields(s)
	require.Equal(t, portainer.DefaultComposeBuildPolicy(), *s.ComposeBuildPolicy)
	for _, p := range []portainer.ComposeBuildPolicy{
		{CPUPercent: 0, MemoryPercent: 50, MaxConcurrentBuilds: 1},
		{CPUPercent: 51, MemoryPercent: 50, MaxConcurrentBuilds: 1},
		{CPUPercent: 50, MemoryPercent: 0, MaxConcurrentBuilds: 1},
		{CPUPercent: 50, MemoryPercent: 51, MaxConcurrentBuilds: 1},
		{CPUPercent: 50, MemoryPercent: 50, MaxConcurrentBuilds: 0},
		{CPUPercent: 50, MemoryPercent: 50, MaxConcurrentBuilds: 9},
	} {
		payload := settingsUpdatePayload{ComposeBuildPolicy: &p}
		require.Error(t, payload.Validate(httptest.NewRequest("PUT", "/settings", nil)))
	}
	payload := settingsUpdatePayload{ComposeBuildPolicy: s.ComposeBuildPolicy}
	require.NoError(t, payload.Validate(httptest.NewRequest("PUT", "/settings", nil)))
	payload.ComposeBuildPolicy = nil // unrelated settings updates remain compatible
	require.NoError(t, payload.Validate(httptest.NewRequest("PUT", "/settings", nil)))
}
