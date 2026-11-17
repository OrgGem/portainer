//go:build !linux

package resourcemetrics

import "context"

func HostFilesystem(root, source string) *Metric { return nil }
func Host(ctx context.Context, root string, info Info) Usage {
	return Usage{CPU: Unknown("cores", "unavailable"), Memory: Unknown("bytes", "unavailable"), Mounts: []Mount{}, Notes: []string{"Host metrics collector requires Linux"}}
}
