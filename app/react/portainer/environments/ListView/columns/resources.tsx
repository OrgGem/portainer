import { ResourceUsagePanel } from '@/react/docker/components/ResourceUsage/ResourceUsage';

import { columnHelper } from './helper';

export const resources = columnHelper.display({
  id: 'resources',
  header: 'CPU / RAM / Storage',
  cell: ({ row: { original } }) => (
    <ResourceUsagePanel key={original.Id} environment={original} />
  ),
});
