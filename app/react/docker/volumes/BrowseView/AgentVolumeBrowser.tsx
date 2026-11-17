import { ComponentProps, useState } from 'react';

import { FilesTable } from '@/react/docker/components/FilesTable';
import { FileData } from '@/react/docker/components/FilesTable/types';
import { useEnvironment } from '@/react/portainer/environments/queries/useEnvironment';
import { EnvironmentType } from '@/react/portainer/environments/types';

import { createPersistedStore } from '@@/datatables/types';
import { useTableState } from '@@/datatables/useTableState';

import { VolumeTextEditor } from './VolumeTextEditor';

const tableKey = 'host-browser';

const settingsStore = createPersistedStore(tableKey, 'Name');

interface Props extends Omit<
  ComponentProps<typeof FilesTable>,
  'onSearchChange' | 'tableState' | 'title' | 'onEditText'
> {
  relativePath: string;
  endpointId: number;
  volumeId: string;
  nodeName?: string;
  onFileSaved(): void;
}

export function AgentVolumeBrowser({
  relativePath,
  dataset,
  isRoot,
  onBrowse,
  onDelete,
  onDownload,
  onFileSelectedForUpload,
  onGoToParent,
  onRename,
  isUploadAllowed,
  endpointId,
  volumeId,
  nodeName,
  onFileSaved,
}: Props) {
  const tableState = useTableState(settingsStore, tableKey);
  const environmentQuery = useEnvironment(endpointId);
  const canEditText =
    isUploadAllowed &&
    environmentQuery.data?.Type === EnvironmentType.AgentOnDocker;
  const [editing, setEditing] = useState<FileData | null>(null);
  const filePath = editing
    ? `${relativePath === '/' ? '' : relativePath}/${editing.Name}`
    : '';

  return (
    <>
      <FilesTable
        tableState={tableState}
        dataset={dataset}
        title={`Volume browser - ${relativePath}`}
        isRoot={isRoot}
        onRename={onRename}
        onBrowse={onBrowse}
        onDownload={onDownload}
        onDelete={onDelete}
        isUploadAllowed={isUploadAllowed}
        onFileSelectedForUpload={onFileSelectedForUpload}
        onGoToParent={onGoToParent}
        onEditText={canEditText ? setEditing : undefined}
      />
      {editing && (
        <VolumeTextEditor
          key={`${endpointId}:${nodeName}:${volumeId}:${filePath}`}
          target={{ endpointId, volumeId, nodeName, path: filePath }}
          onClose={() => setEditing(null)}
          onSaved={onFileSaved}
        />
      )}
    </>
  );
}
