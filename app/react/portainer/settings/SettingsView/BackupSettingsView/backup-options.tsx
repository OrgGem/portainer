import { DownloadCloud, UploadCloud } from 'lucide-react';

import { FeatureId } from '@/react/portainer/feature-flags/enums';
import { isBE } from '@/react/portainer/feature-flags/feature-flags.service';

import { BadgeIcon } from '@@/BadgeIcon';

export enum BackupFormType {
  S3 = 's3',
  File = 'file',
}

export const options = [
  {
    id: 'backup_file',
    icon: <BadgeIcon icon={DownloadCloud} />,
    label: 'Download backup file',
    value: BackupFormType.File,
  },
  {
    id: 'backup_s3',
    icon: <BadgeIcon icon={UploadCloud} />,
    label: 'Store in S3',
    description: isBE ? 'Define a cron schedule' : 'Schedule a daily backup',
    value: BackupFormType.S3,
    feature: isBE ? FeatureId.S3_BACKUP_SETTING : undefined,
  },
];
