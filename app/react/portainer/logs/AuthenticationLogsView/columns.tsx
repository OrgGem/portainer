import { createColumnHelper } from '@tanstack/react-table';
import { Check, X } from 'lucide-react';

import { isoDateFromTimestamp } from '@/portainer/filters/filters';

import { Icon } from '@@/Icon';

import { ActivityType, AuthLog, AuthMethodType } from './types';

const activityTypes = {
  [ActivityType.AuthSuccess]: {
    label: 'Authentication success',
    icon: Check,
    mode: 'success',
  },
  [ActivityType.AuthFailure]: {
    label: 'Authentication failure',
    icon: X,
    mode: 'danger',
  },
  [ActivityType.Logout]: { label: 'Logout', icon: undefined, mode: undefined },
} as const;

const columnHelper = createColumnHelper<AuthLog>();

export const columns = [
  columnHelper.accessor('timestamp', {
    header: 'Time',
    cell: ({ getValue }) =>
      getValue() ? isoDateFromTimestamp(getValue()) : 'Unknown',
  }),
  columnHelper.accessor('origin', {
    header: 'Origin',
    cell: ({ getValue }) => getValue() || 'Unknown',
  }),
  columnHelper.accessor('context', {
    header: 'Authentication method',
    cell: ({ getValue }) => AuthMethodType[getValue()] || 'Unknown',
  }),
  columnHelper.accessor('username', {
    header: 'User',
    cell: ({ getValue }) => getValue() || 'Unknown',
  }),
  columnHelper.accessor('type', {
    header: 'Result',
    cell: ({ getValue, row }) => {
      const props = activityTypes[getValue()];
      if (!props) {
        return 'Unknown';
      }
      return (
        <span className="flex items-center gap-1">
          {props.label}
          {row.original.rateLimited && (
            <span>
              {' '}
              (rate-limited sample; {row.original.suppressedRateLimited ||
                0}{' '}
              suppressed since previous sample)
            </span>
          )}
          {props.icon && props.mode && (
            <Icon icon={props.icon} mode={props.mode} />
          )}
        </span>
      );
    },
  }),
  columnHelper.accessor('statusCode', {
    header: 'HTTP status',
    enableSorting: false,
    cell: ({ getValue }) => getValue() || 'Unknown',
  }),
];
