import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { ActivityLogsTable } from './ActivityLogsView/ActivityLogsTable';
import { AuthenticationLogsTable } from './AuthenticationLogsView/AuthenticationLogsTable';

const sharedProps = {
  dataset: [],
  isLoading: false,
  currentPage: 0,
  keyword: '',
  sort: { id: 'timestamp', desc: true },
  limit: 25,
  totalItems: 2501,
  onChangeKeyword: () => {},
  onChangeLimit: () => {},
  onChangePage: () => {},
  onChangeSort: () => {},
};

describe('log tables', () => {
  it.each([
    ['authentication', AuthenticationLogsTable],
    ['activity', ActivityLogsTable],
  ])('%s page sizes stay capped at 100', (_name, LogsTable) => {
    render(<LogsTable {...sharedProps} />);

    const selector = screen.getByRole('combobox', { name: 'Items per page' });
    expect(within(selector).getAllByRole('option').map((option) => option.textContent)).toEqual([
      '10',
      '25',
      '50',
      '100',
    ]);
    expect(within(selector).queryByRole('option', { name: 'All' })).not.toBeInTheDocument();
  });
});
