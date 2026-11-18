import { useState } from 'react';

export function useLogTableState() {
  const [page, setPage] = useState(0);
  const [limit, setLimit] = useState(25);
  const [keyword, setKeyword] = useState('');
  const [sort, setSort] = useState({ id: 'timestamp', desc: true });

  return {
    page,
    setPage,
    limit,
    keyword,
    sort,
    setLimit: (value: number) => {
      setLimit(Math.min(100, Math.max(1, value)));
      setPage(0);
    },
    setKeyword: (value: string) => {
      setKeyword(value);
      setPage(0);
    },
    setSort: (value: { id: string; desc: boolean }) => {
      setSort(value);
      setPage(0);
    },
    resetPage: () => setPage(0),
  };
}
