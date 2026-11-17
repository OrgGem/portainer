package uac

import (
	"fmt"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/internal/authorization"
	"github.com/portainer/portainer/api/slicesx"
	roleauth "github.com/portainer/portainer/pkg/authorization"
)

// FilterByResourceControl filters a list of items based on the user's role and the resource control associated to the item.
// UAC rules
// * UAC in DB (direct)
// * UAC inherited (from swarm service | swarm stack | compose stack)
// * UAC defined in labels (UAC on external resources)
func FilterByResourceControl[T any](
	items []T,
	user *portainer.User,
	userMemberships []portainer.TeamMembership,
	resourceControlGetter func(item T) (*portainer.ResourceControl, error),
	scoped ...*roleauth.StandaloneAccess,
) ([]T, error) {
	filteredItems := make([]T, 0)

	if user == nil {
		return filteredItems, nil
	}

	if canBypassUAC(user) {
		return items, nil
	}

	userTeamIDs := slicesx.Map(userMemberships, func(membership portainer.TeamMembership) portainer.TeamID {
		return membership.TeamID
	})

	for _, item := range items {
		rc, err := resourceControlGetter(item)
		if err != nil {
			return nil, fmt.Errorf("Unable to retrieve resource control: %w", err)
		}

		// TODO: move UserCanAccessResource function to the UAC package
		allowed := authorization.UserCanAccessResource(user.ID, userTeamIDs, rc)
		if len(scoped) > 0 {
			allowed = roleauth.StandaloneResourceAccess(scoped[0], user.ID, userTeamIDs, rc)
		}
		if allowed {
			filteredItems = append(filteredItems, item)
		}
	}

	return filteredItems, nil
}

func canBypassUAC(user *portainer.User) bool {
	return user != nil && user.Role == portainer.AdministratorRole
}
