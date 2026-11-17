package deployments

import (
	"sync"

	portainer "github.com/portainer/portainer/api"
)

type composeDeploymentLock struct {
	sync.Mutex
	references int
}

// Compose builds are scheduled by Docker host in libstack. A global deployment
// mutex would prevent that scheduler from admitting parallel or separate-host
// builds. Keep only operations on the same stack serialized here.
func (d *stackDeployer) lockComposeDeployment(id portainer.StackID) func() {
	d.composeLockMutex.Lock()
	if d.composeLocks == nil {
		d.composeLocks = make(map[portainer.StackID]*composeDeploymentLock)
	}
	lock := d.composeLocks[id]
	if lock == nil {
		lock = &composeDeploymentLock{}
		d.composeLocks[id] = lock
	}
	lock.references++
	d.composeLockMutex.Unlock()
	lock.Lock()
	return func() {
		lock.Unlock()
		d.composeLockMutex.Lock()
		defer d.composeLockMutex.Unlock()
		lock.references--
		if lock.references == 0 {
			delete(d.composeLocks, id)
		}
	}
}
