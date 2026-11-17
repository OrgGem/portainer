import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { vi } from 'vitest';

import { server } from '@/setup-tests/server';
import { withTestRouter } from '@/react/test-utils/withRouter';

import { VolumeTextEditor } from './VolumeTextEditor';
import {
  editableContent,
  encodeContent,
  textValidationError,
} from './textFiles';

vi.mock('@uiw/react-codemirror', () => ({
  default: ({
    value,
    onChange,
    readOnly,
    onKeyDown,
  }: {
    value: string;
    onChange(value: string): void;
    readOnly: boolean;
    onKeyDown: React.KeyboardEventHandler;
  }) => (
    <textarea
      aria-label="File content"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      readOnly={readOnly}
      onKeyDown={onKeyDown}
    />
  ),
}));
const confirmDiscard = vi.hoisted(() => vi.fn());
vi.mock('@@/modals/confirm', () => ({ confirmGenericDiscard: confirmDiscard }));

const route = '/api/endpoints/1/docker/v2/browse/text';
const version = 'a'.repeat(64);
function setup(content = '{"enabled":true}\n', file = 'app.json') {
  server.use(
    http.get(route, () =>
      HttpResponse.json({ Content: content, Version: version })
    )
  );
  const close = vi.fn();
  const saved = vi.fn();
  const Editor = withTestRouter(VolumeTextEditor);
  render(
    <Editor
      target={{
        endpointId: 1,
        volumeId: 'app-data',
        path: `/nested/${file}`,
        nodeName: 'node-a',
      }}
      onClose={close}
      onSaved={saved}
    />
  );
  return { close, saved };
}

test('saves the latest keystrokes, file version, nested path and explicit agent target', async () => {
  const requests: unknown[] = [];
  server.use(
    http.put(route, async ({ request }) => {
      const data = await request.json();
      requests.push({
        data,
        volume: new URL(request.url).searchParams.get('volumeID'),
        path: new URL(request.url).searchParams.get('path'),
        node: request.headers.get('X-PortainerAgent-Target'),
      });
      return HttpResponse.json({ Content: '{"n":1}', Version: 'b'.repeat(64) });
    })
  );
  const { saved } = setup();
  const editor = await screen.findByLabelText('File content');
  fireEvent.change(editor, { target: { value: '{"n":1}' } });
  fireEvent.keyDown(editor, { key: 's', ctrlKey: true });
  expect(await screen.findByText('Saved and verified.')).toBeVisible();
  expect(requests).toEqual([
    {
      data: { Content: '{"n":1}', Version: version },
      volume: 'app-data',
      path: '/nested/app.json',
      node: 'node-a',
    },
  ]);
  expect(saved).toHaveBeenCalledOnce();
  expect(
    screen.getByRole('button', { name: 'Save', exact: true })
  ).toBeDisabled();
});

test('conflict keeps the draft and reload requires discard confirmation', async () => {
  server.use(
    http.put(route, () =>
      HttpResponse.json(
        { message: 'File changed since it was opened' },
        { status: 409 }
      )
    )
  );
  setup();
  const editor = await screen.findByLabelText('File content');
  fireEvent.change(editor, { target: { value: '{"draft":true}' } });
  await userEvent.click(
    screen.getByRole('button', { name: 'Save', exact: true })
  );
  expect(await screen.findByRole('alert')).toHaveTextContent('File changed');
  expect(editor).toHaveValue('{"draft":true}');
  confirmDiscard.mockResolvedValueOnce(false);
  await userEvent.click(screen.getByRole('button', { name: 'Reload file' }));
  expect(editor).toHaveValue('{"draft":true}');
  confirmDiscard.mockResolvedValueOnce(true);
  await userEvent.click(screen.getByRole('button', { name: 'Reload file' }));
  await waitFor(() => expect(editor).toHaveValue('{"enabled":true}\n'));
});

test('invalid JSON cannot be saved and closing a dirty draft is guarded', async () => {
  const { close } = setup();
  const editor = await screen.findByLabelText('File content');
  fireEvent.change(editor, { target: { value: '{invalid' } });
  expect(
    screen.getByRole('button', { name: 'Save', exact: true })
  ).toBeDisabled();
  expect(screen.getByRole('alert')).toHaveTextContent('Invalid JSON');
  confirmDiscard.mockResolvedValueOnce(false);
  await userEvent.click(
    screen.getByRole('button', { name: 'Close', exact: true })
  );
  expect(close).not.toHaveBeenCalled();
  confirmDiscard.mockResolvedValueOnce(true);
  await userEvent.click(
    screen.getByRole('button', { name: 'Close', exact: true })
  );
  expect(close).toHaveBeenCalledOnce();
});

test('UTF-8 BOM and CRLF survive editing and empty non-JSON files can be saved', async () => {
  const requests: Array<{ Content: string }> = [];
  server.use(
    http.put(route, async ({ request }) => {
      const data = (await request.json()) as { Content: string };
      requests.push(data);
      return HttpResponse.json({ ...data, Version: 'b'.repeat(64) });
    })
  );
  setup('\uFEFFline one\r\nline two\r\n', 'app.conf');
  const editor = await screen.findByLabelText('File content');
  expect(editor).toHaveValue('line one\nline two\n');
  fireEvent.change(editor, { target: { value: 'short\n' } });
  await userEvent.click(
    screen.getByRole('button', { name: 'Save', exact: true })
  );
  await screen.findByText('Saved and verified.');
  expect(requests[0].Content).toBe('\uFEFFshort\r\n');
  expect(encodeContent('', editableContent('old'))).toBe('');
});

test('binary/read errors leave no editable buffer; mixed endings and oversized Unicode are rejected', async () => {
  setup();
  server.use(
    http.get(route, () =>
      HttpResponse.json(
        { message: 'Quick edit supports UTF-8 text files only' },
        { status: 415 }
      )
    )
  );
  expect(await screen.findByRole('alert')).toHaveTextContent('UTF-8');
  expect(screen.queryByLabelText('File content')).toBeNull();
  expect(() => editableContent('one\r\ntwo\n')).toThrow('Mixed');
  expect(
    textValidationError('app.conf', 'ế'.repeat(400000), editableContent(''))
  ).toContain('1 MiB');
});
