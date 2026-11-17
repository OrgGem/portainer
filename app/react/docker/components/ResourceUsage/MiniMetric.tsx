import { useState } from 'react';

import { Metric } from './types';

export function formatMetric(value: number | null, unit: Metric['unit']) {
  if (value === null || !Number.isFinite(value)) return 'Unavailable';
  if (unit === 'cores') return `${value.toFixed(2)} cores`;
  if (value === 0) return '0 B';
  const power = Math.min(
    4,
    Math.max(0, Math.floor(Math.log(value) / Math.log(1024)))
  );
  return `${(value / 1024 ** power).toFixed(power === 0 ? 0 : 1)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][power]}`;
}

export function MiniMetric({
  label,
  metric,
  sampledAt,
  compact = false,
  description,
}: {
  label: string;
  metric: Metric;
  sampledAt: string;
  compact?: boolean;
  description?: string;
}) {
  const { used, total } = metric;
  const percent =
    used !== null && total !== null && total > 0 ? (used / total) * 100 : null;
  const [history, setHistory] = useState<{
    at: string;
    used: number | null;
    points: number[];
  }>({ at: '', used: null, points: [] });
  if (
    (history.at !== sampledAt || history.used !== used) &&
    used !== null &&
    Number.isFinite(used)
  ) {
    setHistory({
      at: sampledAt,
      used,
      points: [...history.points.slice(-29), Math.max(0, used)],
    });
  }
  const scale =
    total !== null && total > 0 ? total : Math.max(1, ...history.points);
  const points = history.points
    .map(
      (p, i) =>
        `${(i / Math.max(1, history.points.length - 1)) * 56},${18 - Math.min(1, p / scale) * 16}`
    )
    .join(' ');

  return (
    <div
      title={
        description ||
        (compact
          ? `${label}: ${formatMetric(used, metric.unit)} / ${total === null ? 'no known quota' : formatMetric(total, metric.unit)}\n${metric.basis}`
          : metric.basis)
      }
      className={compact ? 'min-w-0 text-xs' : 'min-w-[210px] text-xs'}
    >
      <div className="flex items-center justify-between gap-2">
        {compact ? (
          <span className="min-w-0 flex-1 truncate tabular-nums">
            <span className="font-medium">{label}</span>{' '}
            {formatMetric(used, metric.unit)} /{' '}
            {total === null
              ? 'no known quota'
              : formatMetric(total, metric.unit)}
          </span>
        ) : (
          <>
            <span className="font-medium">{label}</span>
            <span className="whitespace-nowrap tabular-nums">
              {formatMetric(used, metric.unit)} /{' '}
              {total === null
                ? 'no known quota'
                : formatMetric(total, metric.unit)}
            </span>
          </>
        )}
        <svg
          width="58"
          height="20"
          viewBox="0 0 58 20"
          aria-hidden="true"
          className="shrink-0 text-blue-6"
        >
          <path d="M0 19 H58" stroke="currentColor" opacity="0.2" />
          {used !== null && (
            <polyline
              points={points}
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            />
          )}
        </svg>
      </div>
      <div
        role={percent === null ? undefined : 'meter'}
        aria-label={label}
        aria-valuemin={percent === null ? undefined : 0}
        aria-valuemax={percent === null ? undefined : 100}
        aria-valuenow={
          percent === null ? undefined : Math.min(100, Math.max(0, percent))
        }
        aria-valuetext={`${formatMetric(used, metric.unit)} of ${total === null ? 'unknown quota' : formatMetric(total, metric.unit)}; ${metric.basis}`}
        className="h-1 overflow-hidden rounded bg-gray-3 th-dark:bg-gray-8"
      >
        {percent !== null && (
          <div
            className={
              percent >= 90 ? 'h-full bg-warning-6' : 'h-full bg-blue-6'
            }
            style={{ width: `${Math.min(100, Math.max(0, percent))}%` }}
          />
        )}
      </div>
    </div>
  );
}
