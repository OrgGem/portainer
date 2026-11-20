import axios from '@/portainer/services/axios/axios';

import { createStandaloneStackFromFile } from './createStandaloneStackFromFile';

test('includes Vault mappings as JSON in a standalone upload request', async () => {
  const post = vi.spyOn(axios, 'post').mockResolvedValue({
    data: { Id: 12, Name: 'uploaded-stack' },
  });
  const file = new File(['services: {}'], 'compose.yaml', {
    type: 'application/yaml',
  });

  try {
    await createStandaloneStackFromFile({
      environmentId: 7,
      Name: 'uploaded-stack',
      file,
      VaultMappings: {
        InjectOnDeploy: true,
        Mappings: [
          {
            ID: '',
            Service: 'backend',
            Variable: 'DB_PASSWORD',
            Path: 'kv/data/apps/backend',
            Key: 'password',
            Engine: 'kv-v2',
          },
        ],
      },
    });

    const [, formData, options] = post.mock.calls[0];
    expect(formData).toBeInstanceOf(FormData);
    expect((formData as FormData).get('VaultMappings')).toBe(
      JSON.stringify({
        InjectOnDeploy: true,
        Mappings: [
          {
            ID: '',
            Service: 'backend',
            Variable: 'DB_PASSWORD',
            Path: 'kv/data/apps/backend',
            Key: 'password',
            Engine: 'kv-v2',
          },
        ],
      })
    );
    expect((formData as FormData).get('file')).toBe(file);
    expect(options).toMatchObject({
      headers: { 'Content-Type': 'multipart/form-data' },
      params: { endpointId: 7 },
    });
  } finally {
    post.mockRestore();
  }
});
