import { useIdParam } from '@/react/hooks/useIdParam';
import { StandaloneAccessManagement } from '@/react/portainer/access-control/StandaloneAccessManagement/StandaloneAccessManagement';

export function AccessTab() {
  const groupId = useIdParam();
  return <StandaloneAccessManagement entityId={groupId} scope="group" />;
}
