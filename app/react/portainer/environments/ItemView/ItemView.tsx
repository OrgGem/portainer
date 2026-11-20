import { useIdParam } from '@/react/hooks/useIdParam';
import { useIsPureAdmin } from '@/react/hooks/useUser';
import { EnvironmentType } from '@/react/portainer/environments/types';

import { PageHeader } from '@@/PageHeader';

import { useEnvironment } from '../queries';
import {
  getPlatformTypeName,
  isEdgeEnvironment,
  isLocalDockerEnvironment,
} from '../utils';

import { EdgeInformationPanel } from './EdgeInformationPanel/EdgeInformationPanel';
import { EdgeAgentDeploymentWidget } from './EdgeAgentDeploymentWidget/EdgeAgentDeploymentWidget';
import { KubeConfigInfo } from './KubeConfigInfo/KubeConfigInfo';
import { EnvironmentDetailsForm } from './EnvironmentDetailsForm';
import { VaultConnectionPanel } from './VaultConnectionPanel';

export function ItemView() {
  const id = useIdParam();
  const isAdmin = useIsPureAdmin();
  const environmentQuery = useEnvironment(id);

  if (!environmentQuery.data) {
    return null;
  }

  const environment = environmentQuery.data;

  const isEdge = isEdgeEnvironment(environment.Type);
  const platformName = getPlatformTypeName(environment.Type);

  return (
    <>
      <PageHeader
        title="Environment details"
        breadcrumbs={[
          { label: 'Environments', link: 'portainer.endpoints' },
          environment.Name,
        ]}
        reload
      />

      <div className="mx-4 space-y-4 [&>*]:block">
        {isEdge &&
          (environment.EdgeID ? (
            <EdgeInformationPanel
              environmentId={id}
              edgeKey={environment.EdgeKey}
              edgeId={environment.EdgeID}
              platformName={platformName}
            />
          ) : (
            <EdgeAgentDeploymentWidget
              edgeKey={environment.EdgeKey}
              edgeId={environment.EdgeID}
              asyncMode={environment.Edge?.AsyncMode}
            />
          ))}

        <KubeConfigInfo
          environmentId={id}
          environmentType={environment.Type}
          edgeId={environment.EdgeID}
          status={environment.Status}
        />

        <EnvironmentDetailsForm environment={environment} />

        {isAdmin &&
          (environment.Type === EnvironmentType.AgentOnDocker ||
            (environment.Type === EnvironmentType.Docker &&
              isLocalDockerEnvironment(environment.URL))) && (
            <VaultConnectionPanel environmentId={id} />
          )}
      </div>
    </>
  );
}
