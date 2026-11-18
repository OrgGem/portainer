export type ActivitySource = 'configuration' | 'deployment' | 'metadata';
export type ActivityStatus =
  | 'success'
  | 'failure'
  | 'in_progress'
  | 'inactive'
  | 'unknown';

export interface ActivityLog {
  id: string;
  timestamp: number;
  username: string;
  context: string;
  action: string;
  status: ActivityStatus;
  source: ActivitySource;
  environmentId: number;
  stackId: number;
  stackName: string;
  payload: Record<string, string | number | boolean | null>;
}
