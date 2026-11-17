import { render, screen } from '@testing-library/react';
import { vi } from 'vitest';

import { Stack, StackStatus } from '@/react/common/stacks/types';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { ConfigurationTab } from './ConfigurationTab';
import { Controls } from './types';

const mocks = vi.hoisted(() => ({ controls: {} as Controls, mutate: vi.fn() }));
vi.mock('@/react/hooks/useUser', () => ({ useIsPureAdmin: () => false }));
vi.mock('./queries', () => ({
  useStackControls: () => ({
    data: mocks.controls,
    isLoading: false,
    isError: false,
  }),
  useControlMutation: () => ({ mutate: mocks.mutate, isLoading: false }),
}));

const stack = { Id: 1, Status: StackStatus.Active } as Stack;

beforeEach(() => {
  mocks.mutate.mockReset();
  mocks.controls = {
    Policy: { Version: 1, Grants: [] },
    Permissions: ['view'],
    Services: ['web'],
    Configuration: {
      Version: 1,
      Targets: [{ ID: 'app', Service: 'web', Target: '/etc/app.conf' }],
      Revisions: [
        {
          ID: 'draft',
          TargetID: 'app',
          Filename: 'app.conf',
          SHA256: 'checksum',
          CreatedAt: 1,
          CreatedBy: 2,
          Size: 10,
        },
      ],
      Events: [],
    },
  };
});

it('lets a viewer review history without upload, apply or permission editing', () => {
  const Component = withTestQueryProvider(ConfigurationTab);
  render(<Component stack={stack} />);
  expect(screen.getByRole('button', { name: 'Review' })).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Upload draft' })).toBeNull();
  expect(
    screen.queryByRole('button', { name: 'Apply this version' })
  ).toBeNull();
  expect(screen.queryByText('Stack operation permissions')).toBeNull();
  expect(mocks.mutate).not.toHaveBeenCalled();
});

it('separates upload permission from apply permission', () => {
  mocks.controls.Permissions = ['view', 'config.upload'];
  const Component = withTestQueryProvider(ConfigurationTab);
  render(<Component stack={stack} />);
  expect(screen.getByRole('button', { name: 'Upload draft' })).toBeDisabled();
  expect(
    screen.queryByRole('button', { name: 'Apply this version' })
  ).toBeNull();
});

it('blocks new uploads while an interrupted deployment needs recovery', () => {
  mocks.controls.Permissions = ['view', 'config.upload', 'config.apply'];
  mocks.controls.Configuration!.PendingRevision = 'draft';
  const Component = withTestQueryProvider(ConfigurationTab);
  render(<Component stack={stack} />);
  expect(screen.getByRole('alert')).toHaveTextContent('interrupted');
  expect(screen.getByRole('button', { name: 'Upload draft' })).toBeDisabled();
  expect(
    screen.queryByRole('button', { name: 'Recover previous configuration' })
  ).toBeNull();
});
