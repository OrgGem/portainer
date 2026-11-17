import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { vi } from 'vitest';

import { server } from '@/setup-tests/server';
import { withTestRouter } from '@/react/test-utils/withRouter';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { VaultMappingsView } from './VaultMappingsView';
import { InventoryResponse } from './types';

vi.mock('@@/PageHeader', () => ({ PageHeader: () => <h1>Vault mappings</h1> }));

const fixture: InventoryResponse = {
  Items: [
    {
      ID: 'one',
      Service: 'web',
      Variable: 'DB_PASSWORD',
      Path: 'kv/data/apps/demo',
      Key: 'password',
      Engine: 'kv-v2',
      EnvironmentID: 7,
      EnvironmentName: 'Production',
      StackID: 12,
      StackName: 'backend',
      Version: 2,
      UpdatedAt: 1,
      UpdatedBy: 1,
    },
  ],
  Summary: {
    Mappings: 1,
    Variables: 1,
    Paths: 1,
    Environments: 1,
    Stacks: 1,
    Services: 1,
  },
  Environments: [{ ID: 7, Name: 'Production' }],
  Stacks: [{ ID: 12, Name: 'backend', EnvironmentID: 7 }],
};

test('displays authorized reference statistics and sends combined filters to the API', async () => {
  const requests: URLSearchParams[] = [];
  server.use(
    http.get('/api/stacks/vault/mappings', ({ request }) => {
      requests.push(new URL(request.url).searchParams);
      return HttpResponse.json(fixture);
    })
  );
  const Component = withTestQueryProvider(withTestRouter(VaultMappingsView));
  render(<Component />);
  expect(await screen.findByText('DB_PASSWORD')).toBeVisible();
  expect(
    within(screen.getByLabelText('Filtered mapping statistics')).getByText(
      'Distinct path names'
    )
  ).toBeVisible();
  await userEvent.selectOptions(screen.getByLabelText('Environment'), '7');
  await userEvent.selectOptions(screen.getByLabelText('Stack'), '12');
  await userEvent.type(screen.getByLabelText('Environment variable'), 'DB_');
  await userEvent.type(screen.getByLabelText('Vault path'), 'apps/demo');
  await userEvent.type(screen.getByLabelText('Vault key'), 'password');
  await userEvent.type(screen.getByLabelText('Service'), 'web');
  await userEvent.click(screen.getByRole('button', { name: 'Apply filters' }));
  await waitFor(() => expect(requests.length).toBe(2));
  expect(Object.fromEntries(requests[1])).toEqual({
    environmentId: '7',
    stackId: '12',
    variable: 'DB_',
    path: 'apps/demo',
    key: 'password',
    service: 'web',
    offset: '0',
    limit: '50',
  });
  await screen.findByText('DB_PASSWORD');
  await userEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
  expect(screen.getByLabelText('Vault path')).toHaveValue('');
  expect(screen.getByLabelText('Stack')).toHaveValue('');
});

test('an empty authorized inventory renders zero counts and no private options', async () => {
  server.use(
    http.get('/api/stacks/vault/mappings', () =>
      HttpResponse.json({
        Items: [],
        Summary: {
          Mappings: 0,
          Variables: 0,
          Paths: 0,
          Environments: 0,
          Stacks: 0,
          Services: 0,
        },
        Environments: [],
        Stacks: [],
      })
    )
  );
  const Component = withTestQueryProvider(withTestRouter(VaultMappingsView));
  render(<Component />);
  expect(
    await screen.findByText('No visible mappings match these filters.')
  ).toBeVisible();
  expect(
    within(screen.getByLabelText('Environment')).getAllByRole('option')
  ).toHaveLength(1);
  expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
});
