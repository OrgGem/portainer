export interface Metric {
  used: number | null;
  total: number | null;
  unit: 'cores' | 'bytes';
  basis: string;
}

export interface MountUsage {
  name?: string;
  target: string;
  type: string;
  readOnly: boolean;
  data: Metric;
  filesystem?: Metric;
}

export interface ResourceUsage {
  sampledAt: string;
  storageSampledAt?: string;
  cpu: Metric;
  memory: Metric;
  writableLayer?: Metric;
  mounts: MountUsage[];
  state: string;
  source: string;
  notes: string[];
}
