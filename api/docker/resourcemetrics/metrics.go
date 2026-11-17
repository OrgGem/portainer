// Package resourcemetrics exposes usage and capacity without carrying Docker
// configuration (which can contain credentials) into a metrics response.
package resourcemetrics

import (
	"math"
	"strconv"
	"strings"
	"time"
)

type Metric struct {
	Used  *float64 `json:"used"`
	Total *float64 `json:"total"`
	Unit  string   `json:"unit"`
	Basis string   `json:"basis"`
}

type Mount struct {
	Name       string  `json:"name,omitempty"`
	Target     string  `json:"target"`
	Type       string  `json:"type"`
	ReadOnly   bool    `json:"readOnly"`
	Data       Metric  `json:"data"`
	Filesystem *Metric `json:"filesystem,omitempty"`
}

type Usage struct {
	SampledAt        time.Time  `json:"sampledAt"`
	StorageSampledAt *time.Time `json:"storageSampledAt,omitempty"`
	CPU              Metric     `json:"cpu"`
	Memory           Metric     `json:"memory"`
	WritableLayer    *Metric    `json:"writableLayer,omitempty"`
	Mounts           []Mount    `json:"mounts"`
	State            string     `json:"state"`
	Source           string     `json:"source"`
	Notes            []string   `json:"notes"`
}

type Info struct {
	NCPU          int
	MemTotal      int64
	OSType        string
	DockerRootDir string
}
type Inspect struct {
	ID    string `json:"Id"`
	State struct {
		Running bool
		Paused  bool
	}
	HostConfig struct {
		NanoCpus   int64
		CpuQuota   int64
		CpuPeriod  int64
		CpusetCpus string
		Memory     int64
		StorageOpt map[string]string
	}
	Mounts []struct {
		Type, Name, Source, Destination string
		RW                              bool
	}
}
type CPUStats struct {
	CPUUsage struct {
		TotalUsage  uint64   `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint64 `json:"online_cpus"`
}
type Stats struct {
	Read        time.Time `json:"read"`
	CPU         CPUStats  `json:"cpu_stats"`
	PreviousCPU CPUStats  `json:"precpu_stats"`
	Memory      struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}
type DiskUsage struct {
	Containers []struct {
		ID     string `json:"Id"`
		SizeRw int64
	}
	Volumes []struct {
		Name      string
		UsageData *struct{ Size int64 }
	}
}

func Number(n float64) *float64         { return &n }
func Unknown(unit, basis string) Metric { return Metric{Unit: unit, Basis: basis} }

func Container(inspect Inspect, info Info, stats *Stats, disk *DiskUsage) Usage {
	u := Usage{SampledAt: time.Now().UTC(), CPU: Unknown("cores", "host capacity; no CPU quota"), Memory: Unknown("bytes", "host capacity; no memory limit"), Mounts: []Mount{}, Notes: []string{}, Source: "Docker Engine", State: "running"}
	if info.NCPU > 0 {
		u.CPU.Total = Number(float64(info.NCPU))
	}
	if info.MemTotal > 0 {
		u.Memory.Total = Number(float64(info.MemTotal))
	}
	limit := func(m *Metric, v float64, basis string) {
		if v > 0 && (m.Total == nil || v < *m.Total) {
			m.Total = Number(v)
		}
		if v > 0 {
			m.Basis = basis
		}
	}
	hc := inspect.HostConfig
	limit(&u.CPU, float64(hc.NanoCpus)/1e9, "CPU quota / host capacity")
	if hc.CpuQuota > 0 {
		period := hc.CpuPeriod
		if period == 0 {
			period = 100000
		}
		limit(&u.CPU, float64(hc.CpuQuota)/float64(period), "CPU quota / host capacity")
	}
	if count := CPUSetCount(hc.CpusetCpus); count > 0 {
		limit(&u.CPU, float64(count), "CPU affinity / quota / host capacity")
	}
	limit(&u.Memory, float64(hc.Memory), "memory limit / host capacity")
	if !inspect.State.Running || inspect.State.Paused {
		u.State = "stopped"
		if inspect.State.Paused {
			u.State = "paused"
		}
		u.CPU.Used = Number(0)
		if !inspect.State.Running {
			u.Memory.Used = Number(0)
		}
	}
	if stats != nil {
		if !stats.Read.IsZero() {
			u.SampledAt = stats.Read
		}
		cur, prev := stats.CPU, stats.PreviousCPU
		n := cur.OnlineCPUs
		if n == 0 {
			n = uint64(len(cur.CPUUsage.PercpuUsage))
		}
		if n == 0 && info.NCPU > 0 {
			n = uint64(info.NCPU)
		}
		if u.CPU.Used == nil && cur.SystemCPUUsage > prev.SystemCPUUsage && cur.CPUUsage.TotalUsage >= prev.CPUUsage.TotalUsage && n > 0 {
			u.CPU.Used = Number(float64(cur.CPUUsage.TotalUsage-prev.CPUUsage.TotalUsage) / float64(cur.SystemCPUUsage-prev.SystemCPUUsage) * float64(n))
		}
		if stats.Memory.Limit > 0 {
			limit(&u.Memory, float64(stats.Memory.Limit), u.Memory.Basis)
			used := stats.Memory.Usage
			for _, key := range []string{"total_inactive_file", "inactive_file", "cache"} {
				if cached, ok := stats.Memory.Stats[key]; ok {
					if cached <= used {
						used -= cached
					}
					break
				}
			}
			u.Memory.Used = Number(float64(used))
		}
	}
	if info.OSType != "" && info.OSType != "linux" {
		u.CPU.Used = nil
		u.Memory.Used = nil
		u.Notes = append(u.Notes, "Live CPU/RAM calculation requires Linux containers")
	}
	rw := Unknown("bytes", "writable layer; no known quota")
	if q := ParseBytes(hc.StorageOpt["size"]); q > 0 {
		rw.Total = Number(q)
		rw.Basis = "writable layer quota"
	}
	if disk != nil {
		for _, c := range disk.Containers {
			if c.ID == inspect.ID && c.SizeRw >= 0 {
				rw.Used = Number(float64(c.SizeRw))
				break
			}
		}
	}
	u.WritableLayer = &rw
	for _, m := range inspect.Mounts {
		mount := Mount{Name: m.Name, Target: m.Destination, Type: m.Type, ReadOnly: !m.RW, Data: Unknown("bytes", "mount data; quota unavailable")}
		if disk != nil && m.Type == "volume" {
			for _, v := range disk.Volumes {
				if v.Name == m.Name && v.UsageData != nil && v.UsageData.Size >= 0 {
					mount.Data.Used = Number(float64(v.UsageData.Size))
					break
				}
			}
		}
		u.Mounts = append(u.Mounts, mount)
	}
	return u
}

func CPUSetCount(s string) int {
	seen := map[int]bool{}
	if s == "" {
		return 0
	}
	for _, item := range strings.Split(s, ",") {
		parts := strings.Split(item, "-")
		if len(parts) > 2 {
			return 0
		}
		lo, err := strconv.Atoi(parts[0])
		if err != nil || lo < 0 || lo > 65535 {
			return 0
		}
		hi := lo
		if len(parts) == 2 {
			hi, err = strconv.Atoi(parts[1])
			if err != nil || hi < lo || hi > 65535 {
				return 0
			}
		}
		for n := lo; n <= hi; n++ {
			seen[n] = true
		}
	}
	return len(seen)
}

func ParseBytes(s string) float64 {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	multiplier := float64(1)
	for _, unit := range []struct {
		s string
		n float64
	}{{"g", 1 << 30}, {"m", 1 << 20}, {"k", 1 << 10}, {"t", 1 << 40}} {
		found := false
		for _, suffix := range []string{unit.s + "ib", unit.s + "b", unit.s} {
			if strings.HasSuffix(s, suffix) {
				s = strings.TrimSuffix(s, suffix)
				multiplier = unit.n
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 1e18/multiplier {
		return 0
	}
	return n * multiplier
}
