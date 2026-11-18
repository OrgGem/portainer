angular.module('portainer.app').controller('MainController', MainController);

/* @ngInject */
function MainController($scope, StateManager, SidebarService) {
  /**
   * Sidebar Toggle & Cookie Control
   */

  $scope.applicationState = StateManager.getState();

  $scope.isSidebarOpen = SidebarService.isSidebarOpen;

}
