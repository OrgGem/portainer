import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { notifyError, notifySuccess } from '@/portainer/services/notifications';
import { Stack, StackType } from '@/react/common/stacks/types';
import { queryKeys } from '@/react/common/stacks/queries/query-keys';
import { useCurrentUser, useIsPureAdmin } from '@/react/hooks/useUser';

import { Controls, StackAction } from './types';

export function useStackControls(stack?: Stack) {
  return useQuery({
    queryKey: [...queryKeys.stack(stack?.Id), 'controls'],
    queryFn: async () =>
      (await axios.get<Controls>(`/stacks/${stack?.Id}/controls`)).data,
    enabled:
      !!stack && stack.Type === StackType.DockerCompose && !stack.GitConfig,
  });
}

export function useStackAction(stack: Stack | undefined, action: StackAction) {
  const admin = useIsPureAdmin();
  const { user } = useCurrentUser();
  const query = useStackControls(stack);
  const role = stack && user.StandaloneEndpointRoles?.[stack.EndpointId];
  if (!admin && role) {
    if ((role === 'read-only' || role === 'helpdesk') && action !== 'view')
      return false;
    if (role === 'operator' && action === 'edit') return false;
    if (role === 'environment-admin') return true;
  }
  if (admin || !stack?.ResourceControl?.StackPolicy) return true;
  return !!query.data?.Permissions.includes(action);
}

export function useControlMutation(stack: Stack) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async ({
      path,
      method = 'post',
      data,
    }: {
      path: string;
      method?: 'post' | 'put';
      data: unknown;
    }) => {
      try {
        return await axios.request({
          url: `/stacks/${stack.Id}/${path}`,
          method,
          data,
          timeout: 300000,
        });
      } catch (error) {
        throw parseAxiosError(error, 'Stack operation failed');
      }
    },
    onSuccess: () => notifySuccess('Success', 'Stack operation completed'),
    onError: (error) => notifyError('Stack operation failed', error as Error),
    onSettled: () => client.invalidateQueries(queryKeys.stack(stack.Id)),
  });
}
