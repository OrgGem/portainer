import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';

import { server } from '@/setup-tests/server';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { RoleManagement } from './RoleManagement';

test('loads saved settings, saves edits and restores defaults only on Save', async () => {
  const user = userEvent.setup();
  let saved = {
    Enabled: true,
    Features: { 'containers.view': true, 'containers.operate': false },
    Version: 4,
  };
  let writes = 0;
  server.use(
    http.get('/api/roles/configuration', () =>
      HttpResponse.json({
        Roles: [
          {
            Role: { Id: 5, Name: 'Operator', Description: 'Operate workloads' },
            Key: 'operator',
            Policy: saved,
            Defaults: {
              Enabled: true,
              Features: { 'containers.view': true, 'containers.operate': true },
              Version: 1,
            },
          },
        ],
        Features: [
          { ID: 'containers.view', Name: 'View containers' },
          { ID: 'containers.operate', Name: 'Operate containers' },
        ],
      })
    ),
    http.put('/api/roles/5/policy', async ({ request }) => {
      const body = (await request.json()) as typeof saved;
      expect(body.Version).toBe(saved.Version);
      saved = { ...body, Version: saved.Version + 1 };
      writes += 1;
      return HttpResponse.json(saved);
    })
  );
  const View = withTestQueryProvider(RoleManagement);
  render(<View />);
  await user.click(await screen.findByRole('button', { name: 'Operator' }));
  expect(
    screen.getByRole('checkbox', { name: 'Operate containers' })
  ).not.toBeChecked();
  await user.click(screen.getByRole('button', { name: 'Restore defaults' }));
  expect(writes).toBe(0);
  await user.click(screen.getByRole('button', { name: 'Save role' }));
  await screen.findByText(/Role saved/);
  expect(saved.Features['containers.operate']).toBe(true);
  await user.click(screen.getByRole('checkbox', { name: 'Role enabled' }));
  await user.click(screen.getByRole('button', { name: 'Save role' }));
  await screen.findByRole('button', { name: 'Operator (disabled)' });
  expect(saved.Enabled).toBe(false);
});
