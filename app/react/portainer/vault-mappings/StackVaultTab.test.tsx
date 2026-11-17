import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { vi } from 'vitest';

import { server } from '@/setup-tests/server';
import { suppressConsoleLogs } from '@/setup-tests/suppress-console';
import { withTestRouter } from '@/react/test-utils/withRouter';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';
import { Stack } from '@/react/common/stacks/types';

import { StackVaultTab } from './StackVaultTab';
import { StackVaultResponse } from './types';

vi.mock('@/portainer/services/notifications', () => ({
  notifyError: vi.fn(),
  notifySuccess: vi.fn(),
}));

const fixture: StackVaultResponse = {
  Configuration: {
    Version: 4,
    UpdatedAt: 1,
    UpdatedBy: 1,
    Mappings: [
      {
        ID: 'one',
        Service: 'web',
        Variable: 'DB_PASSWORD',
        Path: 'kv/data/apps/demo',
        Key: 'password',
        Engine: 'kv-v2',
      },
    ],
  },
  Services: ['web', 'worker'],
  CanEdit: true,
};

function setup(canEdit = true) {
  server.use(
    http.get('/api/stacks/1/vault', () =>
      HttpResponse.json({ ...fixture, CanEdit: canEdit })
    )
  );
  const Component = withTestQueryProvider(withTestRouter(StackVaultTab));
  render(<Component stack={{ Id: 1, EndpointId: 1 } as Stack} />);
}

test('a viewer sees references and export but no write controls or secret input', async () => {
  setup(false);
  expect(await screen.findByText('DB_PASSWORD')).toBeVisible();
  expect(screen.getByText('kv/data/apps/demo')).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Edit reference' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Add mapping' })).toBeNull();
  expect(screen.queryByLabelText(/secret value/i)).toBeNull();
  expect(
    screen.getByText('Saved configuration, runtime not verified.')
  ).toBeVisible();
});

test('editing submits reference-only fields with the version and preserves the draft on conflict', async () => {
  const updates: unknown[] = [];
  server.use(
    http.put('/api/stacks/1/vault', async ({ request }) => {
      updates.push(await request.json());
      return HttpResponse.json(
        { message: 'Vault mappings changed; reload before saving' },
        { status: 409 }
      );
    })
  );
  setup();
  await userEvent.click(
    await screen.findByRole('button', { name: 'Edit reference' })
  );
  await userEvent.clear(screen.getByLabelText('Vault API path'));
  await userEvent.type(
    screen.getByLabelText('Vault API path'),
    'kv/data/apps/updated'
  );
  const restore = suppressConsoleLogs();
  try {
    await userEvent.click(screen.getByRole('button', { name: 'Save mapping' }));
    await screen.findByRole('alert');
    expect(updates).toEqual([
      {
        Version: 4,
        Mappings: [
          {
            ...fixture.Configuration.Mappings[0],
            Path: 'kv/data/apps/updated',
          },
        ],
      },
    ]);
    expect(screen.getByLabelText('Vault API path')).toHaveValue(
      'kv/data/apps/updated'
    );
  } finally {
    restore();
  }
});

test('removing a mapping saves an empty list with the current version', async () => {
  const onSave = vi.fn();
  server.use(
    http.put('/api/stacks/1/vault', async ({ request }) => {
      onSave(await request.json());
      return HttpResponse.json({
        ...fixture.Configuration,
        Version: 5,
        Mappings: [],
      });
    })
  );
  setup();
  await userEvent.click(
    await screen.findByRole('button', { name: 'Remove reference' })
  );
  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith({ Version: 4, Mappings: [] })
  );
});

test('exports the selected service from the saved configuration', async () => {
  const requests: string[] = [];
  server.use(
    http.get('/api/stacks/1/vault/template', ({ request }) => {
      requests.push(new URL(request.url).searchParams.get('service') || '');
      return HttpResponse.json({
        Version: 4,
        Content:
          'env_template "DB_PASSWORD" {\n  error_on_missing_key = true\n}',
      });
    })
  );
  setup(false);
  await userEvent.selectOptions(
    await screen.findByLabelText('Service to export'),
    'web'
  );
  await userEvent.click(
    screen.getByRole('button', { name: 'Preview Agent template' })
  );
  expect(await screen.findByLabelText('Agent template references')).toHaveValue(
    'env_template "DB_PASSWORD" {\n  error_on_missing_key = true\n}'
  );
  expect(requests).toEqual(['web']);
});
