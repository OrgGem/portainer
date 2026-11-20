import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';

import { server } from '@/setup-tests/server';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { VaultConnectionPanel } from './VaultConnectionPanel';

const savedConnection = {
  Address: 'https://vault.example.com:8200',
  AuthMount: 'approle',
  RoleID: 'role-id-visible-to-admin',
  CAPEM: '',
  HasSecretID: true,
};

function renderPanel() {
  const Component = withTestQueryProvider(VaultConnectionPanel);
  return render(<Component environmentId={7} />);
}

test('keeps SecretID write-only in the environment form', async () => {
  server.use(
    http.get('/api/endpoints/7/vault', () => HttpResponse.json(savedConnection))
  );
  renderPanel();

  const secretInput = await screen.findByLabelText('AppRole SecretID');
  expect(secretInput).toHaveValue('');
  expect(secretInput).not.toBeRequired();
  expect(screen.getByPlaceholderText('Configured — leave blank to keep')).toBe(
    secretInput
  );
  expect(screen.queryByText(/secret-id-that-was-saved/i)).toBeNull();
});

test('retains the saved SecretID when the admin saves an unchanged address with a blank field', async () => {
  let savedSecretID = 'saved-secret-id';
  let update: unknown;
  server.use(
    http.get('/api/endpoints/7/vault', () =>
      HttpResponse.json(savedConnection)
    ),
    http.put('/api/endpoints/7/vault', async ({ request }) => {
      update = await request.json();
      const payload = update as { SecretID: string };
      if (payload.SecretID) savedSecretID = payload.SecretID;
      return HttpResponse.json({ ok: true });
    })
  );
  renderPanel();

  await userEvent.click(
    await screen.findByRole('button', { name: 'Save Vault connection' })
  );

  await waitFor(() => expect(update).toBeDefined());
  expect(update).toMatchObject({
    Address: savedConnection.Address,
    AuthMount: savedConnection.AuthMount,
    RoleID: savedConnection.RoleID,
    SecretID: '',
  });
  expect(savedSecretID).toBe('saved-secret-id');
});

test('requires a new SecretID after the Vault address changes', async () => {
  server.use(
    http.get('/api/endpoints/7/vault', () => HttpResponse.json(savedConnection))
  );
  renderPanel();

  const address = await screen.findByLabelText('Vault URL');
  await userEvent.clear(address);
  await userEvent.type(address, 'https://new-vault.example.com');

  expect(screen.getByLabelText('AppRole SecretID')).toBeRequired();
});
