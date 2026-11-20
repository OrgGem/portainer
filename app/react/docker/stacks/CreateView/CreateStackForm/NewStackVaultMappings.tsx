import { useEffect, useMemo, useState } from 'react';
import { useFormikContext } from 'formik';

import {
  emptyVaultMapping,
  VaultMappingFields,
} from '@/react/portainer/vault-mappings/VaultMappingFields';
import { VaultMapping } from '@/react/portainer/vault-mappings/types';

import { FormSection } from '@@/form-components/FormSection';
import { Button } from '@@/buttons';

import { FormValues } from './types';
import { composeServices, readComposeFile } from './composeServices';

export function NewStackVaultMappings() {
  const { values, errors, setFieldValue } = useFormikContext<FormValues>();
  const [uploaded, setUploaded] = useState<{ file: File; content: string }>();
  const file = values.upload.file;

  useEffect(() => {
    let active = true;
    if (file) {
      readComposeFile(file)
        .then((content) => {
          if (active) setUploaded({ file, content });
          return undefined;
        })
        .catch(() => {
          if (active) setUploaded({ file, content: '' });
        });
    }
    return () => {
      active = false;
    };
  }, [file]);

  const uploadedContent = uploaded?.file === file ? uploaded.content : '';
  const content =
    values.method === 'upload' ? uploadedContent : values.editor.fileContent;
  const services = useMemo(() => composeServices(content), [content]);
  const mappings = values.vaultMappings || [];

  return (
    <FormSection title="Vault mappings">
      <p>
        Portainer reads these references when the stack is deployed. Configure
        the Vault connection in the environment settings first.
      </p>
      {mappings.map((mapping, index) => (
        <div key={index} className="mb-4 space-y-3 rounded border p-3">
          <VaultMappingFields
            mapping={mapping}
            services={services}
            idPrefix={`new-stack-vault-${index}`}
            onChange={(updated) => changeMapping(index, updated)}
          />
          <Button
            color="dangerlight"
            data-cy={`remove-new-vault-mapping-${index}`}
            onClick={() =>
              setFieldValue(
                'vaultMappings',
                mappings.filter((_, i) => i !== index)
              )
            }
          >
            Remove mapping
          </Button>
        </div>
      ))}
      {typeof errors.vaultMappings === 'string' && (
        <p role="alert">{errors.vaultMappings}</p>
      )}
      <Button
        data-cy="add-new-vault-mapping"
        disabled={services.length === 0 || mappings.length >= 128}
        onClick={() =>
          setFieldValue('vaultMappings', [
            ...mappings,
            { ...emptyVaultMapping },
          ])
        }
      >
        Add Vault mapping
      </Button>
      {services.length === 0 && (
        <p>
          Enter or upload a Compose file with services before adding a mapping.
        </p>
      )}
    </FormSection>
  );

  function changeMapping(index: number, mapping: VaultMapping) {
    setFieldValue(
      'vaultMappings',
      mappings.map((entry, i) => (i === index ? mapping : entry))
    );
  }
}
