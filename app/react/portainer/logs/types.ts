export interface SourceStatus {
  available: boolean;
  partial: boolean;
  message: string;
}

export interface LogsResponse<T> {
  logs: T[];
  totalCount: number;
  sourceStatus: SourceStatus;
}

export interface LogQuery {
  after?: number;
  before?: number;
  keyword?: string;
  username?: string;
  sortBy: string;
  sortDesc: boolean;
  offset?: number;
  limit?: number;
}
