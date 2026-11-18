import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { useLogTableState } from './useLogTableState';

function LogTableStateHarness() {
  const state = useLogTableState();

  return (
    <>
      <output data-cy="page">{state.page}</output>
      <output data-cy="limit">{state.limit}</output>
      <output data-cy="keyword">{state.keyword}</output>
      <output data-cy="sort">{state.sort.id}</output>
      <button type="button" onClick={() => state.setPage(4)}>Set page</button>
      <button type="button" onClick={() => state.setLimit(2001)}>Set oversized limit</button>
      <button type="button" onClick={() => state.setLimit(0)}>Set invalid limit</button>
      <button type="button" onClick={() => state.setLimit(50)}>Set normal limit</button>
      <button type="button" onClick={() => state.setKeyword('deploy')}>Set keyword</button>
      <button type="button" onClick={() => state.setSort({ id: 'username', desc: false })}>
        Set sort
      </button>
    </>
  );
}

describe('useLogTableState', () => {
  it('starts at 25 rows and clamps requested page sizes to 1 through 100', () => {
    render(<LogTableStateHarness />);

    expect(screen.getByTestId('limit')).toHaveTextContent('25');
    fireEvent.click(screen.getByRole('button', { name: 'Set oversized limit' }));
    expect(screen.getByTestId('limit')).toHaveTextContent('100');
    fireEvent.click(screen.getByRole('button', { name: 'Set invalid limit' }));
    expect(screen.getByTestId('limit')).toHaveTextContent('1');
  });

  it('returns to the first page after filters, sort, or page size change', () => {
    render(<LogTableStateHarness />);

    fireEvent.click(screen.getByRole('button', { name: 'Set page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Set keyword' }));
    expect(screen.getByTestId('page')).toHaveTextContent('0');
    expect(screen.getByTestId('keyword')).toHaveTextContent('deploy');

    fireEvent.click(screen.getByRole('button', { name: 'Set page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Set sort' }));
    expect(screen.getByTestId('page')).toHaveTextContent('0');
    expect(screen.getByTestId('sort')).toHaveTextContent('username');

    fireEvent.click(screen.getByRole('button', { name: 'Set page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Set normal limit' }));
    expect(screen.getByTestId('page')).toHaveTextContent('0');
    expect(screen.getByTestId('limit')).toHaveTextContent('50');
  });
});
