import { describe, expect, it } from 'vitest';

import { dateRangeToQuery, getLastDaysRange } from './dateRange';

describe('log date ranges', () => {
  it('converts inclusive local dates to a half-open UTC interval', () => {
    const start = new Date(2026, 2, 8);
    const end = new Date(2026, 2, 10);

    expect(dateRangeToQuery({ start, end })).toEqual({
      after: Math.floor(start.getTime() / 1000),
      before: Math.floor(new Date(2026, 2, 11).getTime() / 1000),
    });
  });

  it('omits the upper bound for an open-ended date range', () => {
    const start = new Date(2026, 5, 2);

    expect(dateRangeToQuery({ start, end: null })).toEqual({
      after: Math.floor(start.getTime() / 1000),
    });
  });

  it('returns no date filters when all available dates are selected', () => {
    expect(dateRangeToQuery(undefined)).toEqual({});
  });

  it('uses local calendar days for the last N days range', () => {
    const now = new Date(2026, 2, 10, 23, 45, 30);
    const range = getLastDaysRange(7, now);

    expect(range.start).toEqual(new Date(2026, 2, 4));
    expect(range.end).toEqual(new Date(2026, 2, 10));
  });
});
