import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import axios from '@/portainer/services/axios/axios';
import { notifyError } from '@/portainer/services/notifications';
import { Stack, StackStatus } from '@/react/common/stacks/types';
import { useIsPureAdmin } from '@/react/hooks/useUser';

import { Button } from '@@/buttons';

import { ConfigRevision, ConfigTarget, Controls } from './types';
import { useControlMutation, useStackControls } from './queries';
import { PolicyEditor } from './PolicyEditor';

export function ConfigurationTab({ stack }: { stack: Stack }) {
  const query = useStackControls(stack);
  const admin = useIsPureAdmin();
  if (query.isLoading) return <p>Loading stack controls...</p>;
  if (query.isError || !query.data)
    return (
      <p role="alert">
        Unable to load stack controls. Check your access and refresh.
      </p>
    );
  const controls = query.data;
  return (
    <div className="space-y-8">
      {admin && (
        <PolicyEditor
          key={controls.Policy?.Version || 0}
          stack={stack}
          policy={controls.Policy}
        />
      )}
      {!controls.Policy && (
        <p>
          An administrator must enable an operation policy before application
          configuration can be managed.
        </p>
      )}
      {controls.ConfigurationUnavailable ? (
        <p role="alert">{controls.ConfigurationUnavailable}</p>
      ) : (
        controls.Policy && (
          <ConfigurationEditor stack={stack} controls={controls} />
        )
      )}
      <div>
        <h4>Application secrets with Vault</h4>
        <p>
          Keep ordinary variables such as DB_HOST in the stack. Run Vault Agent
          as the application process supervisor to resolve secret references
          into variables such as DB_PASSWORD at runtime. The application needs
          its own Vault identity. Do not upload secret values as configuration
          files.
        </p>
        <a
          href="https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor"
          target="_blank"
          rel="noreferrer"
        >
          Vault Agent process supervisor documentation
        </a>
      </div>
    </div>
  );
}

function ConfigurationEditor({
  stack,
  controls,
}: {
  stack: Stack;
  controls: Controls;
}) {
  const admin = useIsPureAdmin();
  const mutation = useControlMutation(stack);
  const [targetID, setTargetID] = useState('');
  const [file, setFile] = useState<File>();
  const [selected, setSelected] = useState<ConfigRevision>();
  const config = controls.Configuration;
  const targets = config?.Targets || [];
  const pending = !!config?.PendingRevision;
  const busy =
    mutation.isLoading || stack.Status === StackStatus.Deploying || pending;
  const content = useQuery({
    queryKey: ['stack-config-content', stack.Id, selected?.ID],
    queryFn: async () =>
      (
        await axios.get<{ Content: string }>(
          `/stacks/${stack.Id}/configurations/${selected?.ID}/content`
        )
      ).data.Content,
    enabled: !!selected,
    cacheTime: 0,
  });
  const selectedTarget = targets.find(
    (target) => target.ID === selected?.TargetID
  );
  const canApply =
    !!selectedTarget &&
    controls.Services.includes(selectedTarget.Service) &&
    controls.Permissions.includes('config.apply') &&
    !busy &&
    stack.Status === StackStatus.Active &&
    content.isSuccess;

  return (
    <div className="space-y-6">
      <h4>Application configuration</h4>
      {pending && (
        <div role="alert">
          <p>
            A configuration deployment was interrupted. Recovery restores the
            last successful configuration.
          </p>
          {admin && (
            <Button
              data-cy="recover-stack-config"
              disabled={mutation.isLoading}
              onClick={() =>
                mutation.mutate({ path: 'configurations/recover', data: {} })
              }
            >
              Recover previous configuration
            </Button>
          )}
        </div>
      )}
      <table className="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Service</th>
            <th>Read-only target</th>
            <th>Active revision</th>
            {admin && <th>Actions</th>}
          </tr>
        </thead>
        <tbody>
          {targets.map((target) => (
            <tr key={target.ID}>
              <td>{target.ID}</td>
              <td>{target.Service}</td>
              <td>
                <code>{target.Target}</code>
              </td>
              <td>{target.ActiveRevision || 'None'}</td>
              {admin && (
                <td>
                  {!controls.Services.includes(target.Service) && (
                    <Button
                      color="secondary"
                      disabled={busy}
                      data-cy={`retire-config-target-${target.ID}`}
                      onClick={() =>
                        mutation.mutate({
                          path: 'configurations/targets',
                          method: 'put',
                          data: {
                            Version: config?.Version,
                            Targets: targets.filter(
                              (item) => item.ID !== target.ID
                            ),
                          },
                        })
                      }
                    >
                      Retire missing service target
                    </Button>
                  )}
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {admin && (
        <TargetForm
          key={config?.Version || 0}
          stack={stack}
          controls={controls}
          disabled={busy}
        />
      )}
      {controls.Permissions.includes('config.upload') && (
        <form className="space-y-3" onSubmit={upload}>
          <label htmlFor="config-target">Declared service and path</label>
          <select
            id="config-target"
            className="form-control"
            value={targetID}
            required
            onChange={(event) => setTargetID(event.target.value)}
          >
            <option value="">Select target</option>
            {targets.map((target) => (
              <option key={target.ID} value={target.ID}>
                {target.Service}: {target.Target}
              </option>
            ))}
          </select>
          <label htmlFor="config-file">
            Configuration file (UTF-8, maximum 1 MiB)
          </label>
          <input
            id="config-file"
            type="file"
            accept=".yaml,.yml,.json,.conf"
            required
            onChange={(event) => setFile(event.target.files?.[0])}
          />
          <Button
            type="submit"
            data-cy="upload-stack-config"
            disabled={busy || !file || !targetID}
          >
            Upload draft
          </Button>
          <p>
            Uploading does not change the running application. Review the
            uploaded contents before applying.
          </p>
        </form>
      )}
      <h4>Version history</h4>
      <table className="table">
        <thead>
          <tr>
            <th>File / target</th>
            <th>Uploaded</th>
            <th>Status</th>
            <th>Review</th>
          </tr>
        </thead>
        <tbody>
          {[...(config?.Revisions || [])].reverse().map((revision) => (
            <tr key={revision.ID}>
              <td>
                {revision.Filename}
                <br />
                {revision.TargetID}
                <br />
                <small>{revision.ID}</small>
              </td>
              <td>
                {new Date(revision.CreatedAt * 1000).toLocaleString()}
                <br />
                User #{revision.CreatedBy}
              </td>
              <td>
                {targets.some((target) => target.ActiveRevision === revision.ID)
                  ? 'Active'
                  : revision.AppliedAt
                    ? 'Previously applied'
                    : 'Draft'}
              </td>
              <td>
                <Button
                  data-cy={`review-config-${revision.ID}`}
                  onClick={() => setSelected(revision)}
                >
                  Review
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {selected && (
        <div className="space-y-3">
          <h4>Review {selected.Filename}</h4>
          <p>
            Service: <strong>{selectedTarget?.Service}</strong> — path:{' '}
            <code>{selectedTarget?.Target}</code>
          </p>
          <p className="break-all">SHA-256: {selected.SHA256}</p>
          {content.isLoading && <p>Loading revision...</p>}
          {content.isError && <p role="alert">Unable to load this revision.</p>}
          {content.isSuccess && (
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap">
              {content.data}
            </pre>
          )}
          {controls.Permissions.includes('config.apply') && (
            <Button
              data-cy="apply-stack-config"
              disabled={
                !canApply || selectedTarget?.ActiveRevision === selected.ID
              }
              onClick={() =>
                mutation.mutate({
                  path: `configurations/${selected.ID}/${selected.AppliedAt ? 'rollback' : 'apply'}`,
                  data: { Version: config?.Version },
                })
              }
            >
              {selected.AppliedAt
                ? 'Roll back to this version'
                : 'Apply this version'}
            </Button>
          )}
          <p>
            Applying recreates this service with its current image and waits for
            it to be running or healthy.
          </p>
        </div>
      )}
      {!!config?.Events?.length && (
        <details>
          <summary>Configuration activity</summary>
          <ul>
            {[...config.Events].reverse().map((event, index) => (
              <li key={`${event.Time}-${index}`}>
                {new Date(event.Time * 1000).toLocaleString()} — user #
                {event.UserID}: {event.Action} {event.RevisionID} (
                {event.Success ? 'successful' : 'failed'})
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );

  async function upload(event: React.FormEvent) {
    event.preventDefault();
    if (!file || !targetID) return;
    if (file.size > 1024 * 1024) {
      notifyError('Upload failed', new Error('Maximum file size is 1 MiB'));
      return;
    }
    try {
      const text = await file.text();
      mutation.mutate({
        path: 'configurations',
        data: {
          TargetID: targetID,
          Filename: file.name,
          Content: text,
          Version: config?.Version || 0,
        },
      });
    } catch (error) {
      notifyError('Unable to read file', error as Error);
    }
  }
}

function TargetForm({
  stack,
  controls,
  disabled,
}: {
  stack: Stack;
  controls: Controls;
  disabled: boolean;
}) {
  const [target, setTarget] = useState<ConfigTarget>({
    ID: '',
    Service: '',
    Target: '',
  });
  const mutation = useControlMutation(stack);
  return (
    <form
      className="space-y-3"
      onSubmit={(event) => {
        event.preventDefault();
        mutation.mutate({
          path: 'configurations/targets',
          method: 'put',
          data: {
            Version: controls.Configuration?.Version || 0,
            Targets: [...(controls.Configuration?.Targets || []), target],
          },
        });
      }}
    >
      <h5>Declare an application configuration target</h5>
      <label htmlFor="target-name">Name</label>
      <input
        id="target-name"
        className="form-control"
        required
        pattern="[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}"
        value={target.ID}
        onChange={(event) => setTarget({ ...target, ID: event.target.value })}
      />
      <label htmlFor="target-service">Service</label>
      <select
        id="target-service"
        className="form-control"
        required
        value={target.Service}
        onChange={(event) =>
          setTarget({ ...target, Service: event.target.value })
        }
      >
        <option value="">Select service</option>
        {controls.Services.map((service) => (
          <option key={service} value={service}>
            {service}
          </option>
        ))}
      </select>
      <label htmlFor="target-path">
        Absolute file path inside the container
      </label>
      <input
        id="target-path"
        className="form-control"
        placeholder="/etc/myapp/application.yaml"
        required
        value={target.Target}
        onChange={(event) =>
          setTarget({ ...target, Target: event.target.value })
        }
      />
      <Button
        type="submit"
        data-cy="add-config-target"
        disabled={disabled || mutation.isLoading}
      >
        Declare target
      </Button>
    </form>
  );
}
