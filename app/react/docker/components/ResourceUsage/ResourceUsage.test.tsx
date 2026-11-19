import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';

import { server } from '@/setup-tests/server';
import { suppressConsoleLogs } from '@/setup-tests/suppress-console';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';
import {
  EnvironmentStatus,
  EnvironmentType,
} from '@/react/portainer/environments/types';

import { MiniMetric } from './MiniMetric';
import { ResourceUsagePanel } from './ResourceUsage';
import { ResourceUsage } from './types';

const fixture: ResourceUsage = {
  sampledAt: '2026-11-17T10:00:00Z',
  storageSampledAt: '2026-11-17T09:59:30Z',
  cpu: { used: 0.25, total: 0.5, unit: 'cores', basis: 'CPU quota' },
  memory: {
    used: 32 * 1024 ** 2,
    total: 64 * 1024 ** 2,
    unit: 'bytes',
    basis: 'memory limit',
  },
  writableLayer: {
    used: 4096,
    total: null,
    unit: 'bytes',
    basis: 'no known quota',
  },
  mounts: [
    {
      target: '/data',
      name: 'app-data',
      type: 'volume',
      readOnly: false,
      data: {
        used: 100 * 1024,
        total: 200 * 1024,
        unit: 'bytes',
        basis: 'volume quota',
      },
      filesystem: {
        used: 100,
        total: 1000,
        unit: 'bytes',
        basis: 'shared filesystem',
      },
    },
  ],
  state: 'running',
  source: 'Docker Engine',
  notes: [],
};
const env = {
  Id: 1,
  Type: EnvironmentType.Docker,
  Status: EnvironmentStatus.Up,
};

test('renders CPU and RAM quotas while container storage shows usage only', async () => {
  server.use(
    http.get('/api/endpoints/1/containers/abc/metrics', () =>
      HttpResponse.json(fixture)
    )
  );
  const Panel = withTestQueryProvider(ResourceUsagePanel);
  const { container } = render(<Panel environment={env} containerId="abc" />);
  expect(await screen.findByText('0.25 cores / 0.50 cores')).toBeVisible();
  expect(screen.getByRole('meter', { name: 'CPU' })).toHaveAttribute(
    'aria-valuenow',
    '50'
  );
  expect(screen.getByText('32.0 MiB / 64.0 MiB')).toBeVisible();
  expect(screen.getByText('Vol /data')).toBeVisible();
  expect(screen.getAllByTitle(/app-data/)[0]).toHaveAttribute(
    'title',
    expect.stringContaining('Volume data: 100.0 KiB')
  );
  expect(screen.getByTitle('Layer: 4.0 KiB')).toHaveTextContent(
    /Layer\s*4.0 KiB/
  );
  expect(screen.getAllByTitle(/Volume data: 100.0 KiB/).length).toBeGreaterThan(
    0
  );
  expect(screen.getByText('Vol /data').parentElement).toHaveTextContent(
    '100.0 KiB'
  );
  expect(screen.queryByText(/100\.0 KiB \/ 200\.0 KiB/)).toBeNull();
  expect(screen.queryByText(/Shared filesystem:/)).toBeNull();
  expect(screen.queryByRole('meter', { name: 'Layer' })).toBeNull();
  expect(screen.queryByRole('meter', { name: 'Volume data' })).toBeNull();

  expect(container.querySelectorAll('svg polyline').length).toBeGreaterThan(0);
});

test('shows zero and unavailable mount measurements without capacity', async () => {
  server.use(
    http.get('/api/endpoints/1/containers/abc/metrics', () =>
      HttpResponse.json({
        ...fixture,
        writableLayer: {
          ...fixture.writableLayer!,
          used: 0,
          total: 8 * 1024,
        },
        mounts: [
          {
            ...fixture.mounts[0],
            data: { ...fixture.mounts[0].data, used: 0, total: 4096 },
          },
          {
            ...fixture.mounts[0],
            target: '/unmeasured',
            data: { ...fixture.mounts[0].data, used: null, total: null },
          },
        ],
      })
    )
  );
  const Panel = withTestQueryProvider(ResourceUsagePanel);
  render(<Panel environment={env} containerId="abc" />);

  expect(await screen.findAllByTitle('Layer: 0 B')).not.toHaveLength(0);
  expect(screen.getByText('Vol /data').parentElement).toHaveTextContent('0 B');
  expect(screen.getByText('+1')).toBeVisible();
  const mountDetails = screen.getAllByTitle(/Volume data: 0 B/);
  expect(mountDetails.length).toBeGreaterThan(0);
  expect(
    mountDetails.some((element) =>
      element.getAttribute('title')?.includes('Volume data: Unavailable')
    )
  ).toBe(true);
  expect(screen.queryByText(/4096 B|8\.0 KiB/)).toBeNull();
});

test('retains host filesystem capacities in the host resource panel', async () => {
  server.use(
    http.get('/api/endpoints/1/metrics', () =>
      HttpResponse.json({
        ...fixture,
        writableLayer: undefined,
        mounts: [
          {
            ...fixture.mounts[0],
            type: 'filesystem',
            target: '/var/lib/docker',
            data: {
              used: 900,
              total: 1000,
              unit: 'bytes',
              basis: 'host filesystem',
            },
            filesystem: undefined,
          },
        ],
      })
    )
  );
  const Panel = withTestQueryProvider(ResourceUsagePanel);
  render(<Panel environment={env} />);

  expect(await screen.findByText('CPU', { selector: 'span' })).toBeVisible();
  expect(screen.getByText('Filesystem')).toBeVisible();
  expect(screen.getByText('900 B / 1000 B')).toBeVisible();
  expect(screen.getByRole('meter', { name: 'Filesystem' })).toHaveAttribute(
    'aria-valuenow',
    '90'
  );
});

test('permission failures show unavailable without fabricated zeroes', async () => {
  const restore = suppressConsoleLogs();
  server.use(
    http.get('/api/endpoints/1/metrics', () =>
      HttpResponse.json({ message: 'Permission denied' }, { status: 403 })
    )
  );
  const Panel = withTestQueryProvider(ResourceUsagePanel);
  render(<Panel environment={env} />);
  expect(
    await screen.findByText('CPU / RAM / Storage: Unavailable')
  ).toBeVisible();
  expect(screen.queryByRole('meter')).toBeNull();
  restore();
});

test('offline environments do not request live samples', async () => {
  let calls = 0;
  server.use(
    http.get('/api/endpoints/1/metrics', () => {
      calls += 1;
      return HttpResponse.json(fixture);
    })
  );
  const Panel = withTestQueryProvider(ResourceUsagePanel);
  render(<Panel environment={{ ...env, Status: EnvironmentStatus.Down }} />);
  expect(screen.getByText('CPU / RAM / Storage: Unavailable')).toBeVisible();
  await waitFor(() => expect(calls).toBe(0));
});

test('history is bounded and a missing measurement has no percentage', () => {
  const { rerender, container } = render(
    <MiniMetric label="CPU" metric={fixture.cpu} sampledAt="0" />
  );
  for (let i = 1; i <= 40; i += 1)
    rerender(
      <MiniMetric label="CPU" metric={fixture.cpu} sampledAt={String(i)} />
    );
  expect(
    container.querySelector('polyline')?.getAttribute('points')?.split(' ')
  ).toHaveLength(30);
  rerender(
    <MiniMetric
      label="CPU"
      metric={{ ...fixture.cpu, used: null }}
      sampledAt="41"
    />
  );
  expect(screen.queryByRole('meter')).toBeNull();
  expect(container.querySelector('polyline')).toBeNull();
});

test('known disk usage without quota has a trend but no percentage meter', () => {
  const { rerender, container } = render(
    <MiniMetric
      label="Volume"
      metric={{ used: 100, total: null, unit: 'bytes', basis: 'no quota' }}
      sampledAt="0"
    />
  );
  rerender(
    <MiniMetric
      label="Volume"
      metric={{ used: 200, total: null, unit: 'bytes', basis: 'no quota' }}
      sampledAt="1"
    />
  );
  expect(container.querySelector('polyline')?.getAttribute('points')).toBe(
    '0,10 56,2'
  );
  expect(screen.queryByRole('meter')).toBeNull();
});
