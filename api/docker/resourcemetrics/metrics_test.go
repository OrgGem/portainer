package resourcemetrics

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContainerQuotasAndUnknownDisk(t *testing.T) {
	info := Info{NCPU: 8, MemTotal: 8 << 30, OSType: "linux"}
	var inspect Inspect
	inspect.ID = "container"
	inspect.State.Running = true
	inspect.HostConfig.NanoCpus = 500000000
	inspect.HostConfig.Memory = 64 << 20
	var stats Stats
	stats.CPU.SystemCPUUsage = 2000
	stats.PreviousCPU.SystemCPUUsage = 1000
	stats.CPU.CPUUsage.TotalUsage = 100
	stats.PreviousCPU.CPUUsage.TotalUsage = 50
	stats.CPU.OnlineCPUs = 8
	stats.Memory.Usage = 40 << 20
	stats.Memory.Limit = 64 << 20
	stats.Memory.Stats = map[string]uint64{"inactive_file": 10 << 20}
	u := Container(inspect, info, &stats, nil)
	require.InDelta(t, 0.4, *u.CPU.Used, 0.0001)
	require.Equal(t, 0.5, *u.CPU.Total)
	require.Equal(t, float64(30<<20), *u.Memory.Used)
	require.Equal(t, float64(64<<20), *u.Memory.Total)
	require.Nil(t, u.WritableLayer.Used)
	require.Nil(t, u.WritableLayer.Total)
	inspect.HostConfig.CpusetCpus = "0-1"
	inspect.HostConfig.NanoCpus = 0
	inspect.HostConfig.CpuQuota = 300000
	inspect.HostConfig.CpuPeriod = 100000
	u = Container(inspect, info, &stats, nil)
	require.Equal(t, float64(2), *u.CPU.Total)
	stats.CPU.SystemCPUUsage = 500
	stats.Memory.Stats = map[string]uint64{"total_inactive_file": 60 << 20}
	u = Container(inspect, info, &stats, nil)
	require.Nil(t, u.CPU.Used)
	require.Equal(t, float64(40<<20), *u.Memory.Used)
	inspect.State.Running = false
	u = Container(inspect, info, nil, nil)
	require.Zero(t, *u.CPU.Used)
	require.Zero(t, *u.Memory.Used)
	require.Equal(t, "stopped", u.State)
}

func TestSizesAndCPUSet(t *testing.T) {
	for text, want := range map[string]float64{"10G": 10 << 30, "64MiB": 64 << 20, "1024": 1024, "-1": 0, "NaN": 0, "Inf": 0, "bad": 0} {
		require.Equal(t, want, ParseBytes(text), text)
	}
	for text, want := range map[string]int{"0-2,2,4": 4, "": 0, "2-1": 0, "x": 0, "0-99999999": 0} {
		require.Equal(t, want, CPUSetCount(text), text)
	}
}

func TestCacheCoalescesAndExpires(t *testing.T) {
	c := NewCache()
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sample, err := c.Get(context.Background(), "shared", 20*time.Millisecond, func() ([]byte, error) {
				calls.Add(1)
				time.Sleep(5 * time.Millisecond)
				return []byte("measurement"), nil
			})
			require.NoError(t, err)
			require.Equal(t, "measurement", string(sample.Data))
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
	time.Sleep(25 * time.Millisecond)
	_, err := c.Get(context.Background(), "shared", time.Second, func() ([]byte, error) { calls.Add(1); return nil, nil })
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
}
