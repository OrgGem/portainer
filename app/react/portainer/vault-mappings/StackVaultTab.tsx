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
import { emptyVaultMapping, VaultMappingFields } from './VaultMappingFields';

export function StackVaultTab({ stack }: { stack: Stack }) {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: vaultQueryKey(stack.Id),
    queryFn: async () =>
      (await axios.get<StackVaultResponse>(`/stacks/${stack.Id}/vault`)).data,
    refetchOnWindowFocus: false,
  });
  const [draft, setDraft] = useState<VaultMapping>(emptyVaultMapping);
  const [service, setService] = useState('');
  const [exported, setExported] = useState<{
    Content: string;
    Version: number;
  }>();
  const mutation = useMutation({
    mutationFn: async ({
      Mappings,
      InjectOnDeploy,
    }: {
      Mappings: VaultMapping[];
      InjectOnDeploy?: boolean;
    }) => {
      if (!query.data) throw new Error('Reload mappings before saving');
      try {
        return (
          await axios.put<VaultConfiguration>(`/stacks/${stack.Id}/vault`, {
            Version: query.data.Configuration.Version,
            Mappings,
            ...(InjectOnDeploy === undefined ? {} : { InjectOnDeploy }),
          })
        ).data;
      } catch (error) {
        throw parseAxiosError(error, 'Unable to save Vault references');
      }
    },
    onSuccess: async (_, variables) => {
      if (variables.InjectOnDeploy === undefined) {
        setDraft(emptyVaultMapping);
      }
      setExported(undefined);
      notifySuccess(
        'Success',
        'Vault mappings saved. Deploy the stack to apply the changes.'
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
        Map an environment variable for a service to a Vault API path and key.
      </p>
      <p>
        Saving mappings does not change running containers. When deployment
        injection is enabled, Portainer reads Vault when the stack is deployed.
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
      {query.data.InjectionSupported === false && (
        <p role="alert">
          Deployment injection requires local Docker or a standard Docker Agent.
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <span>
          Vault injection at deploy:{' '}
          <strong>{config.InjectOnDeploy ? 'enabled' : 'disabled'}</strong>
        </span>
        {canEdit && (
          <Button
            data-cy="toggle-vault-injection"
            disabled={
              mutation.isLoading ||
              (query.data.InjectionSupported === false &&
                !config.InjectOnDeploy)
            }
            onClick={() =>
              mutation.mutate({
                Mappings: config.Mappings,
                InjectOnDeploy: !config.InjectOnDeploy,
              })
            }
          >
            {config.InjectOnDeploy
              ? 'Disable deployment injection'
              : 'Enable deployment injection'}
          </Button>
        )}
      </div>
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
                        mutation.mutate({
                          Mappings: config.Mappings.filter(
                            (item) => item.ID !== entry.ID
                          ),
                        })
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
          <VaultMappingFields
            mapping={draft}
            onChange={setDraft}
            services={services}
            idPrefix="vault"
          />
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
              onClick={() => setDraft(emptyVaultMapping)}
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
    mutation.mutate({ Mappings: [...others, draft] });
  }
}
