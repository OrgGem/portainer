import { useQuery } from '@tanstack/react-query';
import { isCancel } from 'axios';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';

import { LogQuery, LogsResponse } from './types';

export type LogEndpoint = 'authlogs' | 'logs';

export function useLogsQuery<T>(endpoint: LogEndpoint, query: LogQuery) {
  return useQuery({
    queryKey: ['userActivity', endpoint, query] as const,
    queryFn: async ({ signal }) => {
      try {
        const { data } = await axios.get<LogsResponse<T>>(
          `/useractivity/${endpoint}`,
          { params: query, signal }
        );
        return data;
      } catch (error) {
        if (signal?.aborted || isCancel(error)) {
          throw error;
        }
        throw parseAxiosError(error, 'Failed to load logs');
      }
    },
  });
}
