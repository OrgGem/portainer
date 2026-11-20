import { object, array, number, mixed, SchemaOf, bool } from 'yup';

import { accessControlFormValidation } from '@/react/portainer/access-control/AccessControlForm';
import { EnvironmentId } from '@/react/portainer/environments/types';
import { nameValidation } from '@/react/docker/stacks/common/NameField';
import { Stack } from '@/react/common/stacks/types';
import { VaultMapping } from '@/react/portainer/vault-mappings/types';

import { envVarValidation } from '@@/form-components/EnvironmentVariablesFieldset';

import { BaseFormValues, FormValues } from './types';
import { getEditorValidationSchema } from './EditorSection/validation';
import { getGitValidationSchema } from './GitSection/validation';
import { getTemplateValidationSchema } from './TemplateSection/validation';
import { getUploadValidationSchema } from './UploadSection/validation';
import { composeServices, readComposeFile } from './composeServices';

export function getValidationSchema({
  isAdmin,
  environmentId,
  stacks,
  containerNames = [],
}: {
  isAdmin: boolean;
  environmentId: EnvironmentId;
  stacks?: Array<Stack>;
  containerNames?: Array<string>;
}): SchemaOf<FormValues> {
  return getBaseValidationSchema({ isAdmin, environmentId, stacks }).concat(
    object({
      git: getGitValidationSchema().when('method', {
        is: 'repository',
        then: (schema) => schema.required(),
        otherwise: () => mixed(),
      }),
      upload: getUploadValidationSchema({ containerNames }).when('method', {
        is: 'upload',
        then: (schema) => schema.required(),
        otherwise: () => mixed(),
      }),
      editor: getEditorValidationSchema({ containerNames }).when('method', {
        is: 'editor',
        then: (schema) => schema.required(),
        otherwise: () => mixed(),
      }),
      template: getTemplateValidationSchema({ containerNames }).when('method', {
        is: 'template',
        then: (schema) => schema.required(),
        otherwise: () => mixed(),
      }),
      vaultMappings: mixed<VaultMapping[]>()
        .test(
          'valid-vault-mappings',
          'Complete the Vault mappings before deploying',
          async function validateMappings(mappings) {
            const values = this.parent as FormValues;
            if (
              !mappings?.length ||
              (values.method !== 'editor' && values.method !== 'upload')
            ) {
              return true;
            }
            let content = values.editor?.fileContent || '';
            if (values.method === 'upload') {
              try {
                content = values.upload?.file
                  ? await readComposeFile(values.upload.file)
                  : '';
              } catch {
                content = '';
              }
            }
            const services = composeServices(content);
            const seen = new Set<string>();
            for (const mapping of mappings) {
              const service = mapping.Service || '';
              const variable = mapping.Variable || '';
              const key = mapping.Key || '';
              const path = mapping.Path || '';
              const identity = `${service}/${variable}`;
              if (!services.includes(service)) {
                return this.createError({
                  message:
                    'Select an existing Compose service for each Vault mapping.',
                });
              }
              if (
                !/^[a-zA-Z_][a-zA-Z0-9_]{0,127}$/.test(variable) ||
                !/^[a-zA-Z_][a-zA-Z0-9_]{0,127}$/.test(key)
              ) {
                return this.createError({
                  message:
                    'Each mapping needs a valid environment variable and Vault key.',
                });
              }
              if (
                !/^[a-zA-Z0-9_][a-zA-Z0-9_./-]{0,511}$/.test(path) ||
                path
                  .split('/')
                  .some(
                    (segment) =>
                      segment === '' || segment === '.' || segment === '..'
                  ) ||
                (mapping.Engine === 'kv-v2' && !path.includes('/data/'))
              ) {
                return this.createError({
                  message:
                    'Enter a valid Vault API path, including /data/ for KV v2.',
                });
              }
              if (seen.has(identity)) {
                return this.createError({
                  message:
                    'A service may map an environment variable only once.',
                });
              }
              seen.add(identity);
            }
            return true;
          }
        )
        .notRequired(),
    })
  );
}

function getBaseValidationSchema({
  isAdmin,
  environmentId,
  stacks,
}: {
  isAdmin: boolean;
  environmentId: EnvironmentId;
  stacks?: Array<Stack>;
}): SchemaOf<BaseFormValues> {
  return object({
    method: mixed<'editor' | 'upload' | 'repository' | 'template'>()
      .oneOf(['editor', 'repository', 'template', 'upload'])
      .default('editor'),
    name: nameValidation({ environmentId, stacks }).required(
      'Stack name is required'
    ),
    env: envVarValidation(),
    accessControl: accessControlFormValidation(isAdmin),
    enableWebhook: bool().default(false),
    registries: array(number().required()).default([]),
    vaultMappings: mixed<VaultMapping[]>().notRequired(),
  });
}
