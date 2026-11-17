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
      data: { used: null, total: null, unit: 'bytes', basis: 'unknown' },
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

test('renders quota-relative usage, mini charts and mount capacities in the list row', async () => {
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
    expect.stringContaining('Shared filesystem: 100 B / 1000 B')
  );
  expect(screen.queryByRole('meter', { name: 'Volume data' })).toBeNull();

  expect(container.querySelectorAll('svg polyline').length).toBeGreaterThan(0);
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
