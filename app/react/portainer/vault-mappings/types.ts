export interface VaultMapping {
  ID: string;
  Service: string;
  Variable: string;
  Path: string;
  Key: string;
  Engine: 'kv-v1' | 'kv-v2';
}

export interface VaultConfiguration {
  Version: number;
  Mappings: VaultMapping[];
  InjectOnDeploy?: boolean;
  UpdatedAt: number;
  UpdatedBy: number;
}

export interface VaultMappingsInput {
  Mappings: VaultMapping[];
  InjectOnDeploy: boolean;
}

export interface StackVaultResponse {
  Configuration: VaultConfiguration;
  Services: string[];
  CanEdit: boolean;
  InjectionSupported?: boolean;
  Unavailable?: string;
}

export interface InventoryRow extends VaultMapping {
  EnvironmentID: number;
  EnvironmentName: string;
  StackID: number;
  StackName: string;
  Version: number;
  UpdatedAt: number;
  UpdatedBy: number;
}

export interface InventoryResponse {
  Items: InventoryRow[];
  Summary: Record<
    'Mappings' | 'Variables' | 'Paths' | 'Environments' | 'Stacks' | 'Services',
    number
  >;
  Environments: { ID: number; Name: string }[];
  Stacks: { ID: number; Name: string; EnvironmentID: number }[];
}

export const inventoryQueryKey = ['vault-mappings-inventory'];
export function vaultQueryKey(id: number) {
  return ['stacks', id, 'vault-mappings'];
}
