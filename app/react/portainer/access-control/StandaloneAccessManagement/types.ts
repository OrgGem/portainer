export type StandaloneRole =
  | 'environment-admin'
  | 'operator'
  | 'helpdesk'
  | 'standard'
  | 'read-only';

export const standaloneRoles: Array<{
  value: StandaloneRole;
  label: string;
  id: number;
  description: string;
}> = [
  {
    value: 'environment-admin',
    label: 'Environment administrator',
    id: 1,
    description:
      'Manage environment resources. Global settings, host administration and ownership remain administrator-only.',
  },
  {
    value: 'operator',
    label: 'Operator',
    id: 5,
    description:
      'Operate existing containers and consoles. No resource creation/deletion or Compose edits.',
  },
  {
    value: 'helpdesk',
    label: 'Helpdesk',
    id: 2,
    description:
      'Read all environment resources. No writes, console or file access.',
  },
  {
    value: 'standard',
    label: 'Standard user',
    id: 3,
    description:
      'Manage assigned and public resources, subject to stack operation permissions.',
  },
  {
    value: 'read-only',
    label: 'Read-only user',
    id: 4,
    description:
      'Read assigned and public resources. No writes, console or file access.',
  },
];

export interface ScopedAccessPolicy {
  RoleId: number;
  StandaloneRole?: StandaloneRole;
}
export interface ScopedAccessEntity {
  Id: number;
  GroupId?: number;
  Name: string;
  UserAccessPolicies: Record<number, ScopedAccessPolicy>;
  TeamAccessPolicies: Record<number, ScopedAccessPolicy>;
}
