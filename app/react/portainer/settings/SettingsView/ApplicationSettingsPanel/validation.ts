import { SchemaOf, boolean, number, object, string } from 'yup';

import { isValidUrl } from '@@/form-components/validate-url';

import { Values } from './types';

export function validation(): SchemaOf<Values> {
  return object({
    edgeAgentCheckinInterval: number().required(),
    loginBannerEnabled: boolean().default(false),
    loginBanner: string()
      .default('')
      .when('loginBannerEnabled', {
        is: true,
        then: (schema) =>
          schema.required('Login banner is required when enabled'),
      }),
    snapshotInterval: string().required('Snapshot interval is required'),
    templatesUrl: string()
      .default('')
      .test(
        'valid-url',
        'Must be a valid URL',
        (value) => !value || isValidUrl(value)
      ),
  });
}
