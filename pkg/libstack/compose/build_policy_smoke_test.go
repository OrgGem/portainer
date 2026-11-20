package compose

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/stretchr/testify/require"
)

// Set PORTAINER_COMPOSE_BUILD_SMOKE_DOCKER_HOST to an isolated Linux Docker
// endpoint with moby/buildkit:v0.25.1 and a loopback HTTP fixture on port 18080.
// This exercises actual Compose builds and managed builder reconfiguration.
func TestManagedComposeBuildHostNetworkDockerSmoke(t *testing.T) {
	host := os.Getenv("PORTAINER_COMPOSE_BUILD_SMOKE_DOCKER_HOST")
	if host == "" {
		t.Skip("set PORTAINER_COMPOSE_BUILD_SMOKE_DOCKER_HOST to an isolated Docker endpoint to run the Docker smoke test")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	apiClient, err := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = apiClient.Close() })
	info, err := apiClient.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, "linux", info.OSType)
	require.Equal(t, "2", info.CgroupVersion)
	require.True(t, info.MemoryLimit && info.SwapLimit && info.CPUCfsQuota && info.CPUCfsPeriod)

	policy := portainer.DefaultComposeBuildPolicy()
	policy.AllowHostNetwork = true
	deployer := NewComposeDeployer()
	deployer.GetBuildPolicy = func() (portainer.ComposeBuildPolicy, error) {
		return policy, nil
	}

	projectName := fmt.Sprintf("portainer-host-net-smoke-%x", time.Now().UnixNano())
	workingDir := t.TempDir()
	composePath := filepath.Join(workingDir, "compose.yaml")
	writeSmokeFile(t, workingDir, "Host.Dockerfile", `FROM alpine:3.22
RUN if wget -q -T 2 -O - http://127.0.0.1:18080/ >/dev/null 2>&1; then echo "default RUN reached host loopback"; exit 1; else echo "default RUN remains isolated"; fi
RUN --network=host test "$(wget -q -O - http://127.0.0.1:18080/)" = "host-network-ok"
CMD ["sh", "-c", "sleep 600"]
`)
	writeSmokeFile(t, workingDir, "ImplicitHost.Dockerfile", `FROM alpine:3.22
RUN test "$(wget -q -O - http://127.0.0.1:18080/)" = "host-network-ok"
CMD ["sh", "-c", "sleep 600"]
`)
	writeSmokeFile(t, workingDir, "Default.Dockerfile", `FROM alpine:3.22
RUN if wget -q -T 2 -O - http://127.0.0.1:18080/ >/dev/null 2>&1; then echo "default build reached host loopback"; exit 1; else echo "host loopback remains isolated"; fi
CMD ["sh", "-c", "sleep 600"]
`)
	writeSmokeCompose(t, composePath, `services:
  host-probe:
    image: `+projectName+`-host
    build:
      context: .
      dockerfile: Host.Dockerfile
      network: default
      entitlements:
        - network.host
    network_mode: bridge
  implicit-host-probe:
    image: `+projectName+`-implicit-host
    build:
      context: .
      dockerfile: ImplicitHost.Dockerfile
      network: host
    network_mode: bridge
  default-probe:
    image: `+projectName+`-default
    build:
      context: .
      dockerfile: Default.Dockerfile
    network_mode: bridge
`)
	options := libstack.DeployOptions{Options: libstack.Options{Host: host, ProjectName: projectName}}
	firstOptions := options.Options
	remove := func() error {
		return deployer.Remove(ctx, projectName, []string{composePath}, libstack.RemoveOptions{Options: firstOptions})
	}
	firstProjectRemoved := false
	removeImages := func(names ...string) {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, name := range names {
			if _, err := apiClient.ImageRemove(cleanupCtx, name, image.RemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
				t.Logf("clean up image %s: %v", name, err)
			}
		}
	}
	cleanupProject := func(project string, options libstack.Options, imageNames ...string) {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := deployer.Remove(cleanupCtx, project, []string{composePath}, libstack.RemoveOptions{Options: options}); err != nil {
			t.Logf("clean up Compose project %s: %v", project, err)
		}
		for _, name := range imageNames {
			if _, err := apiClient.ImageRemove(cleanupCtx, name, image.RemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
				t.Logf("clean up image %s: %v", name, err)
			}
		}
	}
	t.Cleanup(func() {
		images := []string{projectName + "-host", projectName + "-implicit-host", projectName + "-default"}
		if firstProjectRemoved {
			removeImages(images...)
			return
		}
		cleanupProject(projectName, firstOptions, images...)
	})
	if err := deployer.Deploy(ctx, []string{composePath}, options); err != nil {
		t.Fatalf("managed mixed host/default stack deployment failed: %v", err)
	}

	hostBuilderName := smokeBuilderName(info.ID)
	hostBuilder, err := apiClient.ContainerInspect(ctx, hostBuilderName)
	require.NoError(t, err)
	require.Equal(t, managedBuildkitImage, hostBuilder.Config.Image)
	require.True(t, hostBuilder.HostConfig.NetworkMode.IsHost())
	require.Equal(t, []string{"--allow-insecure-entitlement=network.host", "--oci-worker-net=bridge"}, []string(hostBuilder.Config.Cmd))
	require.Greater(t, hostBuilder.HostConfig.CPUQuota, int64(0))
	require.Equal(t, int64(100000), hostBuilder.HostConfig.CPUPeriod)
	require.Greater(t, hostBuilder.HostConfig.Memory, int64(0))
	require.Equal(t, hostBuilder.HostConfig.Memory, hostBuilder.HostConfig.MemorySwap)
	cacheMounts := builderCacheMounts(hostBuilder.Mounts)
	require.NotEmpty(t, cacheMounts, "the managed builder should own a persistent state volume")
	require.NoError(t, remove())
	firstProjectRemoved = true

	repeatedProject := projectName + "-repeat"
	repeatedOptions := libstack.DeployOptions{Options: firstOptions}
	repeatedOptions.ProjectName = repeatedProject
	if err := deployer.Deploy(ctx, []string{composePath}, repeatedOptions); err != nil {
		t.Fatalf("repeated managed host-network build failed on its existing builder: %v", err)
	}
	require.NoError(t, deployer.Remove(ctx, repeatedProject, []string{composePath}, libstack.RemoveOptions{Options: repeatedOptions.Options}))
	repeatedBuilder, err := apiClient.ContainerInspect(ctx, hostBuilderName)
	require.NoError(t, err)
	require.True(t, repeatedBuilder.HostConfig.NetworkMode.IsHost())
	require.Equal(t, cacheMounts, builderCacheMounts(repeatedBuilder.Mounts), "reusing the host builder should keep its state volume")

	// The admin opt-in permits a stack to ask for host access. A default build
	// remains on the OCI bridge and moves an idle managed builder back to its
	// normal network configuration.
	writeSmokeCompose(t, composePath, `services:
  default-probe:
    image: `+projectName+`-default-after-toggle
    build:
      context: .
      dockerfile: Default.Dockerfile
    network_mode: bridge
`)
	secondProject := projectName + "-default"
	options.ProjectName = secondProject
	t.Cleanup(func() {
		cleanupProject(secondProject, options.Options, projectName+"-default-after-toggle")
	})
	if err := deployer.Deploy(ctx, []string{composePath}, options); err != nil {
		t.Fatalf("managed default-network stack deployment failed after disabling host networking: %v", err)
	}
	defaultBuilder, err := apiClient.ContainerInspect(ctx, hostBuilderName)
	require.NoError(t, err)
	require.False(t, defaultBuilder.HostConfig.NetworkMode.IsHost())
	require.Empty(t, defaultBuilder.Config.Cmd)
	require.Equal(t, managedBuildkitImage, defaultBuilder.Config.Image)
	require.Equal(t, cacheMounts, builderCacheMounts(defaultBuilder.Mounts), "builder migration should preserve its state volume")

	legacyProject := projectName + "-legacy"
	legacyOptions := libstack.DeployOptions{Options: firstOptions}
	legacyOptions.ProjectName = legacyProject
	policy.UseLegacyBuild = true
	writeSmokeFile(t, workingDir, "Legacy.Dockerfile", `FROM alpine:3.22
RUN if wget -q -T 2 -O - http://127.0.0.1:18080/ >/dev/null 2>&1; then echo "embedded default RUN reached host loopback"; exit 1; else echo "embedded default RUN remains isolated"; fi
RUN --network=host test "$(wget -q -O - http://127.0.0.1:18080/)" = "host-network-ok"
RUN printf 'legacy-compose-build-ok' > /legacy.txt
CMD ["sh", "-c", "sleep 600"]
`)
	writeSmokeCompose(t, composePath, `services:
  legacy-probe:
    image: `+legacyProject+`-built
    build:
      context: .
      dockerfile: Legacy.Dockerfile
      network: default
      entitlements:
        - network.host
    network_mode: bridge
`)
	t.Setenv("COMPOSE_BAKE", "false")
	if err := deployer.Deploy(ctx, []string{composePath}, legacyOptions); err != nil {
		t.Fatalf("legacy Compose build and deployment failed: %v", err)
	}
	t.Cleanup(func() {
		cleanupProject(legacyProject, legacyOptions.Options, legacyProject+"-built")
	})
	_, _, err = apiClient.ImageInspectWithRaw(ctx, legacyProject+"-built")
	require.NoError(t, err, "legacy Compose should load its built image into the Docker endpoint")
	legacyContainers, err := apiClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+legacyProject)),
	})
	require.NoError(t, err)
	require.Len(t, legacyContainers, 1, "legacy Compose should start the service after building it")
	managedBuilderAfterLegacy, err := apiClient.ContainerInspect(ctx, hostBuilderName)
	require.NoError(t, err)
	require.Equal(t, defaultBuilder.ID, managedBuilderAfterLegacy.ID, "legacy builds should keep using the Docker endpoint default builder")
}

func writeSmokeFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600))
}

func writeSmokeCompose(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func smokeBuilderName(daemonID string) string {
	return "buildx_buildkit_portainer-compose-" + fmt.Sprintf("%x", sha256.Sum256([]byte(daemonID)))[:20]
}

func builderCacheMounts(mounts []container.MountPoint) []string {
	var cacheMounts []string
	for _, mount := range mounts {
		if mount.Type == "volume" && strings.HasSuffix(mount.Destination, "/buildkit") {
			cacheMounts = append(cacheMounts, mount.Name)
		}
	}
	return cacheMounts
}
