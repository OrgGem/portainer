import { beforeEach, describe, expect, it, vi } from 'vitest';

const { configBlocks, moduleApi } = vi.hoisted(() => ({
  configBlocks: [],
  moduleApi: { config: vi.fn((block) => configBlocks.push(block)) },
}));

vi.mock('angular', () => ({
  default: { module: vi.fn(() => moduleApi) },
}));

vi.mock('@/portainer/models/endpoint/models', () => ({
  PortainerEndpointTypes: {
    DockerEnvironment: 1,
    AgentOnDockerEnvironment: 2,
    EdgeAgentOnDockerEnvironment: 4,
  },
}));

vi.mock('@/react/portainer/environments/types', () => ({
  EnvironmentStatus: { Up: 1, Down: 2 },
}));

vi.mock('@/portainer/authorization-guard', () => ({
  AccessHeaders: { Admin: 'admin', Restricted: 'restricted' },
}));

vi.mock('./react', () => ({ reactModule: 'docker.react' }));

import './__module.js';

describe('Docker environment entry', () => {
  let dockerState;

  beforeEach(() => {
    const registry = new Map();
    const configBlock = configBlocks[0];
    configBlock[configBlock.length - 1]({
      register: (state) => registry.set(state.name, state),
    });
    dockerState = registry.get('docker');
  });

  it('opens for a non-admin when the saved environment status is stale', async () => {
    const dependencies = makeDependencies({ savedStatus: 2, isAdmin: false });

    await enterDockerEnvironment(dependencies);

    expect(dependencies.SystemService.ping).toHaveBeenCalledWith(40);
    expect(dependencies.EndpointService.updateEndpoint).not.toHaveBeenCalled();
    expect(dependencies.endpoint.Status).toBe(1);
    expect(dependencies.StateManager.updateEndpointState).toHaveBeenCalledWith(dependencies.endpoint);
    expect(dependencies.$state.go).not.toHaveBeenCalled();
  });

  it('lets a global admin persist a changed Docker environment status', async () => {
    const dependencies = makeDependencies({ savedStatus: 2, isAdmin: true });

    await enterDockerEnvironment(dependencies);

    expect(dependencies.EndpointService.updateEndpoint).toHaveBeenCalledWith(40, {
      Status: 1,
    });
    expect(dependencies.endpoint.Status).toBe(1);
  });

  it('still reports an unreachable environment to a non-admin', async () => {
    const dependencies = makeDependencies({ savedStatus: 1, isAdmin: false });
    dependencies.SystemService.ping.mockRejectedValue(new Error('unreachable'));

    await enterDockerEnvironment(dependencies);

    expect(dependencies.EndpointService.updateEndpoint).not.toHaveBeenCalled();
    expect(dependencies.endpoint.Status).toBe(2);
    expect(dependencies.EndpointProvider.clean).toHaveBeenCalled();
    expect(dependencies.Notifications.error).toHaveBeenCalledWith('Failed loading environment', expect.any(Error));
    expect(dependencies.$state.go).toHaveBeenCalledWith('portainer.home', {}, { reload: true, inherit: false });
  });

  async function enterDockerEnvironment(dependencies) {
    const { endpoint, $state, EndpointService, Notifications, StateManager, SystemService, EndpointProvider, Authentication } = dependencies;
    return dockerState.onEnter(endpoint, (callback) => callback(), $state, EndpointService, Notifications, StateManager, SystemService, EndpointProvider, Authentication);
  }
});

function makeDependencies({ savedStatus, isAdmin }) {
  return {
    endpoint: { Id: 40, Name: 'test-agent', Type: 2, Status: savedStatus },
    $state: { go: vi.fn() },
    EndpointService: { updateEndpoint: vi.fn().mockResolvedValue(undefined) },
    Notifications: { error: vi.fn() },
    StateManager: { updateEndpointState: vi.fn().mockResolvedValue(undefined) },
    SystemService: { ping: vi.fn().mockResolvedValue(undefined) },
    EndpointProvider: { clean: vi.fn() },
    Authentication: { isPureAdmin: vi.fn(() => isAdmin) },
  };
}
