import { LogQuery } from '../types';
import { useLogsQuery } from '../useLogsQuery';

import { ActivityLog } from './types';

export interface ActivityQuery extends LogQuery {
  endpointId?: string;
  stackId?: string;
  source?: string;
  action?: string;
  status?: string;
}

export function useActivityLogs(query: ActivityQuery) {
  return useLogsQuery<ActivityLog>('logs', query);
}
