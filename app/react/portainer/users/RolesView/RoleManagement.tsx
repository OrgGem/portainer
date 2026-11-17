import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import axios from '@/portainer/services/axios/axios';

import { Button } from '@@/buttons';
import { Widget, WidgetBody } from '@@/Widget';

import { RbacRole } from './types';

export interface RolePolicy {
  Enabled: boolean;
  Features: Record<string, boolean>;
  Version: number;
}
interface ConfiguredRole {
  Role: RbacRole;
  Key: string;
  Policy: RolePolicy;
  Defaults: RolePolicy;
}
interface Configuration {
  Roles: ConfiguredRole[];
  Features: { ID: string; Name: string }[];
}

export function RoleManagement() {
  const client = useQueryClient();
  const query = useQuery(
    ['role-configuration'],
    async () => (await axios.get<Configuration>('/roles/configuration')).data
  );
  const [selected, setSelected] = useState<number>();
  const [draft, setDraft] = useState<RolePolicy>();
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');
  const current = query.data?.Roles.find((r) => r.Role.Id === selected);
  const policy = draft || current?.Policy;
  return (
    <Widget>
      <WidgetBody>
        <h3>Role management</h3>
        <p>
          Configure the five Docker Standalone roles. Changes apply to explicit
          user and team assignments in every environment and environment group.
          Global administrators and legacy CE grants are unaffected.
        </p>
        <p>
          Feature switches restrict each role’s built-in capabilities.
          Unsupported features remain disabled; select a different role when a
          broader capability is needed. Resource ownership, stack policies and
          environment security settings still apply.
        </p>
        {query.isLoading && <p>Loading saved roles…</p>}
        {query.isError && (
          <p role="alert">
            Unable to load roles.{' '}
            <Button data-cy="reload-roles" onClick={() => query.refetch()}>
              Retry
            </Button>
          </p>
        )}
        <div className="flex flex-wrap gap-2 pb-4">
          {query.data?.Roles.map((r) => (
            <Button
              key={r.Key}
              data-cy={`select-role-${r.Key}`}
              disabled={saving}
              color={selected === r.Role.Id ? 'primary' : 'default'}
              onClick={() => {
                setSelected(r.Role.Id);
                setDraft(undefined);
                setMessage('');
              }}
            >
              {r.Role.Name}
              {!r.Policy.Enabled && ' (disabled)'}
            </Button>
          ))}
        </div>
        {!current && (
          <p>Select a role to view and edit its saved permissions.</p>
        )}
        {current && policy && (
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              await save();
            }}
          >
            <h4>{current.Role.Name}</h4>
            <p>
              {current.Role.Description} · Policy version {policy.Version}
            </p>
            <label className="flex items-center gap-2 pb-4">
              <input
                type="checkbox"
                checked={policy.Enabled}
                disabled={saving}
                onChange={(e) =>
                  setDraft({ ...policy, Enabled: e.target.checked })
                }
              />
              Role enabled
            </label>
            <div className="grid grid-cols-1 gap-3 pb-4 md:grid-cols-2">
              {query.data?.Features.map((f) => (
                <label
                  key={f.ID}
                  className="flex items-center gap-2"
                  title={f.ID}
                >
                  <input
                    type="checkbox"
                    aria-label={f.Name}
                    checked={!!policy.Features[f.ID]}
                    disabled={
                      saving ||
                      !policy.Enabled ||
                      !current.Defaults.Features[f.ID]
                    }
                    onChange={(e) =>
                      setDraft({
                        ...policy,
                        Features: {
                          ...policy.Features,
                          [f.ID]: e.target.checked,
                        },
                      })
                    }
                  />
                  {f.Name}
                  {!current.Defaults.Features[f.ID] && ' (outside role scope)'}
                </label>
              ))}
            </div>
            <p className="text-muted">
              Write operations also require the corresponding View permission.
              Dashboard counts require all resource View permissions. Turning
              off a role denies new requests; existing console sessions are not
              disconnected.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button
                type="submit"
                data-cy="save-role-policy"
                disabled={saving || !draft}
              >
                {saving ? 'Saving…' : 'Save role'}
              </Button>
              <Button
                data-cy="reset-role-policy"
                disabled={saving}
                onClick={() =>
                  setDraft({
                    ...current.Defaults,
                    Features: { ...current.Defaults.Features },
                    Version: policy.Version,
                  })
                }
              >
                Restore defaults
              </Button>
              <Button
                data-cy="reload-role-policy"
                disabled={saving}
                onClick={() => {
                  setDraft(undefined);
                  setMessage('');
                  query.refetch();
                }}
              >
                Reload saved
              </Button>
            </div>
          </form>
        )}
        {message && (
          <p role="status" className="pt-3">
            {message}
          </p>
        )}
      </WidgetBody>
    </Widget>
  );
  async function save() {
    if (!current || !draft) return;
    setSaving(true);
    setMessage('');
    try {
      await axios.put(`/roles/${current.Role.Id}/policy`, draft);
      await query.refetch();
      await client.invalidateQueries({
        predicate: (q) => q.queryKey.includes('effective-access'),
      });
      setDraft(undefined);
      setMessage(
        'Role saved. New API requests use the updated permissions. Refresh other open sessions to update their controls.'
      );
    } catch {
      setMessage(
        'Unable to save role. If another administrator changed it, reload saved settings before editing again.'
      );
    } finally {
      setSaving(false);
    }
  }
}
