import angular from 'angular';

import { r2a } from '@/react-tools/react2angular';
import { withUIRouter } from '@/react-tools/withUIRouter';
import { ActivityLogsView } from '@/react/portainer/logs/ActivityLogsView/ActivityLogsView';
import { withCurrentUser } from '@/react-tools/withCurrentUser';
import { withReactQuery } from '@/react-tools/withReactQuery';
import { AuthenticationLogsView } from '@/react/portainer/logs/AuthenticationLogsView/AuthenticationLogsView';

export const activityLogsModule = angular
  .module('portainer.app.react.views.activity-logs', [])
  .component(
    'activityLogsView',
    r2a(withUIRouter(withReactQuery(withCurrentUser(ActivityLogsView))), [])
  )
  .component(
    'authLogsView',
    r2a(
      withUIRouter(withReactQuery(withCurrentUser(AuthenticationLogsView))),
      []
    )
  ).name;
