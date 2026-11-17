import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { saveAs } from 'file-saver';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { notifyError, notifySuccess } from '@/portainer/services/notifications';
import { Stack } from '@/react/common/stacks/types';

import { Button } from '@@/buttons';
import { Link } from '@@/Link';

import {
  inventoryQueryKey,
  StackVaultResponse,
  VaultConfiguration,
  VaultMapping,
  vaultQueryKey,
} from './types';

const emptyMapping: VaultMapping = {
  ID: '',
  Service: '',
  Variable: '',
  Path: '',
  Key: '',
  Engine: 'kv-v2',
};

export function StackVaultTab({ stack }: { stack: Stack }) {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: vaultQueryKey(stack.Id),
    queryFn: async () =>
      (await axios.get<StackVaultResponse>(`/stacks/${stack.Id}/vault`)).data,
    refetchOnWindowFocus: false,
  });
  const [draft, setDraft] = useState<VaultMapping>(emptyMapping);
  const [service, setService] = useState('');
  const [exported, setExported] = useState<{
    Content: string;
    Version: number;
  }>();
  const mutation = useMutation({
    mutationFn: async (Mappings: VaultMapping[]) => {
      if (!query.data) throw new Error('Reload mappings before saving');
      try {
        return (
          await axios.put<VaultConfiguration>(`/stacks/${stack.Id}/vault`, {
            Version: query.data.Configuration.Version,
            Mappings,
          })
        ).data;
      } catch (error) {
        throw parseAxiosError(error, 'Unable to save Vault references');
      }
    },
    onSuccess: async () => {
      setDraft(emptyMapping);
      setExported(undefined);
      notifySuccess(
        'Success',
        'Vault references saved. Runtime deployment is separate.'
      );
      await Promise.all([
        client.invalidateQueries(vaultQueryKey(stack.Id)),
        client.invalidateQueries(inventoryQueryKey),
      ]);
    },
    onError: (error) =>
      notifyError('Unable to save Vault mappings', error as Error),
  });
  const template = useMutation({
    mutationFn: async () =>
      (
        await axios.get<{ Content: string; Version: number }>(
          `/stacks/${stack.Id}/vault/template`,
          { params: { service } }
        )
      ).data,
    onSuccess: setExported,
    onError: (error) =>
      notifyError('Unable to export Agent template', error as Error),
  });

  if (query.isLoading) return <p>Loading Vault references...</p>;
  if (query.isError || !query.data)
    return (
      <p role="alert">
        Unable to load Vault mappings.{' '}
        <Button data-cy="retry-vault-load" onClick={() => query.refetch()}>
          Retry
        </Button>
      </p>
    );
  const {
    Configuration: config,
    Services: services,
    CanEdit: canEdit,
  } = query.data;
  return (
    <div className="space-y-5">
      <h3>Vault mappings</h3>
      <p>
        References only: environment variable → Vault API path → key. Portainer
        does not retrieve, store or display secret values or Vault credentials.
      </p>
      <p>
        <strong>Saved configuration, runtime not verified.</strong> Saving here
        does not change running containers. Export the service template, merge
        it into the application&apos;s Vault Agent configuration and deploy
        through your normal release process.
      </p>
      <div className="flex items-center gap-3">
        <span>
          Version {config.Version} · {config.Mappings.length} mappings
        </span>
        <Button
          data-cy="reload-vault-mappings"
          disabled={mutation.isLoading}
          onClick={() => query.refetch()}
        >
          Reload references
        </Button>
        <Link
          data-cy="vault-inventory-for-stack"
          to="portainer.vaultMappings"
          params={{ environmentId: stack.EndpointId, stackId: stack.Id }}
        >
          View inventory for this stack
        </Link>
      </div>
      {!!config.UpdatedAt && (
        <p>
          Last saved {new Date(config.UpdatedAt * 1000).toLocaleString()} by
          user #{config.UpdatedBy}.
        </p>
      )}
      {query.data.Unavailable && <p role="alert">{query.data.Unavailable}</p>}
      {mutation.isError && (
        <p role="alert">
          {(mutation.error as Error).message} Your form has been kept. Reload
          references after a version conflict, review the changes, then save
          again.
        </p>
      )}
      {!canEdit && (
        <p>
          You can view references. Global or environment administrators manage
          mappings.
        </p>
      )}
      <div className="overflow-x-auto">
        <table className="table">
          <thead>
            <tr>
              <th>Service</th>
              <th>Environment variable</th>
              <th>Vault API path</th>
              <th>Key</th>
              <th>Engine</th>
              {canEdit && <th>Actions</th>}
            </tr>
          </thead>
          <tbody>
            {config.Mappings.map((entry) => (
              <tr key={entry.ID}>
                <td>
                  {entry.Service}
                  {!services.includes(entry.Service) && (
                    <span className="text-warning">
                      {' '}
                      (not in current Compose)
                    </span>
                  )}
                </td>
                <td>
                  <code>{entry.Variable}</code>
                </td>
                <td>
                  <code>{entry.Path}</code>
                </td>
                <td>
                  <code>{entry.Key}</code>
                </td>
                <td>{entry.Engine}</td>
                {canEdit && (
                  <td>
                    <Button
                      data-cy={`edit-vault-${entry.ID}`}
                      disabled={mutation.isLoading}
                      onClick={() => setDraft(entry)}
                    >
                      Edit reference
                    </Button>
                    <Button
                      data-cy={`remove-vault-${entry.ID}`}
                      color="dangerlight"
                      disabled={mutation.isLoading}
                      onClick={() =>
                        mutation.mutate(
                          config.Mappings.filter((item) => item.ID !== entry.ID)
                        )
                      }
                    >
                      Remove reference
                    </Button>
                  </td>
                )}
              </tr>
            ))}
            {config.Mappings.length === 0 && (
              <tr>
                <td colSpan={canEdit ? 6 : 5}>
                  No Vault references configured.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {canEdit && (
        <form onSubmit={save} className="space-y-3">
          <h4>{draft.ID ? 'Edit mapping' : 'Add mapping'}</h4>
          <label htmlFor="vault-service">Service</label>
          <select
            id="vault-service"
            className="form-control"
            required
            value={draft.Service}
            onChange={(e) => setDraft({ ...draft, Service: e.target.value })}
          >
            <option value="">Select a service</option>
            {services.map((name) => (
              <option key={name}>{name}</option>
            ))}
          </select>
          <label htmlFor="vault-variable">Environment variable</label>
          <input
            id="vault-variable"
            className="form-control"
            required
            maxLength={128}
            pattern="[a-zA-Z_][a-zA-Z0-9_]*"
            placeholder="DB_PASSWORD"
            value={draft.Variable}
            onChange={(e) => setDraft({ ...draft, Variable: e.target.value })}
          />
          <label htmlFor="vault-engine">KV engine</label>
          <select
            id="vault-engine"
            className="form-control"
            value={draft.Engine}
            onChange={(e) =>
              setDraft({
                ...draft,
                Engine: e.target.value as VaultMapping['Engine'],
              })
            }
          >
            <option value="kv-v2">KV v2</option>
            <option value="kv-v1">KV v1</option>
          </select>
          <label htmlFor="vault-path">Vault API path</label>
          <input
            id="vault-path"
            className="form-control"
            required
            maxLength={512}
            placeholder="kv/data/apps/backend"
            value={draft.Path}
            onChange={(e) => setDraft({ ...draft, Path: e.target.value })}
          />
          <p>
            For KV v2, include /data/ after the mount. Use the API path, without
            a URL, token or secret value.
          </p>
          <label htmlFor="vault-key">Vault key name</label>
          <input
            id="vault-key"
            className="form-control"
            required
            maxLength={128}
            pattern="[a-zA-Z_][a-zA-Z0-9_]*"
            placeholder="password"
            value={draft.Key}
            onChange={(e) => setDraft({ ...draft, Key: e.target.value })}
          />
          <p>
            Key names support letters, digits and underscores, starting with a
            letter or underscore.
          </p>
          <Button
            type="submit"
            data-cy="save-vault-mapping"
            disabled={mutation.isLoading}
          >
            {draft.ID ? 'Save mapping' : 'Add mapping'}
          </Button>
          {draft.ID && (
            <Button
              data-cy="cancel-vault-edit"
              disabled={mutation.isLoading}
              onClick={() => setDraft(emptyMapping)}
            >
              Cancel edit
            </Button>
          )}
        </form>
      )}
      {config.Mappings.length > 0 && (
        <div className="space-y-3">
          <h4>Export Vault Agent references</h4>
          <label htmlFor="vault-export-service">Service to export</label>
          <select
            id="vault-export-service"
            className="form-control"
            value={service}
            onChange={(e) => {
              setService(e.target.value);
              setExported(undefined);
            }}
          >
            <option value="">Select a service</option>
            {services
              .filter((name) =>
                config.Mappings.some((entry) => entry.Service === name)
              )
              .map((name) => (
                <option key={name}>{name}</option>
              ))}
          </select>
          <Button
            data-cy="preview-vault-template"
            disabled={!service || template.isLoading}
            onClick={() => template.mutate()}
          >
            Preview Agent template
          </Button>
          {exported && (
            <>
              <p>
                Exported saved version {exported.Version}. This snippet contains
                references only; keep Agent authentication, Vault connection and
                application command in your existing agent.hcl.
              </p>
              <textarea
                aria-label="Agent template references"
                className="form-control font-mono"
                rows={10}
                readOnly
                value={exported.Content}
              />
              <Button
                data-cy="download-vault-template"
                onClick={() =>
                  saveAs(
                    new Blob([exported.Content], {
                      type: 'text/plain;charset=utf-8',
                    }),
                    `stack-${stack.Id}-vault-env-template.hcl`
                  )
                }
              >
                Download template
              </Button>
            </>
          )}
        </div>
      )}
    </div>
  );

  function save(event: FormEvent) {
    event.preventDefault();
    if (!query.data) return;
    const others = query.data.Configuration.Mappings.filter(
      (entry) => !draft.ID || entry.ID !== draft.ID
    );
    mutation.mutate([...others, draft]);
  }
}
