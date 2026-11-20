import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { KeyRound } from 'lucide-react';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { notifyError, notifySuccess } from '@/portainer/services/notifications';
import { EnvironmentId } from '@/react/portainer/environments/types';

import { Button } from '@@/buttons';
import { Widget } from '@@/Widget';

interface VaultConnection {
  Address: string;
  AuthMount: string;
  RoleID: string;
  CAPEM: string;
  HasSecretID: boolean;
}

interface VaultConnectionInput {
  Address: string;
  AuthMount: string;
  RoleID: string;
  SecretID: string;
  CAPEM: string;
}

export function VaultConnectionPanel({
  environmentId,
}: {
  environmentId: EnvironmentId;
}) {
  const query = useQuery({
    queryKey: ['environment', environmentId, 'vault-connection'],
    queryFn: async () =>
      (await axios.get<VaultConnection>(`/endpoints/${environmentId}/vault`))
        .data,
    refetchOnWindowFocus: false,
  });

  return (
    <Widget>
      <Widget.Title icon={KeyRound} title="HashiCorp Vault" />
      <Widget.Body>
        {query.isLoading && <p>Loading Vault connection...</p>}
        {query.isError && (
          <p role="alert">
            Unable to load the Vault connection.{' '}
            <Button
              data-cy="vault-connection-retry"
              onClick={() => query.refetch()}
            >
              Retry
            </Button>
          </p>
        )}
        {query.data && (
          <VaultConnectionForm
            environmentId={environmentId}
            connection={query.data}
          />
        )}
      </Widget.Body>
    </Widget>
  );
}

function VaultConnectionForm({
  environmentId,
  connection,
}: {
  environmentId: EnvironmentId;
  connection: VaultConnection;
}) {
  const client = useQueryClient();
  const [values, setValues] = useState<VaultConnectionInput>({
    Address: connection.Address || '',
    AuthMount: connection.AuthMount || 'approle',
    RoleID: connection.RoleID || '',
    SecretID: '',
    CAPEM: connection.CAPEM || '',
  });
  const mutation = useMutation({
    mutationFn: async (payload: VaultConnectionInput) => {
      try {
        await axios.put(`/endpoints/${environmentId}/vault`, payload);
      } catch (error) {
        throw parseAxiosError(error, 'Unable to save Vault connection');
      }
    },
    onSuccess: async () => {
      setValues((current) => ({ ...current, SecretID: '' }));
      notifySuccess(
        'Vault connection saved',
        'The connection applies to this environment.'
      );
      await client.invalidateQueries([
        'environment',
        environmentId,
        'vault-connection',
      ]);
      mutation.reset();
    },
    onError: (error) =>
      notifyError('Unable to save Vault connection', error as Error),
  });

  const needsSecret =
    !connection.HasSecretID || values.Address !== connection.Address;

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    mutation.mutate(values);
  }

  return (
    <form onSubmit={save} className="space-y-3">
      <p>
        Portainer connects to Vault when a stack is deployed in this
        environment. The configured AppRole must be able to read the mapped
        paths.
      </p>
      <label htmlFor="vault-connection-address">Vault URL</label>
      <input
        id="vault-connection-address"
        className="form-control"
        type="url"
        required
        placeholder="https://vault.example.com:8200"
        value={values.Address}
        onChange={(event) =>
          setValues({ ...values, Address: event.target.value })
        }
      />
      <label htmlFor="vault-connection-auth-mount">AppRole auth mount</label>
      <input
        id="vault-connection-auth-mount"
        className="form-control"
        required
        value={values.AuthMount}
        onChange={(event) =>
          setValues({ ...values, AuthMount: event.target.value })
        }
      />
      <label htmlFor="vault-connection-role-id">AppRole RoleID</label>
      <input
        id="vault-connection-role-id"
        className="form-control"
        required
        autoComplete="off"
        value={values.RoleID}
        onChange={(event) =>
          setValues({ ...values, RoleID: event.target.value })
        }
      />
      <label htmlFor="vault-connection-secret-id">AppRole SecretID</label>
      <input
        id="vault-connection-secret-id"
        className="form-control"
        type="password"
        autoComplete="new-password"
        required={needsSecret}
        placeholder={
          needsSecret ? 'Enter SecretID' : 'Configured — leave blank to keep'
        }
        value={values.SecretID}
        onChange={(event) =>
          setValues({ ...values, SecretID: event.target.value })
        }
      />
      <label htmlFor="vault-connection-ca">
        Additional CA certificate (PEM)
      </label>
      <textarea
        id="vault-connection-ca"
        className="form-control font-mono"
        rows={4}
        value={values.CAPEM}
        onChange={(event) =>
          setValues({ ...values, CAPEM: event.target.value })
        }
      />
      <Button
        type="submit"
        data-cy="save-vault-connection"
        disabled={mutation.isLoading}
      >
        Save Vault connection
      </Button>
    </form>
  );
}
