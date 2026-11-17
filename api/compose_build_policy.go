package portainer

import "fmt"

// ComposeBuildPolicy is a shared budget for all managed builds on a Docker host.
// The ceiling is deliberately not overridable by individual stacks.
type ComposeBuildPolicy struct {
	CPUPercent                 int  `json:"CPUPercent"`
	MemoryPercent              int  `json:"MemoryPercent"`
	MaxConcurrentBuilds        int  `json:"MaxConcurrentBuilds"`
	AutoConfigureBuilderCgroup bool `json:"AutoConfigureBuilderCgroup"`
}

func DefaultComposeBuildPolicy() ComposeBuildPolicy {
	return ComposeBuildPolicy{CPUPercent: 50, MemoryPercent: 50, MaxConcurrentBuilds: 1}
}

func (p ComposeBuildPolicy) Validate() error {
	if p.CPUPercent < 1 || p.CPUPercent > 50 || p.MemoryPercent < 1 || p.MemoryPercent > 50 {
		return fmt.Errorf("build CPU and memory percentages must be integers between 1 and 50")
	}
	if p.MaxConcurrentBuilds < 1 || p.MaxConcurrentBuilds > 8 {
		return fmt.Errorf("concurrent builds must be between 1 and 8; all builds share the host budget")
	}
	return nil
}

func (s *Settings) EffectiveComposeBuildPolicy() ComposeBuildPolicy {
	if s.ComposeBuildPolicy == nil {
		return DefaultComposeBuildPolicy()
	}
	return *s.ComposeBuildPolicy
}
