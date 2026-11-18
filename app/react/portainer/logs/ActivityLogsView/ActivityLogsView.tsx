import { useState } from 'react';

import { PageHeader } from '@@/PageHeader';

import {
  LogSelectFilter,
  LogsFilterBar,
  LogTextFilter,
} from '../components/LogsFilterBar';
import { DateRange, dateRangeToQuery, getLastDaysRange } from '../dateRange';
import { useExportLogs } from '../useExportLogs';
import { useLogTableState } from '../useLogTableState';

import { ActivityLogsTable } from './ActivityLogsTable';
import { useActivityLogs } from './useActivityLogs';

const sources = [
  { value: 'configuration', label: 'Configuration' },
  { value: 'deployment', label: 'Deployment' },
  { value: 'metadata', label: 'Stack metadata' },
];
const actions = [
  { value: 'upload', label: 'Upload' },
  { value: 'apply', label: 'Apply' },
  { value: 'rollback', label: 'Rollback' },
  { value: 'recover', label: 'Recover' },
  { value: 'unknown', label: 'Unknown' },
  { value: 'deployment', label: 'Deployment' },
  { value: 'create', label: 'Create' },
  { value: 'update', label: 'Update' },
];
const statuses = [
  { value: 'success', label: 'Success' },
  { value: 'failure', label: 'Failure' },
  { value: 'in_progress', label: 'In progress' },
  { value: 'inactive', label: 'Inactive' },
  { value: 'unknown', label: 'Unknown' },
];

export function ActivityLogsView() {
  const table = useLogTableState();
  const [range, setRange] = useState<DateRange | undefined>(() =>
    getLastDaysRange(7)
  );
  const [username, setUsername] = useState('');
  const [endpointId, setEndpointId] = useState('');
  const [stackId, setStackId] = useState('');
  const [source, setSource] = useState('');
  const [action, setAction] = useState('');
  const [status, setStatus] = useState('');
  const query = {
    ...dateRangeToQuery(range),
    ...(username ? { username } : {}),
    ...(table.keyword ? { keyword: table.keyword } : {}),
    ...(endpointId ? { endpointId } : {}),
    ...(stackId ? { stackId } : {}),
    ...(source ? { source } : {}),
    ...(action ? { action } : {}),
    ...(status ? { status } : {}),
    sortBy: table.sort.id,
    sortDesc: table.sort.desc,
    offset: table.page * table.limit,
    limit: table.limit,
  };
  const logs = useActivityLogs(query);
  const exportLogs = useExportLogs('logs');

  return (
    <>
      <PageHeader
        title="User activity logs"
        breadcrumbs="User activity logs"
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
          allowAllDates
          description="Configuration history contains at most 200 existing events. Deployment entries show the current deployment status, not service health or a deployment history. Stack create and last-update entries are metadata snapshots; deleted stacks no longer appear."
          sourceStatus={logs.data?.sourceStatus}
          isCheckingSource={logs.isFetching}
          queryError={logs.error}
          exportError={exportLogs.error}
          isExporting={exportLogs.isLoading}
          onExport={() => exportLogs.mutate(query)}
          exportDataCy="activity-logs-export-csv-button"
        >
          <LogTextFilter
            id="activity-endpoint"
            label="Environment ID"
            value={endpointId}
            onChange={(value) => {
              setEndpointId(value);
              table.resetPage();
            }}
            placeholder="Exact environment ID"
            numeric
          />
          <LogTextFilter
            id="activity-stack"
            label="Stack ID"
            value={stackId}
            onChange={(value) => {
              setStackId(value);
              table.resetPage();
            }}
            placeholder="Exact stack ID"
            numeric
          />
          <LogSelectFilter
            id="activity-source"
            label="Source"
            value={source}
            onChange={(value) => {
              setSource(value);
              table.resetPage();
            }}
            options={sources}
          />
          <LogSelectFilter
            id="activity-action"
            label="Action"
            value={action}
            onChange={(value) => {
              setAction(value);
              table.resetPage();
            }}
            options={actions}
          />
          <LogSelectFilter
            id="activity-status"
            label="Status"
            value={status}
            onChange={(value) => {
              setStatus(value);
              table.resetPage();
            }}
            options={statuses}
          />
        </LogsFilterBar>
        {!logs.isError && logs.data?.sourceStatus.available !== false && (
          <ActivityLogsTable
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
