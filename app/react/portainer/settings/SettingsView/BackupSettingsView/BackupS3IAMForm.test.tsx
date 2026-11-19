import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';

import { server } from '@/setup-tests/server';
import { suppressConsoleLogs } from '@/setup-tests/suppress-console';
import { withTestQueryProvider } from '@/react/test-utils/withTestQuery';

import { BackupS3IAMForm } from './BackupS3IAMForm';

const settings = {
  bucketName: 'portainer-backups-b',
  region: 'ap-southeast-2',
  prefix: 'current/config',
  enabled: true,
  dailyTime: '02:00',
  lastRun: {
    at: '2026-11-19T02:00:00Z',
    status: 'success' as const,
    key: 'previous/config/portainer-backup_20261119.tar.gz',
    destination: {
      bucketName: 'portainer-backups-a',
      region: 'ap-southeast-1',
      prefix: 'previous/config',
    },
  },
};

test('loads IAM S3 settings and runs an immediate backup using saved settings', async () => {
  let executeBody: unknown;
  server.use(
    http.get('/api/backup/s3/settings', () => HttpResponse.json(settings)),
    http.post('/api/backup/s3/execute', async ({ request }) => {
      executeBody = await request.json();
      return HttpResponse.json({
        at: '2026-11-19T03:00:00Z',
        status: 'success',
        key: 'prod/portainer/portainer-backup.tar.gz',
      });
    })
  );

  const Form = withTestQueryProvider(BackupS3IAMForm);
  render(<Form />);

  expect(await screen.findByDisplayValue('portainer-backups-b')).toBeVisible();
  expect(screen.getByDisplayValue('02:00')).toBeVisible();
  expect(screen.getByText(/Last backup success/)).toBeVisible();
  expect(
    screen.getByText(
      'Destination: s3://portainer-backups-a/previous/config (ap-southeast-1)'
    )
  ).toBeVisible();
  expect(
    screen.getByText(
      'Object key: previous/config/portainer-backup_20261119.tar.gz'
    )
  ).toBeVisible();
  expect(screen.getByText(/IAM role available to this server/)).toBeVisible();
  expect(screen.queryByLabelText(/Access key|Secret access key/i)).toBeNull();

  fireEvent.click(screen.getByRole('button', { name: 'Backup now' }));
  await waitFor(() => expect(executeBody).toEqual({}));
});

test('shows the saved destination from a failed run after settings change', async () => {
  const failedSettings = {
    ...settings,
    lastRun: {
      at: '2026-11-19T03:00:00Z',
      status: 'failed' as const,
      message: 'S3 rejected the backup upload (AccessDenied)',
      destination: {
        bucketName: 'portainer-backups-a',
        region: 'ap-southeast-1',
        prefix: 'previous/config',
      },
    },
  };
  server.use(
    http.get('/api/backup/s3/settings', () => HttpResponse.json(failedSettings))
  );

  const Form = withTestQueryProvider(BackupS3IAMForm);
  render(<Form />);

  expect(await screen.findByDisplayValue('portainer-backups-b')).toBeVisible();
  expect(screen.getByText(/Last backup failed/)).toBeVisible();
  expect(
    screen.getByText(
      'Destination: s3://portainer-backups-a/previous/config (ap-southeast-1)'
    )
  ).toBeVisible();
  expect(
    screen.getByText('S3 rejected the backup upload (AccessDenied)')
  ).toBeVisible();
  expect(screen.queryByText(/Object key:/)).toBeNull();
});

test('marks a legacy run destination as unavailable', async () => {
  const legacySettings = {
    ...settings,
    lastRun: {
      at: '2026-11-19T02:00:00Z',
      status: 'success' as const,
      key: 'legacy/backup.tar.gz',
    },
  };
  server.use(
    http.get('/api/backup/s3/settings', () => HttpResponse.json(legacySettings))
  );

  const Form = withTestQueryProvider(BackupS3IAMForm);
  render(<Form />);

  expect(
    await screen.findByText('Destination unavailable for this historical run.')
  ).toBeVisible();
  expect(screen.getByText('Object key: legacy/backup.tar.gz')).toBeVisible();
});

test('retains unsaved form values and does not run a backup when saving fails', async () => {
  const restoreConsole = suppressConsoleLogs();
  server.use(
    http.get('/api/backup/s3/settings', () => HttpResponse.json(settings)),
    http.post('/api/backup/s3/settings', () =>
      HttpResponse.json({ message: 'settings write failed' }, { status: 500 })
    )
  );

  const Form = withTestQueryProvider(BackupS3IAMForm);
  render(<Form />);

  const region = await screen.findByDisplayValue('ap-southeast-2');
  fireEvent.change(region, { target: { value: 'ap-southeast-1' } });

  const save = screen.getByRole('button', { name: 'Save backup settings' });
  expect(save).toBeEnabled();
  fireEvent.click(save);

  expect(await screen.findByRole('alert')).toHaveTextContent(
    'settings write failed'
  );
  expect(region).toHaveValue('ap-southeast-1');
  expect(screen.getByRole('button', { name: 'Backup now' })).toBeDisabled();
  expect(
    screen.getByText(/Save changes before running a backup/)
  ).toBeVisible();
  restoreConsole();
});

test('shows a retry action when settings cannot be loaded', async () => {
  const restoreConsole = suppressConsoleLogs();
  server.use(
    http.get('/api/backup/s3/settings', () =>
      HttpResponse.json({ message: 'settings unavailable' }, { status: 500 })
    )
  );

  const Form = withTestQueryProvider(BackupS3IAMForm);
  render(<Form />);

  expect(
    await screen.findByText('Unable to load S3 backup settings.')
  ).toBeVisible();
  expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled();
  restoreConsole();
});
