import { FormEvent, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { notifyError, notifySuccess } from '@/portainer/services/notifications';
import { User } from '@/portainer/users/types';
import { Team } from '@/react/portainer/users/teams/types';

import { Widget, WidgetBody } from '@@/Widget';
import { Button } from '@@/buttons';
import { Link } from '@@/Link';

import { ScopedAccessEntity, StandaloneRole, standaloneRoles } from './types';

export function StandaloneAccessManagement({
  entityId,
  scope,
}: {
  entityId: number;
  scope: 'endpoint' | 'group';
}) {
  const url = `/${scope === 'endpoint' ? 'endpoints' : 'endpoint_groups'}/${entityId}`;
  const client = useQueryClient();
  const entity = useQuery({
    queryKey: ['standalone-access', scope, entityId],
    queryFn: async () => (await axios.get<ScopedAccessEntity>(url)).data,
  });
  const principals = useQuery({
    queryKey: ['standalone-access-principals'],
    queryFn: async () => {
      const [users, teams] = await Promise.all([
        axios.get<User[]>('/users'),
        axios.get<Team[]>('/teams'),
      ]);
      return {
        users: users.data.filter((user) => user.Role !== 1),
        teams: teams.data,
      };
    },
  });
  const inherited = useQuery({
    queryKey: ['standalone-access', 'group', entity.data?.GroupId],
    queryFn: async () =>
      (
        await axios.get<ScopedAccessEntity>(
          `/endpoint_groups/${entity.data?.GroupId}`
        )
      ).data,
    enabled: scope === 'endpoint' && !!entity.data?.GroupId,
  });
  const [principal, setPrincipal] = useState('');
  const [role, setRole] = useState<StandaloneRole | 'legacy'>('read-only');
  const [drafts, setDrafts] = useState<
    Record<string, StandaloneRole | 'legacy'>
  >({});
  const mutation = useMutation({
    mutationFn: async ({
      key,
      value,
    }: {
      key: string;
      value?: StandaloneRole | 'legacy';
    }) => {
      if (!entity.data) throw new Error('Access data is unavailable');
      const [kind, id] = key.split(':');
      const payload = {
        UserAccessPolicies: { ...entity.data.UserAccessPolicies },
        TeamAccessPolicies: { ...entity.data.TeamAccessPolicies },
      };
      const policies =
        kind === 'team'
          ? payload.TeamAccessPolicies
          : payload.UserAccessPolicies;
      if (value === 'legacy') policies[Number(id)] = { RoleId: 1 };
      else if (value)
        policies[Number(id)] = {
          RoleId: standaloneRoles.find((item) => item.value === value)!.id,
          StandaloneRole: value,
        };
      else delete policies[Number(id)];
      try {
        await axios.put(url, payload);
      } catch (error) {
        throw parseAxiosError(error, 'Unable to save environment roles');
      }
    },
    onSuccess: () => {
      notifySuccess('Success', 'Environment access updated');
      setDrafts({});
      return client.invalidateQueries();
    },
    onError: (error) => notifyError('Unable to save roles', error as Error),
  });

  if (entity.isLoading || principals.isLoading)
    return <p>Loading group roles...</p>;
  if (!entity.data || !principals.data || entity.isError || principals.isError)
    return <p role="alert">Unable to load environment access.</p>;

  return (
    <Widget>
      <WidgetBody>
        <div className="space-y-6">
          <h3>Docker Standalone group roles</h3>
          <p>
            Teams are user groups. Assign a team once to apply its role to all
            current members. These roles apply to Docker Standalone
            environments; global administrators retain full control.
          </p>
          <p>
            Direct user access overrides inherited user access, then direct team
            access, then inherited team access. Within a team layer the priority
            is Read-only, Standard, Helpdesk, Operator, then Environment
            administrator. Backend changes take effect immediately; refresh
            existing browser sessions to update their controls.
          </p>
          <form onSubmit={assign} className="space-y-3">
            <label htmlFor="scoped-principal">Group or user</label>
            <select
              id="scoped-principal"
              className="form-control"
              value={principal}
              required
              onChange={(event) => setPrincipal(event.target.value)}
            >
              <option value="">Select a group or user</option>
              <optgroup label="Groups (teams)">
                {principals.data.teams.map((team) => (
                  <option key={team.Id} value={`team:${team.Id}`}>
                    {team.Name}
                  </option>
                ))}
              </optgroup>
              <optgroup label="Users">
                {principals.data.users.map((user) => (
                  <option key={user.Id} value={`user:${user.Id}`}>
                    {user.Username}
                  </option>
                ))}
              </optgroup>
            </select>
            <label htmlFor="scoped-role">Environment role</label>
            <select
              id="scoped-role"
              className="form-control"
              value={role}
              onChange={(event) =>
                setRole(event.target.value as StandaloneRole | 'legacy')
              }
            >
              {scope === 'group' && (
                <option value="legacy">
                  Legacy CE access (all environment types)
                </option>
              )}
              {standaloneRoles.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
            <p>
              {standaloneRoles.find((item) => item.value === role)?.description}
            </p>
            <Button
              type="submit"
              data-cy="assign-standalone-role"
              disabled={!principal || mutation.isLoading}
            >
              Assign role
            </Button>
          </form>
          <h4>Direct access</h4>
          <table className="table">
            <thead>
              <tr>
                <th>Group / user</th>
                <th>Role</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {rows(entity.data).map((entry) => (
                <tr key={entry.key}>
                  <td>{displayName(entry.key)}</td>
                  <td>
                    <select
                      className="form-control"
                      aria-label={`Role for ${displayName(entry.key)}`}
                      value={
                        drafts[entry.key] ||
                        entry.policy.StandaloneRole ||
                        'legacy'
                      }
                      onChange={(event) =>
                        setDrafts({
                          ...drafts,
                          [entry.key]: event.target.value as
                            | StandaloneRole
                            | 'legacy',
                        })
                      }
                    >
                      {scope !== 'group' && !entry.policy.StandaloneRole && (
                        <option value="legacy" disabled>
                          Standard user (legacy CE access)
                        </option>
                      )}
                      {scope === 'group' && (
                        <option value="legacy">
                          Legacy CE access (all environment types)
                        </option>
                      )}
                      {standaloneRoles.map((item) => (
                        <option key={item.value} value={item.value}>
                          {item.label}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <Button
                      data-cy={`save-role-${entry.key}`}
                      disabled={!drafts[entry.key] || mutation.isLoading}
                      onClick={() =>
                        mutation.mutate({
                          key: entry.key,
                          value: drafts[entry.key],
                        })
                      }
                    >
                      Save role
                    </Button>
                    <Button
                      color="dangerlight"
                      data-cy={`remove-role-${entry.key}`}
                      disabled={mutation.isLoading}
                      onClick={() => mutation.mutate({ key: entry.key })}
                    >
                      Remove access
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {scope === 'endpoint' && !!entity.data.GroupId && (
            <div>
              <h4>Access inherited from environment group</h4>
              {inherited.isError && (
                <p role="alert">Unable to load inherited access.</p>
              )}
              <ul>
                {(inherited.data ? rows(inherited.data) : []).map((entry) => (
                  <li key={entry.key}>
                    {displayName(entry.key)}:{' '}
                    {standaloneRoles.find(
                      (item) => item.value === entry.policy.StandaloneRole
                    )?.label || 'Standard user (legacy CE access)'}
                  </li>
                ))}
              </ul>
              <Link
                data-cy="manage-inherited-standalone-access"
                to="portainer.groups.group"
                params={{ id: entity.data.GroupId }}
              >
                Manage environment group access
              </Link>
            </div>
          )}
          <p>
            Read-only roles never gain write access from a stack grant. Stack
            operation policies further restrict other roles; Environment
            administrators can manage all stacks in their environment. Ownership
            and role assignment are managed by global administrators.
          </p>
          <p>
            Public Docker webhooks and scheduled Git updates are disabled in
            environments with explicit roles. Roles for Edge and Kubernetes are
            outside this Docker Standalone implementation.
          </p>
        </div>
      </WidgetBody>
    </Widget>
  );

  function displayName(key: string) {
    const [kind, id] = key.split(':');
    return kind === 'team'
      ? `Group: ${principals.data?.teams.find((team) => team.Id === Number(id))?.Name || `#${id}`}`
      : `User: ${principals.data?.users.find((user) => user.Id === Number(id))?.Username || `#${id}`}`;
  }
  function assign(event: FormEvent) {
    event.preventDefault();
    if (principal) mutation.mutate({ key: principal, value: role });
  }
}

function rows(entity: ScopedAccessEntity) {
  return [
    ...Object.entries(entity.TeamAccessPolicies || {}).map(([id, policy]) => ({
      key: `team:${id}`,
      policy,
    })),
    ...Object.entries(entity.UserAccessPolicies || {}).map(([id, policy]) => ({
      key: `user:${id}`,
      policy,
    })),
  ];
}
