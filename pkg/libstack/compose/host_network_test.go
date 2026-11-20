package compose

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateHostBuildNetwork(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		routes     string
		interfaces string
		wantErr    string
	}{
		{
			name:       "normal routes do not overlap",
			routes:     "default via 192.0.2.1 dev eth0\n172.17.0.0/16 dev docker0\n10.1.0.0/16 dev cni0\nlocal 127.0.0.0/8 dev lo",
			interfaces: "lo: flags=73\neth0: flags=4163\n",
		},
		{
			name:       "exact buildkit subnet is rejected",
			routes:     "10.10.0.0/16 dev eth0",
			interfaces: "lo: flags=73\neth0: flags=4163\n",
			wantErr:    "overlaps the BuildKit bridge subnet",
		},
		{
			name:       "more specific route within buildkit subnet is rejected",
			routes:     "10.10.4.0/24 dev eth0",
			interfaces: "lo: flags=73\neth0: flags=4163\n",
			wantErr:    "overlaps the BuildKit bridge subnet",
		},
		{
			name:       "host route within buildkit subnet is rejected",
			routes:     "10.10.10.12 dev eth0",
			interfaces: "lo: flags=73\neth0: flags=4163\n",
			wantErr:    "overlaps the BuildKit bridge subnet",
		},
		{
			name:       "local route within buildkit subnet is rejected",
			routes:     "local 10.10.0.0/16 dev lo",
			interfaces: "lo: flags=73\neth0: flags=4163\n",
			wantErr:    "overlaps the BuildKit bridge subnet",
		},
		{
			name:       "existing buildkit interface is rejected",
			routes:     "172.17.0.0/16 dev docker0",
			interfaces: "lo: flags=73\nbuildkit0: flags=4163\n",
			wantErr:    "interface buildkit0 already exists",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := validateHostBuildNetwork(testCase.routes, testCase.interfaces)
			if testCase.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}
