import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import axios from '@/portainer/services/axios/axios';

import { useLogsQuery } from './useLogsQuery';

function LogsQueryHarness() {
  useLogsQuery('logs', { sortBy: 'timestamp', sortDesc: true });
  return null;
}

describe('useLogsQuery cancellation', () => {
  afterEach(() => vi.restoreAllMocks());

  it('passes React Query cancellation through to the HTTP request', async () => {
    let signal: AbortSignal | undefined;
    const get = vi.spyOn(axios, 'get').mockImplementation((_url, config) => {
      signal = config?.signal as AbortSignal;
      return new Promise((_resolve, reject) => {
        signal?.addEventListener('abort', () => reject(new Error('canceled')));
      });
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { unmount } = render(
      <QueryClientProvider client={queryClient}>
        <LogsQueryHarness />
      </QueryClientProvider>
    );

    await waitFor(() => expect(get).toHaveBeenCalledOnce());
    expect(signal).toBeInstanceOf(AbortSignal);
    expect(signal?.aborted).toBe(false);

    unmount();

    await waitFor(() => expect(signal?.aborted).toBe(true));
    queryClient.clear();
  });
});
