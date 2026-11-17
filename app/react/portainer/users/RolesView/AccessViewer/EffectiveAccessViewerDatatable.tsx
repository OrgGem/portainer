import { TextTip } from '@@/Tip/TextTip';
import { Datatable } from '@@/datatables';
import { useTableStateWithStorage } from '@@/datatables/useTableState';

import { AccessViewerPolicyModel } from './model';
import { columns } from './columns';

export function EffectiveAccessViewerDatatable({
  dataset,
}: {
  dataset?: Array<AccessViewerPolicyModel>;
}) {
  const tableState = useTableStateWithStorage('access-viewer', 'Environment');

  if (dataset?.length === 0) {
    return (
      <TextTip color="blue">
        The selected user does not have access to any environments.
      </TextTip>
    );
  }

  return (
    <Datatable
      dataset={dataset || []}
      columns={columns}
      settingsManager={tableState}
      noWidget
      title="Access"
      description={
        <TextTip color="blue">
          Role features are an upper bound, not a guarantee for every resource.
          Precedence: direct user/environment, user/group, team/environment,
          team/group. Resource ownership, stack action policies and environment
          security settings may further restrict operations.
        </TextTip>
      }
      disableSelect
      data-cy="effective-access-viewer-datatable"
    />
  );
}
