import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';

import { server } from '@/setup-tests/server';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import {
  BuildPolicyPanel,
  buildPolicyValidation,
  defaultBuildPolicy,
} from './BuildPolicyPanel';

test('shows the enforced defaults and saves a shared policy through settings', async () => {
  const payloads: unknown[] = [];
  server.use(
    http.put('/api/settings', async ({ request }) => {
      const body = await request.json();
      payloads.push(body);
      return HttpResponse.json(body);
    })
  );
  const Panel = withTestQueryProvider(BuildPolicyPanel);
  render(<Panel />);
  expect(screen.getByLabelText('CPU limit (%)')).toHaveValue(50);
  const cgroupSwitch = screen.getByLabelText(
    'Automatically configure private cgroup for builder'
  );
  expect(cgroupSwitch).not.toBeChecked();
  fireEvent.click(cgroupSwitch);
  await waitFor(() => expect(cgroupSwitch).toBeChecked());
  expect(screen.getByLabelText('Memory limit (%)')).toHaveValue(50);
  expect(screen.getByLabelText('Concurrent builds per host')).toHaveValue(1);
  fireEvent.change(screen.getByLabelText('CPU limit (%)'), {
    target: { value: '25' },
  });
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Save build policy' })
    ).toBeEnabled()
  );
  fireEvent.click(screen.getByRole('button', { name: 'Save build policy' }));
  await waitFor(() =>
    expect(payloads).toEqual([
      {
        ComposeBuildPolicy: {
          CPUPercent: 25,
          MemoryPercent: 50,
          MaxConcurrentBuilds: 1,
          AutoConfigureBuilderCgroup: true,
        },
      },
    ])
  );
});

test.each([
  { CPUPercent: 51 },
  { MemoryPercent: 51 },
  { CPUPercent: 0 },
  { MemoryPercent: -1 },
  { CPUPercent: 1.5 },
  { MaxConcurrentBuilds: 0 },
  { MaxConcurrentBuilds: 9 },
])('rejects a policy outside the enforced budget: %j', async (invalid) => {
  expect(
    await buildPolicyValidation.isValid({ ...defaultBuildPolicy, ...invalid })
  ).toBe(false);
});

test('loads persisted policy values', () => {
  const Panel = withTestQueryProvider(BuildPolicyPanel);
  render(
    <Panel
      policy={{
        CPUPercent: 20,
        MemoryPercent: 30,
        MaxConcurrentBuilds: 2,
        AutoConfigureBuilderCgroup: true,
      }}
    />
  );
  expect(screen.getByLabelText('CPU limit (%)')).toHaveValue(20);
  expect(screen.getByLabelText('Memory limit (%)')).toHaveValue(30);
  expect(screen.getByLabelText('Concurrent builds per host')).toHaveValue(2);
  expect(
    screen.getByLabelText('Automatically configure private cgroup for builder')
  ).toBeChecked();
});
