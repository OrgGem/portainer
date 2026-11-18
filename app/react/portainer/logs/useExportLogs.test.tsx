import { Blob as NodeBlob } from 'node:buffer';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import {
  AxiosError,
  AxiosHeaders,
  InternalAxiosRequestConfig,
  isAxiosError,
} from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';

import axios from '@/portainer/services/axios/axios';

import { LogQuery } from './types';
import { filenameFromHeader, useExportLogs } from './useExportLogs';

const saveAsMock = vi.hoisted(() => vi.fn());

vi.mock('file-saver', () => ({ saveAs: saveAsMock }));

function ExportHarness({
  endpoint,
  query,
  onError,
}: {
  endpoint: 'authlogs' | 'logs';
  query: LogQuery;
  onError(error: unknown): void;
}) {
  const mutation = useExportLogs(endpoint);

  return (
    <button type="button" onClick={() => mutation.mutate(query, { onError })}>
      Export
    </button>
  );
}

describe('useExportLogs', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    saveAsMock.mockReset();
  });

  it('exports every matching row with the current filters and no table paging', async () => {
    const blob = new Blob(['csv']);
    const get = vi.spyOn(axios, 'get').mockResolvedValue({
      data: blob,
      headers: { 'content-disposition': 'attachment; filename="logs.csv"' },
    });
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ExportHarness
          endpoint="logs"
          onError={vi.fn()}
          query={{
        after: 100,
        before: 200,
        username: 'alice',
        keyword: 'stack',
        offset: 2000,
        limit: 25,
        sortBy: 'timestamp',
        sortDesc: true,
          }}
        />
      </QueryClientProvider>
    );
    fireEvent.click(screen.getByRole('button', { name: 'Export' }));

    await waitFor(() => expect(saveAsMock).toHaveBeenCalledOnce());

    expect(get).toHaveBeenCalledWith('/useractivity/logs.csv', {
      params: {
        after: 100,
        before: 200,
        username: 'alice',
        keyword: 'stack',
        sortBy: 'timestamp',
        sortDesc: true,
      },
      responseType: 'blob',
      signal: expect.any(AbortSignal),
    });
    expect(saveAsMock).toHaveBeenCalledWith(blob, 'logs.csv');
  });

  it('shows the server message when a CSV error arrives as a blob', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.stubGlobal('Blob', NodeBlob);
    const responseBlob = new NodeBlob(
      [JSON.stringify({ details: 'The log source is incomplete.' })],
      { type: 'application/json' }
    );
    const config = {} as InternalAxiosRequestConfig;
    const requestError = new AxiosError(
      'Request failed',
      AxiosError.ERR_BAD_RESPONSE,
      config,
      undefined,
      {
        config,
        data: responseBlob,
        headers: new AxiosHeaders(),
        status: 503,
        statusText: 'Service Unavailable',
      }
    );
    expect(isAxiosError(requestError)).toBe(true);
    expect(requestError.response?.data).toBeInstanceOf(Blob);
    const get = vi.spyOn(axios, 'get').mockRejectedValue(requestError);
    const onError = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ExportHarness
          endpoint="authlogs"
          onError={onError}
          query={{ sortBy: 'timestamp', sortDesc: true }}
        />
      </QueryClientProvider>
    );
    fireEvent.click(screen.getByRole('button', { name: 'Export' }));

    await waitFor(() => expect(onError).toHaveBeenCalledOnce());
    expect(onError.mock.calls[0][0]).toMatchObject({
      message: 'Failed to export logs: The log source is incomplete.',
    });

    expect(get).toHaveBeenCalledOnce();
    expect(saveAsMock).not.toHaveBeenCalled();
  });

  it('aborts an active export on unmount and never saves the canceled blob', async () => {
    let signal: AbortSignal | undefined;
    const get = vi.spyOn(axios, 'get').mockImplementation((_url, config) => {
      signal = config?.signal as AbortSignal;
      return new Promise((_resolve, reject) => {
        signal?.addEventListener('abort', () => reject(new Error('canceled')));
      });
    });
    const onError = vi.fn();
    const { unmount } = render(
      <QueryClientProvider client={new QueryClient()}>
        <ExportHarness
          endpoint="logs"
          onError={onError}
          query={{ sortBy: 'timestamp', sortDesc: true }}
        />
      </QueryClientProvider>
    );
    fireEvent.click(screen.getByRole('button', { name: 'Export' }));
    await waitFor(() => expect(get).toHaveBeenCalledOnce());
    expect(signal?.aborted).toBe(false);

    unmount();

    await waitFor(() => expect(signal?.aborted).toBe(true));
    expect(saveAsMock).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it('uses safe names from content disposition and falls back on malformed names', () => {
    expect(
      filenameFromHeader(
        "attachment; filename=logs.csv; filename*=UTF-8''logs%20%C3%A9.csv",
        'fallback.csv'
      )
    ).toBe('logs é.csv');
    expect(
      filenameFromHeader('attachment; filename="../unsafe.csv"', 'fallback.csv')
    ).toBe('.._unsafe.csv');
    expect(filenameFromHeader(undefined, 'fallback.csv')).toBe('fallback.csv');
  });
});
