import { History } from 'lucide-react';

import { Datatable } from '@@/datatables';

import { AuthLog } from './types';
import { columns } from './columns';

export function AuthenticationLogsTable({
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
  dataset?: AuthLog[];
}) {
  return (
    <Datatable<AuthLog>
      title="Authentication events"
      titleIcon={History}
      columns={columns}
      dataset={dataset || []}
      isLoading={isLoading}
      emptyContentLabel="No authentication logs match the selected filters."
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
      data-cy="authentication-logs-datatable"
    />
  );
}
