package authorization

import (
	portainer "github.com/portainer/portainer/api"
	"slices"
)

func StackPolicyAllows(policy *portainer.StackAccessPolicy, userID portainer.UserID, teams []portainer.TeamID, action portainer.StackAction) bool {
	if policy == nil {
		return true
	}
	view, allowed := false, false
	for _, grant := range policy.Grants {
		if (grant.UserID != 0 && grant.UserID == userID) || (grant.TeamID != 0 && slices.Contains(teams, grant.TeamID)) {
			view = view || slices.Contains(grant.Actions, portainer.StackView)
			allowed = allowed || slices.Contains(grant.Actions, action)
		}
	}
	return view && allowed
}

func UserCanPerformStackAction(userID portainer.UserID, teams []portainer.TeamID, control *portainer.ResourceControl, action portainer.StackAction) bool {
	return UserCanAccessResource(userID, teams, control) && StackPolicyAllows(control.StackPolicy, userID, teams, action)
}
