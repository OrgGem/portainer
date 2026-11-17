import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import axios from '@/portainer/services/axios/axios';
import {
  Environment,
  EnvironmentStatus,
  EnvironmentType,
} from '@/react/portainer/environments/types';
import { withAgentTargetHeader } from '@/react/docker/proxy/queries/utils';

import { formatMetric, MiniMetric } from './MiniMetric';
import { Metric, MountUsage, ResourceUsage } from './types';

export function ResourceUsagePanel({
  environment,
  containerId,
  nodeName,
}: {
  environment: Pick<Environment, 'Id' | 'Type' | 'Status'>;
  containerId?: string;
  nodeName?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(
    typeof IntersectionObserver === 'undefined'
  );
  useEffect(() => {
    if (!ref.current || typeof IntersectionObserver === 'undefined')
      return undefined;
    const observer = new IntersectionObserver(
      ([entry]) => setVisible(entry.isIntersecting),
      { rootMargin: '100px' }
    );
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [environment.Type]);
  const supported = [
    EnvironmentType.Docker,
    EnvironmentType.AgentOnDocker,
  ].includes(environment.Type);
  const up = environment.Status === EnvironmentStatus.Up;
  const query = useQuery<ResourceUsage>(
    ['resource-metrics', environment.Id, containerId || 'host', nodeName || ''],
    async ({ signal }) => {
      const path = containerId
        ? `/containers/${containerId}/metrics`
        : '/metrics';
      const { data } = await axios.get<ResourceUsage>(
        `/endpoints/${environment.Id}${path}`,
        { signal, headers: withAgentTargetHeader(nodeName) }
      );
      return data;
    },
    {
      enabled: supported && up && visible,
      refetchInterval: visible ? 10000 : false,
      refetchIntervalInBackground: false,
      staleTime: 8000,
      cacheTime: 30000,
      retry: false,
    }
  );

  if (!supported) return null;
  const data = query.isError || !up ? undefined : query.data;
  return (
    <div
      ref={ref}
      className={
        containerId
          ? 'w-[560px] space-y-1 py-1'
          : 'min-w-[260px] max-w-xl space-y-2 py-2'
      }
      data-cy="resource-usage"
    >
      {!data ? (
        <span className="text-muted text-xs">
          CPU / RAM / Storage:{' '}
          {query.isError || !up ? 'Unavailable' : 'Waiting for sample…'}
        </span>
      ) : containerId ? (
        <div
          title={`${data.source}\n${data.notes.join('\n')}\nSample: ${data.sampledAt}\nStorage: ${data.storageSampledAt || 'unavailable'}`}
        >
          <div className="grid grid-cols-2 gap-3">
            <MiniMetric
              label="CPU"
              metric={data.cpu}
              sampledAt={data.sampledAt}
            />
            <MiniMetric
              label="RAM"
              metric={data.memory}
              sampledAt={data.sampledAt}
            />
          </div>
          <div
            className="grid min-w-0 grid-cols-2 items-center gap-3 pt-1"
            aria-label="Container storage usage"
          >
            {data.writableLayer ? (
              <MiniMetric
                compact
                label="Layer"
                metric={data.writableLayer}
                sampledAt={data.storageSampledAt || data.sampledAt}
              />
            ) : (
              <span className="text-muted text-xs">Layer unavailable</span>
            )}
            {data.mounts[0] ? (
              <div
                className="flex min-w-0 items-center gap-1"
                title={mountDetails(data.mounts)}
              >
                <div className="min-w-0 flex-1">
                  <MiniMetric
                    compact
                    label={`Vol ${data.mounts[0].target}`}
                    metric={data.mounts[0].data}
                    sampledAt={data.storageSampledAt || data.sampledAt}
                    description={mountDetails(data.mounts)}
                  />
                </div>
                {data.mounts.length > 1 && (
                  <span className="text-muted shrink-0 text-xs">
                    +{data.mounts.length - 1}
                  </span>
                )}
              </div>
            ) : (
              <span className="text-muted text-xs">No volume mounts</span>
            )}
          </div>
        </div>
      ) : (
        <>
          <MiniMetric
            label="CPU"
            metric={data.cpu}
            sampledAt={data.sampledAt}
          />
          <MiniMetric
            label="RAM"
            metric={data.memory}
            sampledAt={data.sampledAt}
          />
          {data.writableLayer && (
            <MiniMetric
              label="Writable layer"
              metric={data.writableLayer}
              sampledAt={data.storageSampledAt || data.sampledAt}
            />
          )}
          {data.mounts[0] && (
            <MountMetric
              mount={data.mounts[0]}
              sampledAt={data.storageSampledAt || data.sampledAt}
              host={!containerId}
            />
          )}
          {data.mounts.length > 1 ? (
            <details>
              <summary className="cursor-pointer text-xs">
                More {containerId ? 'volume / bind mounts' : 'host filesystems'}{' '}
                ({data.mounts.length - 1})
              </summary>
              <div className="max-h-64 space-y-3 overflow-y-auto pt-2">
                {data.mounts.slice(1).map((mount) => (
                  <MountMetric
                    key={`${mount.target}:${mount.name}`}
                    mount={mount}
                    sampledAt={data.storageSampledAt || data.sampledAt}
                    host={!containerId}
                  />
                ))}
              </div>
              <div className="text-muted pt-1 text-xs">
                Shared filesystem capacity is not a volume quota. Mounts may
                share the same disk; do not add their totals.
              </div>
            </details>
          ) : (
            data.mounts.length === 0 && (
              <div className="text-muted text-xs">
                {containerId
                  ? 'No volume mounts'
                  : 'Host filesystem usage unavailable'}
              </div>
            )
          )}
          <div
            className="text-muted text-xs"
            title={`${data.source}\n${data.notes.join('\n')}\nStorage: ${data.storageSampledAt ? new Date(data.storageSampledAt).toLocaleTimeString() : 'unavailable'}`}
          >
            {data.state} · {new Date(data.sampledAt).toLocaleTimeString()} ·
            refresh 10s
          </div>
          {data.notes.length > 0 && (
            <div className="text-muted text-xs">{data.notes.join('. ')}</div>
          )}
        </>
      )}
    </div>
  );
}

function MountMetric({
  mount,
  sampledAt,
  host,
}: {
  mount: MountUsage;
  sampledAt: string;
  host: boolean;
}) {
  return (
    <div>
      <div className="break-all text-xs font-medium" title={mount.name}>
        {mount.target}{' '}
        <span className="text-muted font-normal">
          ({mount.type}
          {mount.readOnly ? ', read-only' : ''})
        </span>
      </div>
      {mount.name && !host && (
        <div className="text-muted break-all text-xs">{mount.name}</div>
      )}
      <MiniMetric
        label={host ? 'Filesystem' : 'Volume data'}
        metric={mount.data}
        sampledAt={sampledAt}
      />
      {mount.filesystem && (
        <MiniMetric
          label="Shared filesystem"
          metric={mount.filesystem}
          sampledAt={sampledAt}
        />
      )}
    </div>
  );
}

function mountDetails(mounts: MountUsage[]) {
  function usage(metric: Metric) {
    return `${formatMetric(metric.used, metric.unit)} / ${metric.total === null ? 'no known quota' : formatMetric(metric.total, metric.unit)}`;
  }
  return mounts
    .map((mount) =>
      [
        `${mount.target} (${mount.type}${mount.readOnly ? ', read-only' : ''})`,
        mount.name,
        `Volume data: ${usage(mount.data)}`,
        mount.data.basis,
        mount.filesystem &&
          `Shared filesystem: ${usage(mount.filesystem)} (not a volume quota)`,
      ]
        .filter(Boolean)
        .join('\n')
    )
    .join('\n\n');
}
