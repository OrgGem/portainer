import { fireEvent, render, waitFor } from '@testing-library/react';
import { HttpResponse, http } from 'msw';

import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';
import { server } from '@/setup-tests/server';

import { HelmCertPanel } from './HelmCertPanel';

vi.mock('@/portainer/services/notifications', () => ({
  notifySuccess: vi.fn(),
  notifyError: vi.fn(),
}));

describe('HelmCertPanel', () => {
  test('allows uploading and applying a CA certificate file', async () => {
    let capturedBody: Record<string, unknown> | null = null;

    server.use(
      http.put('/api/ssl', async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, unknown>;
        return new HttpResponse(null, { status: 204 });
      })
    );

    const Wrapped = withTestQueryProvider(HelmCertPanel);
    const { getByLabelText, getByRole } = render(<Wrapped />);

    const caContent =
      '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n';
    const file = new File([caContent], 'helm-ca.pem', {
      type: 'application/x-pem-file',
    });
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve(caContent),
    });

    const input = getByLabelText(/CA file/i) as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });

    const submitButton = getByRole('button', { name: /Apply changes/i });
    await waitFor(() => expect(submitButton).toBeEnabled());

    fireEvent.submit(submitButton.closest('form')!);

    await waitFor(() => {
      expect(capturedBody).toEqual({ clientCert: caContent });
    });
  });
});
