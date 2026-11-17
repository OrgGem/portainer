export const actions = [
  'view',
  'config.upload',
  'config.apply',
  'deploy',
  'edit',
] as const;
export type StackAction = (typeof actions)[number];
export const actionLabels: Record<StackAction, string> = {
  view: 'View',
  'config.upload': 'Upload config',
  'config.apply': 'Apply / rollback config',
  deploy: 'Deploy / stop',
  edit: 'Edit Compose / image',
};
export interface Grant {
  UserID?: number;
  TeamID?: number;
  Actions: StackAction[];
}
export interface Policy {
  Version: number;
  Grants: Grant[];
}
export interface ConfigTarget {
  ID: string;
  Service: string;
  Target: string;
  ActiveRevision?: string;
}
export interface ConfigRevision {
  ID: string;
  TargetID: string;
  Filename: string;
  SHA256: string;
  Size: number;
  CreatedAt: number;
  CreatedBy: number;
  AppliedAt?: number;
  AppliedBy?: number;
}
export interface Configuration {
  Version: number;
  Targets: ConfigTarget[] | null;
  Revisions: ConfigRevision[] | null;
  PendingRevision?: string;
  Events:
    | {
        RevisionID: string;
        Action: string;
        UserID: number;
        Time: number;
        Success: boolean;
      }[]
    | null;
}
export interface Controls {
  Policy: Policy | null;
  Permissions: StackAction[];
  Configuration: Configuration | null;
  Services: string[];
  ConfigurationUnavailable?: string;
}
