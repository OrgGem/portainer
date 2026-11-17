package stacks

import (
	"sync"

	portainer "github.com/portainer/portainer/api"
)

type stackMutationLock struct {
	sync.Mutex
	references int
}

// Reference counts include waiters so an entry cannot be replaced while in use.
func (h *Handler) lockStackMutation(id portainer.StackID) func() {
	h.stackControlsMutex.Lock()
	if h.stackMutationLocks == nil {
		h.stackMutationLocks = make(map[portainer.StackID]*stackMutationLock)
	}
	lock := h.stackMutationLocks[id]
	if lock == nil {
		lock = &stackMutationLock{}
		h.stackMutationLocks[id] = lock
	}
	lock.references++
	h.stackControlsMutex.Unlock()
	lock.Lock()
	return func() {
		lock.Unlock()
		h.stackControlsMutex.Lock()
		defer h.stackControlsMutex.Unlock()
		lock.references--
		if lock.references == 0 {
			delete(h.stackMutationLocks, id)
		}
	}
}
