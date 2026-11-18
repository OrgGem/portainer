import { createColumnHelper } from '@tanstack/react-table';
import { History, Search } from 'lucide-react';

import { isoDateFromTimestamp } from '@/portainer/filters/filters';

import { ExpandableDatatable } from '@@/datatables/ExpandableDatatable';
import { Button } from '@@/buttons';
import { JsonTree } from '@@/JsonTree';

import { ActivityLog } from './types';

const columnHelper = createColumnHelper<ActivityLog>();

const columns = [
  columnHelper.accessor('timestamp', {
    header: 'Time',
    cell: ({ getValue }) =>
      getValue() ? isoDateFromTimestamp(getValue()) : 'Unknown',
  }),
  columnHelper.accessor('username', {
    header: 'User',
    cell: ({ getValue }) => getValue() || 'Unknown',
  }),
  columnHelper.accessor('context', {
    header: 'Environment',
    cell: ({ getValue }) => getValue() || 'Unknown',
  }),
  columnHelper.accessor('stackName', {
    header: 'Stack',
    enableSorting: false,
    cell: ({ getValue }) => getValue() || '—',
  }),
  columnHelper.accessor('source', { header: 'Source' }),
  columnHelper.accessor('action', { header: 'Action' }),
  columnHelper.accessor('status', {
    header: 'Status',
    cell: ({ getValue }) => getValue().replaceAll('_', ' '),
  }),
  columnHelper.accessor('payload', {
    header: 'Details',
    enableSorting: false,
    cell: ({ row }) => (
      <Button
        color="link"
        onClick={() => row.toggleExpanded()}
        icon={Search}
        aria-expanded={row.getIsExpanded()}
        data-cy={`activity-logs-inspect_${row.index}`}
      >
        Inspect
      </Button>
    ),
  }),
];

export function ActivityLogsTable({
  dataset,
  isLoading,
  currentPage,
  keyword,
  limit,
  onChangeKeyword,
  onChangeLimit,
  onChangePage,
  onChangeSort,
  sort,
  totalItems,
}: {
  keyword: string;
  onChangeKeyword(keyword: string): void;
  sort: { id: string; desc: boolean };
  onChangeSort(sort: { id: string; desc: boolean }): void;
  limit: number;
  onChangeLimit(limit: number): void;
  currentPage: number;
  onChangePage(page: number): void;
  totalItems: number;
  isLoading: boolean;
  dataset?: ActivityLog[];
}) {
  return (
    <ExpandableDatatable<ActivityLog>
      title="Activity logs"
      titleIcon={History}
      columns={columns}
      dataset={dataset || []}
      isLoading={isLoading}
      emptyContentLabel="No activity logs match the selected filters."
      settingsManager={{
        pageSize: limit,
        search: keyword,
        setPageSize: onChangeLimit,
        setSearch: onChangeKeyword,
        setSortBy: (id, desc) => onChangeSort({ id: id || 'timestamp', desc }),
        sortBy: sort,
      }}
      page={currentPage}
      onPageChange={onChangePage}
      isServerSidePagination
      totalCount={totalItems}
      showAllRowsOption={false}
      disableSelect
      renderSubRow={(row) => <SubRow item={row.original} />}
      data-cy="activity-logs-datatable"
    />
  );
}

function SubRow({ item }: { item: ActivityLog }) {
  return (
    <tr>
      <td colSpan={Number.MAX_SAFE_INTEGER}>
        <JsonTree
          data={{
            environmentId: item.environmentId,
            stackId: item.stackId,
            ...item.payload,
          }}
        />
      </td>
    </tr>
  );
}
