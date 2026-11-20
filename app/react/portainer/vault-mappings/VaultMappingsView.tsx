import { FormEvent, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useCurrentStateAndParams } from '@uirouter/react';

import axios from '@/portainer/services/axios/axios';

import { PageHeader } from '@@/PageHeader';
import { Widget, WidgetBody } from '@@/Widget';
import { Button } from '@@/buttons';
import { Link } from '@@/Link';

import { inventoryQueryKey, InventoryResponse } from './types';

const emptyFilters = {
  environmentId: '',
  stackId: '',
  variable: '',
  path: '',
  key: '',
  service: '',
};
type Filters = typeof emptyFilters;
const textFilters = [
  { key: 'variable', label: 'Environment variable' },
  { key: 'path', label: 'Vault path' },
  { key: 'key', label: 'Vault key' },
  { key: 'service', label: 'Service' },
] as const;
const pageSize = 50;

export function VaultMappingsView() {
  const { params } = useCurrentStateAndParams();
  const [filters, setFilters] = useState<Filters>(
    () =>
      Object.fromEntries(
        Object.keys(emptyFilters).map((key) => [
          key,
          params[key] ? String(params[key]) : '',
        ])
      ) as Filters
  );
  const [applied, setApplied] = useState(filters);
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: [...inventoryQueryKey, applied, offset],
    queryFn: async () =>
      (
        await axios.get<InventoryResponse>('/stacks/vault/mappings', {
          params: { ...applied, offset, limit: pageSize },
        })
      ).data,
    keepPreviousData: false,
  });

  return (
    <>
      <PageHeader title="Vault mappings" breadcrumbs={['Vault mappings']} />
      <Widget>
        <WidgetBody>
          <div className="space-y-5">
            <h3>Reference inventory</h3>
            <p>
              See which application variables reference each Vault path and key,
              grouped by their environment, stack and service. Only mappings you
              are authorized to view appear in results, filters and counts.
            </p>
            <p>
              <strong>Saved references only.</strong> Counts describe the saved
              configuration; they do not confirm that a path exists or that a
              running application uses it. Portainer retrieves mapped values
              during deployment when injection is enabled.
            </p>
            <form onSubmit={apply} className="space-y-3">
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
                <div>
                  <label htmlFor="vault-filter-environment">Environment</label>
                  <select
                    id="vault-filter-environment"
                    className="form-control"
                    value={filters.environmentId}
                    onChange={(e) =>
                      setFilters({
                        ...filters,
                        environmentId: e.target.value,
                        stackId: '',
                      })
                    }
                  >
                    <option value="">All environments</option>
                    {query.data?.Environments.map((env) => (
                      <option key={env.ID} value={env.ID}>
                        {env.Name} (#{env.ID})
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label htmlFor="vault-filter-stack">Stack</label>
                  <select
                    id="vault-filter-stack"
                    className="form-control"
                    value={filters.stackId}
                    onChange={(e) =>
                      setFilters({ ...filters, stackId: e.target.value })
                    }
                  >
                    <option value="">All stacks</option>
                    {query.data?.Stacks.filter(
                      (stack) =>
                        !filters.environmentId ||
                        String(stack.EnvironmentID) === filters.environmentId
                    ).map((stack) => (
                      <option key={stack.ID} value={stack.ID}>
                        {stack.Name} (#{stack.ID})
                      </option>
                    ))}
                  </select>
                </div>
                {textFilters.map((field) => (
                  <div key={field.key}>
                    <label htmlFor={`vault-filter-${field.key}`}>
                      {field.label}
                    </label>
                    <input
                      id={`vault-filter-${field.key}`}
                      className="form-control"
                      maxLength={512}
                      value={filters[field.key]}
                      onChange={(e) =>
                        setFilters({ ...filters, [field.key]: e.target.value })
                      }
                    />
                  </div>
                ))}
              </div>
              <p>
                Text filters match part of the name or path, ignoring case. All
                filters are combined.
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="submit"
                  data-cy="filter-vault-inventory"
                  disabled={query.isFetching}
                >
                  Apply filters
                </Button>
                <Button
                  data-cy="clear-vault-filters"
                  disabled={query.isFetching}
                  onClick={() => {
                    setFilters(emptyFilters);
                    setApplied(emptyFilters);
                    setOffset(0);
                  }}
                >
                  Clear filters
                </Button>
                <Button
                  data-cy="refresh-vault-inventory"
                  disabled={query.isFetching}
                  onClick={() => query.refetch()}
                >
                  Refresh
                </Button>
              </div>
            </form>
            {query.isLoading && <p>Loading Vault mappings...</p>}
            {query.isError && (
              <p role="alert">
                Unable to load Vault mappings. Check your access and filters,
                then refresh.
              </p>
            )}
            {!query.isError && query.data && (
              <>
                <div
                  className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6"
                  aria-label="Filtered mapping statistics"
                >
                  {Object.entries(query.data.Summary).map(([label, count]) => (
                    <div
                      key={label}
                      className="rounded border border-solid border-gray-5 p-3"
                    >
                      <div className="text-2xl font-semibold">{count}</div>
                      <div>
                        {label === 'Paths'
                          ? 'Distinct path names'
                          : label === 'Variables'
                            ? 'Distinct variable names'
                            : label}
                      </div>
                    </div>
                  ))}
                </div>
                <div className="overflow-x-auto">
                  <table className="table">
                    <thead>
                      <tr>
                        <th>Environment</th>
                        <th>Stack</th>
                        <th>Service</th>
                        <th>Environment variable</th>
                        <th>Vault API path</th>
                        <th>Key</th>
                        <th>Engine</th>
                        <th>Saved</th>
                      </tr>
                    </thead>
                    <tbody>
                      {query.data.Items.map((row) => (
                        <tr key={`${row.StackID}:${row.ID}`}>
                          <td>
                            {row.EnvironmentName} (#{row.EnvironmentID})
                          </td>
                          <td>
                            <Link
                              data-cy={`vault-stack-${row.StackID}-${row.ID}`}
                              to="docker.stacks.stack"
                              params={{
                                endpointId: row.EnvironmentID,
                                id: row.StackID,
                                name: row.StackName,
                                type: 2,
                                regular: true,
                                external: false,
                                orphaned: false,
                                tab: 'vault',
                              }}
                            >
                              {row.StackName}
                            </Link>
                          </td>
                          <td>{row.Service}</td>
                          <td>
                            <code>{row.Variable}</code>
                          </td>
                          <td>
                            <code>{row.Path}</code>
                          </td>
                          <td>
                            <code>{row.Key}</code>
                          </td>
                          <td>{row.Engine}</td>
                          <td>
                            v{row.Version} ·{' '}
                            {new Date(row.UpdatedAt * 1000).toLocaleString()} ·
                            user #{row.UpdatedBy}
                          </td>
                        </tr>
                      ))}
                      {query.data.Items.length === 0 && (
                        <tr>
                          <td colSpan={8}>
                            No visible mappings match these filters.
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
                <div className="flex items-center gap-3">
                  <span>
                    {query.data.Items.length === 0 ? 0 : offset + 1}–
                    {offset + query.data.Items.length} of{' '}
                    {query.data.Summary.Mappings} matching mappings
                  </span>
                  <Button
                    data-cy="vault-previous-page"
                    disabled={offset === 0 || query.isFetching}
                    onClick={() => setOffset(Math.max(0, offset - pageSize))}
                  >
                    Previous
                  </Button>
                  <Button
                    data-cy="vault-next-page"
                    disabled={
                      offset + pageSize >= query.data.Summary.Mappings ||
                      query.isFetching
                    }
                    onClick={() => setOffset(offset + pageSize)}
                  >
                    Next
                  </Button>
                </div>
              </>
            )}
          </div>
        </WidgetBody>
      </Widget>
    </>
  );

  function apply(event: FormEvent) {
    event.preventDefault();
    setApplied({ ...filters });
    setOffset(0);
  }
}
