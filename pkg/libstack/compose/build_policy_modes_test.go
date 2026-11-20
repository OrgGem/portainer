package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	composeapi "github.com/docker/compose/v2/pkg/api"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/pkg/libstack"
	"github.com/stretchr/testify/require"
)

type recordingComposeBuild struct {
	composeapi.Compose
	called    bool
	options   composeapi.BuildOptions
	buildImage string
	buildTags  []string
}

func (c *recordingComposeBuild) Build(_ context.Context, project *types.Project, options composeapi.BuildOptions) error {
	c.called = true
	c.options = options
	service := project.Services["app"]
	c.buildImage = service.Image
	c.buildTags = append([]string(nil), service.Build.Tags...)
	return nil
}

type blockingComposeBuild struct {
	composeapi.Compose
	buildTags chan<- string
	release   <-chan struct{}
	tag       string
	options   composeapi.BuildOptions
}

func (c *blockingComposeBuild) Build(_ context.Context, project *types.Project, options composeapi.BuildOptions) error {
	c.options = options
	c.tag = project.Services["app"].Image
	c.buildTags <- c.tag
	<-c.release
	return nil
}

type fakeDockerImages struct {
	mu         sync.Mutex
	inspected  []string
	removed    []string
	imageIDs   map[string]string
}

func newFakeDockerImageServer(t *testing.T) (*httptest.Server, *fakeDockerImages) {
	t.Helper()
	images := &fakeDockerImages{imageIDs: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(r.URL.Path, "/version"):
			_ = json.NewEncoder(w).Encode(map[string]string{"ApiVersion": "1.47", "MinAPIVersion": "1.24"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			tag := dockerImageTagFromPath(r.URL.Path, "/json")
			imageID := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(tag)))
			images.mu.Lock()
			images.inspected = append(images.inspected, tag)
			images.imageIDs[tag] = imageID
			images.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"Id": imageID})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/images/"):
			images.mu.Lock()
			images.removed = append(images.removed, dockerImageTagFromPath(r.URL.Path, ""))
			images.mu.Unlock()
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, images
}

func dockerImageTagFromPath(path, suffix string) string {
	start := strings.Index(path, "/images/") + len("/images/")
	return strings.TrimSuffix(path[start:], suffix)
}

func withTestDockerClient(ctx context.Context, server *httptest.Server, callback func(context.Context, *command.DockerCli) error) error {
	host := "tcp://" + strings.TrimPrefix(server.URL, "http://")
	return libstack.WithCli(ctx, libstack.DockerCliOptions{Host: host}, callback)
}

func TestComposeBuildPolicyValidatesHostNetworkEntitlement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		build       *types.BuildConfig
		runtimeMode string
		allowHost   bool
		wantErr     string
	}{
		{name: "ordinary build", build: &types.BuildConfig{Context: "."}},
		{name: "runtime host mode does not enable host networking during build", build: &types.BuildConfig{Context: "."}, runtimeMode: "host"},
		{name: "build network host requires administrator opt in", build: &types.BuildConfig{Network: "host"}, wantErr: "host networking"},
		{name: "network host entitlement requires administrator opt in", build: &types.BuildConfig{Entitlements: []string{"network.host"}}, wantErr: "host networking"},
		{name: "build network host is allowed after administrator opt in", build: &types.BuildConfig{Network: "host"}, allowHost: true},
		{name: "network host entitlement is allowed after administrator opt in", build: &types.BuildConfig{Entitlements: []string{"network.host"}}, allowHost: true},
		{name: "other entitlements remain forbidden", build: &types.BuildConfig{Entitlements: []string{"security.insecure"}}, allowHost: true, wantErr: "security.insecure"},
		{name: "privileged builds remain forbidden", build: &types.BuildConfig{Privileged: true}, allowHost: true, wantErr: "privileged builds"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			policy := portainer.DefaultComposeBuildPolicy()
			policy.AllowHostNetwork = testCase.allowHost
			project := &types.Project{Services: types.Services{
				"app": {
					Name:        "app",
					Build:       testCase.build,
					NetworkMode: testCase.runtimeMode,
				},
			}}

			hasBuild, err := validateManagedBuild(project, policy)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				require.False(t, hasBuild)
				return
			}
			require.NoError(t, err)
			require.True(t, hasBuild)
		})
	}
}

func TestLegacyComposeBuildUsesDefaultBuildOptionsAndPinsBuiltImage(t *testing.T) {
	t.Parallel()

	policy := portainer.DefaultComposeBuildPolicy()
	policy.UseLegacyBuild = true
	deployer := NewComposeDeployer()
	deployer.GetBuildPolicy = func() (portainer.ComposeBuildPolicy, error) {
		return policy, nil
	}
	service := &recordingComposeBuild{}
	project := &types.Project{Services: types.Services{
		"app": {Name: "app", Image: "app:shared", Build: &types.BuildConfig{Context: ".", Network: "host"}},
	}}

	server, dockerImages := newFakeDockerImageServer(t)
	var cleanup func()
	err := withTestDockerClient(t.Context(), server, func(ctx context.Context, cli *command.DockerCli) error {
		var err error
		cleanup, err = deployer.build(ctx, cli, service, project)
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	require.True(t, service.called)
	require.Equal(t, composeapi.BuildOptions{}, service.options)
	require.True(t, strings.HasPrefix(service.buildImage, "portainer-build-job:"))
	require.Contains(t, service.buildTags, "app:shared")
	require.Len(t, dockerImages.inspected, 1)
	temporaryTag := dockerImages.inspected[0]
	require.Equal(t, temporaryTag, service.buildImage)
	require.Equal(t, dockerImages.imageIDs[temporaryTag], project.Services["app"].Image)
	require.Nil(t, project.Services["app"].Build)
	cleanup()
	require.Equal(t, []string{temporaryTag}, dockerImages.removed)
}

func TestConcurrentLegacyComposeBuildsPinDistinctSharedTagImages(t *testing.T) {
	policy := portainer.DefaultComposeBuildPolicy()
	policy.UseLegacyBuild = true
	deployer := NewComposeDeployer()
	deployer.GetBuildPolicy = func() (portainer.ComposeBuildPolicy, error) {
		return policy, nil
	}
	server, dockerImages := newFakeDockerImageServer(t)
	buildTags := make(chan string, 2)
	releaseBuilds := make(chan struct{})
	projects := []*types.Project{
		{Services: types.Services{"app": {Name: "app", Image: "app:shared", Build: &types.BuildConfig{Context: "."}}}},
		{Services: types.Services{"app": {Name: "app", Image: "app:shared", Build: &types.BuildConfig{Context: "."}}}},
	}
	services := []*blockingComposeBuild{
		{buildTags: buildTags, release: releaseBuilds},
		{buildTags: buildTags, release: releaseBuilds},
	}
	errors := make(chan error, 2)
	for index := range projects {
		go func(index int) {
		errors <- withTestDockerClient(t.Context(), server, func(ctx context.Context, cli *command.DockerCli) error {
			cleanup, err := deployer.build(ctx, cli, services[index], projects[index])
			if err != nil {
				return err
			}
			defer cleanup()
			return nil
		})
		}(index)
	}

	firstTag := <-buildTags
	secondTag := <-buildTags
	require.NotEqual(t, firstTag, secondTag, "parallel builds of the same Compose tag need distinct temporary image tags")
	close(releaseBuilds)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)

	for index, project := range projects {
		require.Equal(t, composeapi.BuildOptions{}, services[index].options)
		require.Equal(t, dockerImages.imageIDs[services[index].tag], project.Services["app"].Image)
		require.Nil(t, project.Services["app"].Build)
	}
	require.ElementsMatch(t, []string{firstTag, secondTag}, dockerImages.inspected)
	require.ElementsMatch(t, []string{firstTag, secondTag}, dockerImages.removed)
}

func TestManagedBuildRejectsHostNetworkBeforeCallingComposeBuild(t *testing.T) {
	t.Parallel()

	deployer := NewComposeDeployer()
	deployer.GetBuildPolicy = func() (portainer.ComposeBuildPolicy, error) {
		return portainer.DefaultComposeBuildPolicy(), nil
	}
	service := &recordingComposeBuild{}
	project := &types.Project{Services: types.Services{
		"app": {Name: "app", Build: &types.BuildConfig{Network: "host"}},
	}}

	_, err := deployer.build(t.Context(), nil, service, project)
	require.ErrorContains(t, err, "host networking")
	require.False(t, service.called)
}

func TestRequestsHostBuildNetworkOnlyForSelectedBuildServices(t *testing.T) {
	t.Parallel()

	project := &types.Project{
		Services: types.Services{
			"app": {Name: "app", NetworkMode: "host", Build: &types.BuildConfig{Context: "."}},
		},
		DisabledServices: types.Services{
			"profile-service": {Name: "profile-service", Build: &types.BuildConfig{Network: "host"}},
		},
	}
	require.False(t, requestsHostBuildNetwork(project), "runtime host mode and unselected profiles must not switch the builder network")

	app := project.Services["app"]
	app.Build.AdditionalContexts = types.Mapping{"profile": "service:profile-service"}
	project.Services["app"] = app
	require.True(t, requestsHostBuildNetwork(project), "a selected build dependency can request host networking")
}
