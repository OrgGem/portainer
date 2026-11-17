import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { vi } from 'vitest';
import { http, HttpResponse } from 'msw';

import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';
import { withTestRouter } from '@/react/test-utils/withRouter';
import { withUserProvider } from '@/react/test-utils/withUserProvider';
import { server } from '@/setup-tests/server';
import { suppressConsoleLogs } from '@/setup-tests/suppress-console';

import { EnvironmentGroup } from '../../types';

import { AccessTab } from './AccessTab';

vi.mock('@/react/hooks/useIdParam', () => ({
  useIdParam: () => 2,
}));

vi.mock('@/portainer/services/notifications', () => ({
  notifyError: vi.fn(),
  notifySuccess: vi.fn(),
}));

const mockGroup: EnvironmentGroup = {
  Id: 2,
  Name: 'Test Group',
  Description: '',
  TagIds: [],
};

function setupMocks({
  group = mockGroup,
  users = [] as Array<{ Id: number; Username: string; Role: number }>,
  teams = [] as Array<{ Id: number; Name: string }>,
  onUpdate = vi.fn(),
  holdUpdate,
}: {
  group?: EnvironmentGroup;
  users?: Array<{ Id: number; Username: string; Role: number }>;
  teams?: Array<{ Id: number; Name: string }>;
  onUpdate?: (body: unknown) => void;
  holdUpdate?: Promise<void>;
} = {}) {
  server.use(
    http.get('/api/endpoint_groups/2', () => HttpResponse.json(group)),
    http.get('/api/users', () => HttpResponse.json(users)),
    http.get('/api/teams', () => HttpResponse.json(teams)),
    http.put('/api/endpoint_groups/2', async ({ request }) => {
      onUpdate(await request.json());
      await holdUpdate;
      return HttpResponse.json(group);
    })
  );

  return onUpdate;
}

function renderAccessTab() {
  const Wrapped = withTestQueryProvider(
    withTestRouter(withUserProvider(AccessTab))
  );
  return render(<Wrapped />);
}

describe('AccessTab group roles', () => {
  test('does not turn a legacy CE RoleId 1 grant into an administrator', async () => {
    setupMocks({
      group: { ...mockGroup, TeamAccessPolicies: { 3: { RoleId: 1 } } },
      teams: [{ Id: 3, Name: 'legacy-team' }],
    });
    renderAccessTab();
    expect(
      await screen.findByLabelText('Role for Group: legacy-team')
    ).toHaveValue('legacy');
    expect(screen.getByRole('button', { name: 'Save role' })).toBeDisabled();
  });

  test('assigns Read-only to a team by default and preserves other grants', async () => {
    const onUpdate = setupMocks({
      group: { ...mockGroup, UserAccessPolicies: { 5: { RoleId: 3 } } },
      users: [{ Id: 5, Username: 'existing-user', Role: 2 }],
      teams: [{ Id: 3, Name: 'readers' }],
    });
    renderAccessTab();
    await userEvent.selectOptions(
      await screen.findByLabelText('Group or user'),
      'team:3'
    );
    await userEvent.click(screen.getByRole('button', { name: 'Assign role' }));
    await waitFor(() =>
      expect(onUpdate).toHaveBeenCalledWith({
        UserAccessPolicies: { 5: { RoleId: 3 } },
        TeamAccessPolicies: { 3: { RoleId: 4, StandaloneRole: 'read-only' } },
      })
    );
  });

  test('changes a team to Environment administrator using only scoped policies', async () => {
    const onUpdate = setupMocks({
      group: { ...mockGroup, TeamAccessPolicies: { 3: { RoleId: 3 } } },
      teams: [{ Id: 3, Name: 'operators' }],
    });
    renderAccessTab();
    await userEvent.selectOptions(
      await screen.findByLabelText('Role for Group: operators'),
      'environment-admin'
    );
    await userEvent.click(screen.getByRole('button', { name: 'Save role' }));
    await waitFor(() =>
      expect(onUpdate).toHaveBeenCalledWith({
        UserAccessPolicies: {},
        TeamAccessPolicies: {
          3: { RoleId: 1, StandaloneRole: 'environment-admin' },
        },
      })
    );
  });

  test('keeps the selected group and role after a failed save for retry', async () => {
    setupMocks({ teams: [{ Id: 3, Name: 'readers' }] });
    server.use(
      http.put(
        '/api/endpoint_groups/2',
        () => new HttpResponse(null, { status: 500 })
      )
    );
    renderAccessTab();
    await userEvent.selectOptions(
      await screen.findByLabelText('Group or user'),
      'team:3'
    );
    const restoreConsole = suppressConsoleLogs();
    await userEvent.click(screen.getByRole('button', { name: 'Assign role' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Assign role' })).toBeEnabled()
    );
    expect(screen.getByLabelText('Group or user')).toHaveValue('team:3');
    expect(screen.getByLabelText('Environment role')).toHaveValue('read-only');
    restoreConsole();
  });
});

test('allows legacy group access without writing a StandaloneRole', async () => {
  const onUpdate = setupMocks({
    group: { ...mockGroup, UserAccessPolicies: {}, TeamAccessPolicies: {} },
    teams: [{ Id: 3, Name: 'mixed-team' }],
  });
  renderAccessTab();
  await userEvent.selectOptions(
    await screen.findByLabelText('Group or user'),
    'team:3'
  );
  await userEvent.selectOptions(
    screen.getByLabelText('Environment role'),
    'legacy'
  );
  await userEvent.click(screen.getByRole('button', { name: 'Assign role' }));
  await waitFor(() =>
    expect(onUpdate).toHaveBeenCalledWith({
      UserAccessPolicies: {},
      TeamAccessPolicies: { 3: { RoleId: 1 } },
    })
  );
});
