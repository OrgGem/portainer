package compose

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"

	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli/cli/command"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/rs/zerolog/log"
)

// BuildKit 0.25.1's OCI bridge uses a fixed name and subnet. Check the host
// before starting it so it cannot adopt or remove another builder's bridge.
func checkHostBuildNetwork(ctx context.Context, cli *command.DockerCli) error {
	if _, err := cli.Client().ImageInspect(ctx, managedBuildkitImage); err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		auth, err := imagetools.RegistryAuthForRef(managedBuildkitImage, cli.ConfigFile())
		if err != nil {
			return err
		}
		stream, err := cli.Client().ImageCreate(ctx, managedBuildkitImage, image.CreateOptions{RegistryAuth: auth})
		if err != nil {
			return err
		}
		copyErr := jsonmessage.DisplayJSONMessagesStream(stream, io.Discard, 0, false, nil)
		_ = stream.Close()
		if copyErr != nil {
			return copyErr
		}
		if _, err := cli.Client().ImageInspect(ctx, managedBuildkitImage); err != nil {
			return fmt.Errorf("unable to pull managed BuildKit image: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	const separator = "PORTAINER_NETWORK_INTERFACES"
	config := &container.Config{
		Image:        managedBuildkitImage,
		Env:          []string{managedBuilderMarker},
		Entrypoint:   []string{"/bin/sh", "-c"},
		Cmd:          []string{"set -e; ip -4 route show table all; echo " + separator + "; cat /proc/net/dev"},
		AttachStdout: true,
		AttachStderr: true,
	}
	host := &container.HostConfig{NetworkMode: container.NetworkMode("host")}
	created, err := cli.Client().ContainerCreate(ctx, config, host, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cli.Client().ContainerRemove(cleanupCtx, created.ID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !errdefs.IsNotFound(err) {
			log.Warn().Err(err).Str("container", created.ID).Msg("Unable to remove host network inspection container")
		}
	}()
	attached, err := cli.Client().ContainerAttach(ctx, created.ID, container.AttachOptions{Stream: true, Stdout: true, Stderr: true})
	if err != nil {
		return err
	}
	defer attached.Close()
	var stdout, stderr bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader)
		readDone <- err
	}()
	if err := cli.Client().ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return err
	}
	statusCh, errCh := cli.Client().ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	var status container.WaitResponse
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case status = <-statusCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-readDone:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if status.Error != nil {
		return fmt.Errorf("host network inspection failed: %s", status.Error.Message)
	}
	if status.StatusCode != 0 {
		return fmt.Errorf("host network inspection failed with exit code %d: %s", status.StatusCode, strings.TrimSpace(stderr.String()))
	}
	parts := strings.SplitN(stdout.String(), separator, 2)
	if len(parts) != 2 {
		return fmt.Errorf("host network inspection returned incomplete output")
	}
	return validateHostBuildNetwork(parts[0], parts[1])
}

func validateHostBuildNetwork(routes, interfaces string) error {
	for _, line := range strings.Split(interfaces, "\n") {
		name, _, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) == "buildkit0" {
			return fmt.Errorf("host network interface buildkit0 already exists")
		}
	}
	bridge := netip.MustParsePrefix("10.10.0.0/16")
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}
		if fields[0] == "local" || fields[0] == "broadcast" || fields[0] == "unreachable" || fields[0] == "blackhole" || fields[0] == "prohibit" || fields[0] == "throw" || fields[0] == "anycast" || fields[0] == "multicast" || fields[0] == "nat" {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[0])
		if err != nil {
			addr, addrErr := netip.ParseAddr(fields[0])
			if addrErr != nil || !addr.Is4() {
				continue
			}
			prefix = netip.PrefixFrom(addr, 32)
		}
		if prefix.Addr().Is4() && prefix.Bits() != 0 && bridge.Overlaps(prefix) {
			return fmt.Errorf("host route %s overlaps the BuildKit bridge subnet %s", prefix, bridge)
		}
	}
	return nil
}
