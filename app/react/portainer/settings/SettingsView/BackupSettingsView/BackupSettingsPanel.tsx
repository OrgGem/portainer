import { Download } from 'lucide-react';
import { useState } from 'react';

import { isBE } from '@/react/portainer/feature-flags/feature-flags.service';

import { Widget, WidgetBody, WidgetTitle } from '@@/Widget';
import { FormSection } from '@@/form-components/FormSection';
import { BoxSelector } from '@@/BoxSelector';

import { BackupFormType, options } from './backup-options';
import { BackupFileForm } from './BackupFileForm';
import { BackupS3Form } from './BackupS3Form';
import { BackupS3IAMForm } from './BackupS3IAMForm';

export function BackupSettingsPanel() {
  const [backupType, setBackupType] = useState(options[0].value);

  return (
    <Widget>
      <WidgetTitle icon={Download} title="Back up Portainer" />
      <WidgetBody>
        <div className="form-horizontal">
          <FormSection title="Backup configuration">
            <div className="form-group col-sm-12 text-muted small">
              This will back up your Portainer server configuration and does not
              include containers.
            </div>
            <BoxSelector
              slim
              options={options}
              value={backupType}
              onChange={(v) => setBackupType(v)}
              radioName="backup-type"
            />

            {backupType === BackupFormType.S3 && isBE ? (
              <BackupS3Form />
            ) : backupType === BackupFormType.S3 ? (
              <BackupS3IAMForm />
            ) : (
              <BackupFileForm />
            )}
          </FormSection>
        </div>
      </WidgetBody>
    </Widget>
  );
}
