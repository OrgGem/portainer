import { VaultMapping } from './types';

export const emptyVaultMapping: VaultMapping = {
  ID: '',
  Service: '',
  Variable: '',
  Path: '',
  Key: '',
  Engine: 'kv-v2',
};

export function VaultMappingFields({
  mapping,
  onChange,
  services,
  idPrefix,
}: {
  mapping: VaultMapping;
  onChange: (mapping: VaultMapping) => void;
  services: string[];
  idPrefix: string;
}) {
  return (
    <div className="space-y-3">
      <label htmlFor={`${idPrefix}-service`}>Service</label>
      <select
        id={`${idPrefix}-service`}
        className="form-control"
        required
        value={mapping.Service}
        onChange={(event) =>
          onChange({ ...mapping, Service: event.target.value })
        }
      >
        <option value="">Select a service</option>
        {services.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
      </select>
      <label htmlFor={`${idPrefix}-variable`}>Environment variable</label>
      <input
        id={`${idPrefix}-variable`}
        className="form-control"
        required
        maxLength={128}
        pattern="[a-zA-Z_][a-zA-Z0-9_]*"
        placeholder="DB_PASSWORD"
        value={mapping.Variable}
        onChange={(event) =>
          onChange({ ...mapping, Variable: event.target.value })
        }
      />
      <label htmlFor={`${idPrefix}-engine`}>KV engine</label>
      <select
        id={`${idPrefix}-engine`}
        className="form-control"
        value={mapping.Engine}
        onChange={(event) =>
          onChange({
            ...mapping,
            Engine: event.target.value as VaultMapping['Engine'],
          })
        }
      >
        <option value="kv-v2">KV v2</option>
        <option value="kv-v1">KV v1</option>
      </select>
      <label htmlFor={`${idPrefix}-path`}>Vault API path</label>
      <input
        id={`${idPrefix}-path`}
        className="form-control"
        required
        maxLength={512}
        placeholder="kv/data/apps/backend"
        value={mapping.Path}
        onChange={(event) => onChange({ ...mapping, Path: event.target.value })}
      />
      <p>
        For KV v2, include /data/ after the mount. Enter the API path without
        the Vault URL.
      </p>
      <label htmlFor={`${idPrefix}-key`}>Vault key name</label>
      <input
        id={`${idPrefix}-key`}
        className="form-control"
        required
        maxLength={128}
        pattern="[a-zA-Z_][a-zA-Z0-9_]*"
        placeholder="password"
        value={mapping.Key}
        onChange={(event) => onChange({ ...mapping, Key: event.target.value })}
      />
    </div>
  );
}
