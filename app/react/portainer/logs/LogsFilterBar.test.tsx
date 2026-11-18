import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { LogsFilterBar } from './components/LogsFilterBar';

vi.mock('./components/DateRangePicker', () => ({
  DateRangePicker: () => null,
}));

function renderFilterBar(overrides: Partial<Parameters<typeof LogsFilterBar>[0]> = {}) {
  return render(
    <LogsFilterBar
      onRangeChange={vi.fn()}
      username=""
      onUsernameChange={vi.fn()}
      description="Log source details"
      isCheckingSource={false}
      isExporting={false}
      onExport={vi.fn()}
      exportDataCy="logs-export"
      {...overrides}
    />
  );
}

describe('LogsFilterBar', () => {
  it('blocks export while source data is partial and displays the source warning', () => {
    renderFilterBar({
      sourceStatus: {
        available: true,
        partial: true,
        message: 'Some records could not be read.',
      },
    });

    expect(screen.getByText('Some records could not be read.')).toBeInTheDocument();
    expect(screen.getByTestId('logs-export')).toHaveProperty('disabled', true);
    expect(screen.getByText('Export is unavailable because the log data is incomplete.')).toBeInTheDocument();
  });

  it('shows the actual export failure message', () => {
    renderFilterBar({ exportError: new Error('CSV generation was interrupted.') });

    expect(screen.getByText('CSV generation was interrupted.')).toBeInTheDocument();
  });

  it('offers all available dates only when the page enables it', () => {
    const { rerender } = renderFilterBar();
    expect(screen.queryByRole('button', { name: 'All available dates' })).not.toBeInTheDocument();

    rerender(
      <LogsFilterBar
        onRangeChange={vi.fn()}
        username=""
        onUsernameChange={vi.fn()}
        allowAllDates
        description="Log source details"
        isCheckingSource={false}
        isExporting={false}
        onExport={vi.fn()}
        exportDataCy="logs-export"
      />
    );
    expect(screen.getByRole('button', { name: 'All available dates' })).toBeInTheDocument();
  });
});
