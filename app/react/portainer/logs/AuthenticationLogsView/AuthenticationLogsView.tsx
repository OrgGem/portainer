import { useState } from 'react';

import { PageHeader } from '@@/PageHeader';

import { LogsFilterBar, LogMultiFilter } from '../components/LogsFilterBar';
import { DateRange, dateRangeToQuery, getLastDaysRange } from '../dateRange';
import { useExportLogs } from '../useExportLogs';
import { useLogsQuery } from '../useLogsQuery';
import { useLogTableState } from '../useLogTableState';

import { AuthenticationLogsTable } from './AuthenticationLogsTable';
import { ActivityType, AuthLog, AuthMethodType } from './types';

const methods = [
  { value: AuthMethodType.Unknown, label: 'Unknown' },
  { value: AuthMethodType.Internal, label: 'Internal' },
  { value: AuthMethodType.LDAP, label: 'LDAP' },
  { value: AuthMethodType.OAuth, label: 'OAuth' },
];
const results = [
  { value: ActivityType.AuthSuccess, label: 'Success' },
  { value: ActivityType.AuthFailure, label: 'Failure' },
  { value: ActivityType.Logout, label: 'Logout' },
];

export function AuthenticationLogsView() {
  const table = useLogTableState();
  const [range, setRange] = useState<DateRange | undefined>(() =>
    getLastDaysRange(7)
  );
  const [username, setUsername] = useState('');
  const [contexts, setContexts] = useState<number[]>(
    methods.map(({ value }) => value)
  );
  const [types, setTypes] = useState<number[]>(
    results.map(({ value }) => value)
  );
  const query = {
    ...dateRangeToQuery(range),
    ...(username ? { username } : {}),
    ...(table.keyword ? { keyword: table.keyword } : {}),
    contexts: contexts.join(','),
    types: types.join(','),
    sortBy: table.sort.id,
    sortDesc: table.sort.desc,
    offset: table.page * table.limit,
    limit: table.limit,
  };
  const logs = useLogsQuery<AuthLog>('authlogs', query);
  const exportLogs = useExportLogs('authlogs');

  return (
    <>
      <PageHeader
        title="User authentication logs"
        breadcrumbs="User authentication logs"
        reload
      />
      <div className="mx-4">
        <LogsFilterBar
          range={range}
          onRangeChange={(value) => {
            setRange(value);
            table.resetPage();
          }}
          username={username}
          onUsernameChange={(value) => {
            setUsername(value);
            table.resetPage();
          }}
          description="Authentication events are recorded from activation onward and retained for up to 30 days. Earlier events are unavailable."
          sourceStatus={logs.data?.sourceStatus}
          isCheckingSource={logs.isFetching}
          queryError={logs.error}
          exportError={exportLogs.error}
          isExporting={exportLogs.isLoading}
          onExport={() => exportLogs.mutate(query)}
          exportDataCy="authentication-logs-export-csv-button"
        >
          <LogMultiFilter
            label="Authentication method"
            value={contexts}
            onChange={(value) => {
              setContexts(value);
              table.resetPage();
            }}
            options={methods}
          />
          <LogMultiFilter
            label="Result"
            value={types}
            onChange={(value) => {
              setTypes(value);
              table.resetPage();
            }}
            options={results}
          />
        </LogsFilterBar>
        {!logs.isError && logs.data?.sourceStatus.available !== false && (
          <AuthenticationLogsTable
            dataset={logs.data?.logs}
            isLoading={logs.isLoading}
            currentPage={table.page}
            onChangePage={table.setPage}
            limit={table.limit}
            onChangeLimit={table.setLimit}
            keyword={table.keyword}
            onChangeKeyword={table.setKeyword}
            sort={table.sort}
            onChangeSort={table.setSort}
            totalItems={logs.data?.totalCount || 0}
          />
        )}
      </div>
    </>
  );
}
