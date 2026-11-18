export type DateRange = { start: Date; end: Date | null };

/** The current day and the preceding calendar days in the browser's timezone. */
export function getLastDaysRange(days: number, now = new Date()): DateRange {
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const start = new Date(
    end.getFullYear(),
    end.getMonth(),
    end.getDate() - days + 1
  );
  return { start, end };
}

/** Convert inclusive local dates to the API's half-open UTC interval in seconds. */
export function dateRangeToQuery(range?: DateRange) {
  if (!range) {
    return {};
  }

  const { start, end } = range;
  const after = new Date(
    start.getFullYear(),
    start.getMonth(),
    start.getDate()
  );
  const before = end
    ? new Date(end.getFullYear(), end.getMonth(), end.getDate() + 1)
    : undefined;

  return {
    after: Math.floor(after.getTime() / 1000),
    ...(before ? { before: Math.floor(before.getTime() / 1000) } : {}),
  };
}
