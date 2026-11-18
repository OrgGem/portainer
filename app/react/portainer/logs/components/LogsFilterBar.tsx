import { ReactNode } from 'react';
import { DownloadIcon } from 'lucide-react';

import { Button } from '@@/buttons';
import { FormControl } from '@@/form-components/FormControl';
import { TextTip } from '@@/Tip/TextTip';
import { Widget } from '@@/Widget';

import { DateRange, getLastDaysRange } from '../dateRange';
import { SourceStatus } from '../types';

import { DateRangePicker } from './DateRangePicker';

interface Props {
  range?: DateRange;
  onRangeChange(value?: DateRange): void;
  username: string;
  onUsernameChange(value: string): void;
  allowAllDates?: boolean;
  children?: ReactNode;
  description: ReactNode;
  sourceStatus?: SourceStatus;
  isCheckingSource: boolean;
  queryError?: unknown;
  exportError?: unknown;
  isExporting: boolean;
  onExport(): void;
  exportDataCy: string;
}

export function LogsFilterBar({
  range,
  onRangeChange,
  username,
  onUsernameChange,
  allowAllDates,
  children,
  description,
  sourceStatus,
  isCheckingSource,
  queryError,
  exportError,
  isExporting,
  onExport,
  exportDataCy,
}: Props) {
  const disabledReason = getExportDisabledReason(
    sourceStatus,
    isCheckingSource,
    queryError
  );

  return (
    <Widget>
      <Widget.Body>
        <form
          className="form-horizontal"
          onSubmit={(event) => event.preventDefault()}
        >
          <DateRangePicker value={range} onChange={onRangeChange} />
          <div className="form-group">
            <div className="col-sm-offset-3 col-sm-9 col-lg-offset-2 col-lg-10">
              <Button
                color="default"
                onClick={() => onRangeChange(getLastDaysRange(7))}
                data-cy="logs-last-7-days"
              >
                Last 7 days
              </Button>
              <Button
                color="default"
                onClick={() => onRangeChange(getLastDaysRange(30))}
                data-cy="logs-last-30-days"
              >
                Last 30 days
              </Button>
              {allowAllDates && (
                <Button
                  color="default"
                  onClick={() => onRangeChange(undefined)}
                  data-cy="logs-all-available-dates"
                >
                  All available dates
                </Button>
              )}
              <div className="small text-muted">
                Dates use your local time zone and include the entire end date.
              </div>
            </div>
          </div>
          <LogTextFilter
            id="logs-username"
            label="User"
            value={username}
            onChange={onUsernameChange}
            placeholder="Username contains"
          />
          {children}
          <div className="mb-2">
            <TextTip color="blue" inline={false}>
              {description}
            </TextTip>
          </div>
          {sourceStatus?.message && (
            <div className="mb-2">
              <TextTip
                color={
                  sourceStatus.available && !sourceStatus.partial
                    ? 'blue'
                    : 'orange'
                }
                inline={false}
              >
                {sourceStatus.message}
              </TextTip>
            </div>
          )}
          {sourceStatus &&
            (!sourceStatus.available || sourceStatus.partial) &&
            !sourceStatus.message && (
              <div className="mb-2">
                <TextTip color="orange" inline={false}>
                  Some log data is unavailable.
                </TextTip>
              </div>
            )}
          {!!queryError && (
            <div className="mb-2">
              <TextTip color="red" inline={false}>
                {errorMessage(queryError, 'Failed to load logs')}
              </TextTip>
            </div>
          )}
          {!!exportError && (
            <div className="mb-2">
              <TextTip color="red" inline={false}>
                {errorMessage(exportError, 'Failed to export logs')}
              </TextTip>
            </div>
          )}
          <div className="mt-4">
            <Button
              color="primary"
              icon={DownloadIcon}
              onClick={onExport}
              disabled={!!disabledReason || isExporting}
              title={disabledReason}
              className="!ml-0"
              data-cy={exportDataCy}
            >
              {isExporting ? 'Exporting...' : 'Export as CSV'}
            </Button>
            {disabledReason && (
              <div className="small text-muted">{disabledReason}</div>
            )}
          </div>
        </form>
      </Widget.Body>
    </Widget>
  );
}

export function LogTextFilter({
  id,
  label,
  value,
  onChange,
  placeholder,
  numeric,
}: {
  id: string;
  label: string;
  value: string;
  onChange(value: string): void;
  placeholder?: string;
  numeric?: boolean;
}) {
  return (
    <FormControl label={label} inputId={id}>
      <input
        id={id}
        className="form-control max-w-lg"
        type="text"
        inputMode={numeric ? 'numeric' : undefined}
        value={value}
        placeholder={placeholder}
        onChange={(event) => {
          const next = event.target.value;
          if (!numeric || next === '' || /^[1-9]\d*$/.test(next)) {
            onChange(next);
          }
        }}
      />
    </FormControl>
  );
}

export function LogSelectFilter({
  id,
  label,
  value,
  onChange,
  options,
}: {
  id: string;
  label: string;
  value: string;
  onChange(value: string): void;
  options: { value: string; label: string }[];
}) {
  return (
    <FormControl label={label} inputId={id}>
      <select
        id={id}
        className="form-control max-w-lg"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">All</option>
        {options.map((option) => (
          <option value={option.value} key={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </FormControl>
  );
}

export function LogMultiFilter({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: number[];
  onChange(value: number[]): void;
  options: { value: number; label: string }[];
}) {
  return (
    <FormControl label={label}>
      <div className="flex flex-wrap gap-4" role="group" aria-label={label}>
        {options.map((option) => {
          const checked = value.includes(option.value);
          return (
            <label className="font-normal" key={option.value}>
              <input
                type="checkbox"
                className="mr-1"
                checked={checked}
                disabled={checked && value.length === 1}
                onChange={() =>
                  onChange(
                    checked
                      ? value.filter((item) => item !== option.value)
                      : [...value, option.value]
                  )
                }
              />
              {option.label}
            </label>
          );
        })}
      </div>
      <div className="small text-muted">Select at least one.</div>
    </FormControl>
  );
}

function getExportDisabledReason(
  status: SourceStatus | undefined,
  checking: boolean,
  error: unknown
) {
  if (error) {
    return 'Export is unavailable because the logs could not be loaded.';
  }
  if (checking || !status) {
    return 'Checking log source availability before export.';
  }
  if (!status.available) {
    return 'Export is unavailable because the log source is unavailable.';
  }
  if (status.partial) {
    return 'Export is unavailable because the log data is incomplete.';
  }
  return undefined;
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}
