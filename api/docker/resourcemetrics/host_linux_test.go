//go:build linux

package resourcemetrics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostPathsConfinedAndMountsFiltered(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "data"), 0700))
	require.NoError(t, os.Symlink("/etc", filepath.Join(root, "escape")))
	require.Nil(t, HostFilesystem(root, "/escape/passwd"))
	require.Nil(t, HostFilesystem(root, "/../../etc/passwd"))
	require.NotNil(t, HostFilesystem(root, "/data"))
	mounts := hostMounts(root, "1 0 8:1 / / rw - ext4 /dev/sda rw\n2 1 8:1 / /data ro - ext4 /dev/sda ro\n3 1 0:1 / /proc rw - proc proc rw\n4 1 8:1 / /data rw - ext4 /dev/sda rw\n")
	require.Len(t, mounts, 2)
	require.True(t, mounts[1].ReadOnly)
	require.NotNil(t, mounts[0].Data.Total)
}

func TestDockerFilesystemShownBeforeBootPartition(t *testing.T) {
	mounts := []Mount{{Target: "/boot"}, {Target: "/"}, {Target: "/var/lib"}}
	prioritizeMounts(mounts, "/var/lib/docker")
	require.Equal(t, "/var/lib", mounts[0].Target)
	require.Equal(t, "/", mounts[1].Target)
}

func TestCPUTicksExcludeGuestAndCountIOWaitIdle(t *testing.T) {
	total, idle, err := cpuTicks([]byte("cpu  10 20 30 40 50 60 70 80 90 100\n"))
	require.NoError(t, err)
	require.Equal(t, uint64(360), total)
	require.Equal(t, uint64(90), idle)
}
