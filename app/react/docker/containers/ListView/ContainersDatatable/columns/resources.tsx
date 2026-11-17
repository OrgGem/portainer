import { CellContext } from '@tanstack/react-table';

import { ContainerListViewModel } from '@/react/docker/containers/types';
import { ResourceUsagePanel } from '@/react/docker/components/ResourceUsage/ResourceUsage';

import { useRowContext } from '../RowContext';

import { columnHelper } from './helper';

export const resources = columnHelper.display({
  id: 'resources',
  header: 'CPU / RAM / Storage',
  cell: ResourcesCell,
});

function ResourcesCell({
  row: { original },
}: CellContext<ContainerListViewModel, unknown>) {
  const { environment } = useRowContext();
  if (original.State !== 'running') return null;
  return (
    <ResourceUsagePanel
      key={original.Id}
      environment={environment}
      containerId={original.Id}
      nodeName={original.NodeName}
    />
  );
}
