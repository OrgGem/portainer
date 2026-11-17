package authorization

import (
	portainer "github.com/portainer/portainer/api"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStackPolicyOwnershipAndActions(t *testing.T) {
	control := NewRestrictedResourceControl("1_app", portainer.StackResourceControl, []portainer.UserID{2}, []portainer.TeamID{7})
	control.StackPolicy = &portainer.StackAccessPolicy{Grants: []portainer.StackAccessGrant{
		{UserID: 2, Actions: []portainer.StackAction{portainer.StackView, portainer.StackConfigUpload}},
		{TeamID: 7, Actions: []portainer.StackAction{portainer.StackView, portainer.StackConfigApply}},
		{UserID: 9, Actions: []portainer.StackAction{portainer.StackView, portainer.StackDeploy}},
	}}
	require.True(t, UserCanPerformStackAction(2, nil, control, portainer.StackConfigUpload))
	require.False(t, UserCanPerformStackAction(2, nil, control, portainer.StackConfigApply))
	require.True(t, UserCanPerformStackAction(3, []portainer.TeamID{7}, control, portainer.StackConfigApply))
	require.False(t, UserCanPerformStackAction(3, []portainer.TeamID{8}, control, portainer.StackConfigApply))
	require.False(t, UserCanPerformStackAction(9, nil, control, portainer.StackDeploy), "a grant cannot bypass resource ownership")
	require.False(t, UserCanPerformStackAction(2, []portainer.TeamID{7}, control, portainer.StackEdit))
	require.True(t, UserCanPerformStackAction(2, []portainer.TeamID{7}, control, portainer.StackConfigApply), "team and user grants are additive")
	control.StackPolicy = &portainer.StackAccessPolicy{}
	require.False(t, UserCanAccessResource(2, nil, control), "an empty policy denies regular users")
	control.StackPolicy = nil
	require.True(t, UserCanAccessResource(2, nil, control), "legacy access is unchanged")
}

func TestStackPolicyRequiresView(t *testing.T) {
	policy := &portainer.StackAccessPolicy{Grants: []portainer.StackAccessGrant{{UserID: 2, Actions: []portainer.StackAction{portainer.StackDeploy}}}}
	require.False(t, StackPolicyAllows(policy, 2, nil, portainer.StackDeploy))
}
