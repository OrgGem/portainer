import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import axios from '@/portainer/services/axios/axios';
import { Stack } from '@/react/common/stacks/types';
import { User } from '@/portainer/users/types';
import { useTeams } from '@/react/portainer/users/teams/queries/useTeams';

import { Button } from '@@/buttons';

import { actions, actionLabels, Grant, Policy } from './types';
import { useControlMutation } from './queries';

export function PolicyEditor({
  stack,
  policy,
}: {
  stack: Stack;
  policy: Policy | null;
}) {
  const [grants, setGrants] = useState<Grant[]>(policy?.Grants || []);
  const users = useQuery({
    queryKey: ['stack-policy-users'],
    queryFn: async () => (await axios.get<User[]>('/users')).data,
  });
  const teams = useTeams();
  const mutation = useControlMutation(stack);

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        mutation.mutate({
          path: 'policy',
          method: 'put',
          data: { Version: policy?.Version || 0, Grants: grants },
        });
      }}
      className="space-y-4"
    >
      <h4>Stack operation permissions</h4>
      <p>
        Users also need access to this environment and stack through the
        existing access control settings. An empty policy permits administrators
        only.
      </p>
      <p>
        Enabling a policy restricts direct Docker changes and consoles to
        administrators across this environment. Operators use the permitted
        stack actions. Editing Compose and images is a trusted deployment
        permission.
      </p>
      <div className="overflow-x-auto">
        <table className="table">
          <thead>
            <tr>
              <th>User or team</th>
              {actions.map((action) => (
                <th key={action}>{actionLabels[action]}</th>
              ))}
              <th>Remove</th>
            </tr>
          </thead>
          <tbody>
            {grants.map((grant, index) => (
              // A row has no persisted identifier until the policy is saved.
              <tr key={index}>
                <td>
                  <select
                    aria-label={`Principal ${index + 1}`}
                    className="form-control"
                    required
                    value={
                      grant.UserID
                        ? `user:${grant.UserID}`
                        : grant.TeamID
                          ? `team:${grant.TeamID}`
                          : ''
                    }
                    onChange={(event) => {
                      const [kind, id] = event.target.value.split(':');
                      setGrants(
                        grants.map((item, i) =>
                          i === index
                            ? {
                                Actions: item.Actions,
                                ...(kind === 'user'
                                  ? { UserID: Number(id) }
                                  : { TeamID: Number(id) }),
                              }
                            : item
                        )
                      );
                    }}
                  >
                    <option value="">Select user or team</option>
                    <optgroup label="Teams">
                      {teams.data?.map((team) => (
                        <option key={team.Id} value={`team:${team.Id}`}>
                          {team.Name}
                        </option>
                      ))}
                    </optgroup>
                    <optgroup label="Users">
                      {users.data?.map((user) => (
                        <option key={user.Id} value={`user:${user.Id}`}>
                          {user.Username}
                        </option>
                      ))}
                    </optgroup>
                  </select>
                </td>
                {actions.map((action) => (
                  <td key={action}>
                    <input
                      type="checkbox"
                      aria-label={`${actionLabels[action]} for principal ${index + 1}`}
                      checked={grant.Actions.includes(action)}
                      disabled={action === 'view'}
                      onChange={(event) =>
                        setGrants(
                          grants.map((item, i) =>
                            i === index
                              ? {
                                  ...item,
                                  Actions: event.target.checked
                                    ? [...item.Actions, action]
                                    : item.Actions.filter((a) => a !== action),
                                }
                              : item
                          )
                        )
                      }
                    />
                  </td>
                ))}
                <td>
                  <Button
                    data-cy={`remove-stack-grant-${index}`}
                    color="dangerlight"
                    onClick={() =>
                      setGrants(grants.filter((_, i) => i !== index))
                    }
                  >
                    Remove
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Button
        data-cy="add-stack-grant"
        onClick={() => setGrants([...grants, { Actions: ['view'] }])}
      >
        Add user / team
      </Button>
      <Button
        type="submit"
        data-cy="save-stack-policy"
        disabled={mutation.isLoading || users.isError || teams.isError}
      >
        {policy ? 'Save permissions' : 'Enable operation policy'}
      </Button>
    </form>
  );
}
